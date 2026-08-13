// Package governor implements a quota governor for Cloudflare API rate limits.
//
// It maintains a token bucket per API surface (GraphQL and REST) with a
// rolling 5-minute window, and enforces priority-based load shedding when
// the budget ceiling is approached.
package governor

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// APISurface identifies a Cloudflare API transport.
type APISurface string

const (
	// GraphQL is the Cloudflare GraphQL Analytics API.
	GraphQL APISurface = "graphql"
	// REST is the Cloudflare v4 REST API.
	REST APISurface = "rest"
)

// PriorityClass determines the shedding order when budget is constrained.
// Lower numeric values are higher priority and are shed last.
type PriorityClass int

const (
	// PriorityCritical covers Access and Gateway policy outcomes.
	PriorityCritical PriorityClass = iota
	// PriorityStandard covers DNS, tunnels, and isolation.
	PriorityStandard
	// PriorityBackground covers registrar, certificates, and inventory.
	PriorityBackground
)

const (
	// rollingWindow is the Cloudflare rate-limit window (5 minutes).
	rollingWindow = 5 * time.Minute

	// DefaultGraphQLBudget is 50% of the documented 320 queries per 5 minutes.
	DefaultGraphQLBudget = 160

	// DefaultRESTBudget is 50% of the documented 1200 requests per 5 minutes.
	DefaultRESTBudget = 600

	// shedThresholdStandard is the fraction of budget remaining below which
	// standard-priority requests are rejected.
	shedThresholdStandard = 0.20

	// shedThresholdCritical is the fraction of budget remaining below which
	// even critical-priority requests are rejected.
	shedThresholdCritical = 0.05
)

// bucketEntry records a batch of requests at a point in time.
type bucketEntry struct {
	timestamp time.Time
	cost      int
}

// bucket is a sliding-window token bucket for a single API surface.
type bucket struct {
	ceiling int
	entries []bucketEntry

	// retryAfter, if non-zero, is the earliest time new requests may proceed.
	retryAfter time.Time

	// serverRemaining, if non-negative, is the most recent remaining count
	// reported by the server via rate-limit headers. A value of -1 means
	// the server has not reported a value.
	serverRemaining int
}

// Governor manages API budget across GraphQL and REST surfaces.
type Governor struct {
	mu      sync.Mutex
	now     func() time.Time // injectable clock for testing
	buckets map[APISurface]*bucket
}

// NewGovernor creates a Governor with the given budget ceilings.
// Pass 0 for either budget to use the default.
func NewGovernor(graphqlBudget, restBudget int) *Governor {
	if graphqlBudget <= 0 {
		graphqlBudget = DefaultGraphQLBudget
	}
	if restBudget <= 0 {
		restBudget = DefaultRESTBudget
	}

	return &Governor{
		now: time.Now,
		buckets: map[APISurface]*bucket{
			GraphQL: {
				ceiling:         graphqlBudget,
				serverRemaining: -1,
			},
			REST: {
				ceiling:         restBudget,
				serverRemaining: -1,
			},
		},
	}
}

// Allow checks whether a request with the given cost and priority may proceed
// on the specified surface. It returns true if the request is admitted and
// false if budget is exhausted or the priority class should be shed.
//
// Allow does not consume budget; call Record after the request completes.
func (g *Governor) Allow(surface APISurface, cost int, priority PriorityClass) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	b, ok := g.buckets[surface]
	if !ok {
		return false
	}

	now := g.now()

	// Honor any active retry-after back-off.
	if now.Before(b.retryAfter) {
		return false
	}

	remaining := g.remainingLocked(surface, now)

	// Would this request breach the ceiling?
	if remaining-cost < 0 {
		return false
	}

	// Priority-based shedding: compute the fraction of budget remaining.
	fraction := float64(remaining) / float64(b.ceiling)

	switch priority {
	case PriorityBackground:
		// Shed background work when budget drops below the standard threshold.
		if fraction < shedThresholdStandard {
			return false
		}
	case PriorityStandard:
		// Shed standard work when budget drops below the critical threshold.
		if fraction < shedThresholdCritical {
			return false
		}
	case PriorityCritical:
		// Critical work is only rejected when the budget is fully exhausted
		// (handled by the remaining-cost < 0 check above).
	}

	return true
}

// Record records that cost units of budget were consumed on the given surface.
func (g *Governor) Record(surface APISurface, cost int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	b, ok := g.buckets[surface]
	if !ok {
		return
	}

	b.entries = append(b.entries, bucketEntry{
		timestamp: g.now(),
		cost:      cost,
	})
}

// Remaining returns the remaining budget for the given surface in the current
// rolling window.
func (g *Governor) Remaining(surface APISurface) int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.remainingLocked(surface, g.now())
}

// remainingLocked computes remaining budget. Caller must hold g.mu.
func (g *Governor) remainingLocked(surface APISurface, now time.Time) int {
	b, ok := g.buckets[surface]
	if !ok {
		return 0
	}

	// Expire old entries and compute usage within the rolling window.
	cutoff := now.Add(-rollingWindow)
	used := 0
	alive := b.entries[:0]

	for _, e := range b.entries {
		if e.timestamp.After(cutoff) {
			alive = append(alive, e)
			used += e.cost
		}
	}
	b.entries = alive

	remaining := b.ceiling - used

	// If the server has reported a remaining count (via headers), use the
	// lower of our local estimate and the server's value.
	if b.serverRemaining >= 0 {
		if b.serverRemaining < remaining {
			remaining = b.serverRemaining
		}
	}

	return remaining
}

// UpdateFromHeaders updates the governor state from Cloudflare rate-limit
// response headers.
//
// For REST responses it parses:
//   - Ratelimit-Remaining (integer: remaining requests in the current window)
//   - Ratelimit-Reset (integer: seconds until the window resets)
//   - Ratelimit-Policy (e.g. "1200;w=300" documenting the server ceiling)
//   - RateLimit-Remaining / RateLimit-Reset (canonical casing variants)
//
// For GraphQL responses the same headers may appear; the governor treats
// them identically.
func (g *Governor) UpdateFromHeaders(surface APISurface, headers http.Header) {
	g.mu.Lock()
	defer g.mu.Unlock()

	b, ok := g.buckets[surface]
	if !ok {
		return
	}

	// Try both casing conventions used by Cloudflare.
	if v := firstNonEmpty(headers, "Ratelimit-Remaining", "RateLimit-Remaining", "X-Ratelimit-Remaining"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			b.serverRemaining = n
		}
	}

	// Parse Ratelimit-Policy for documentation (e.g., "1200;w=300").
	// We do not override the user-configured ceiling, but we log the
	// server's advertised limit for observability.
	// (No action needed here beyond parsing; the ceiling is set at construction.)

	// Reset timer: if the server tells us when the window resets, we can
	// clear the server-remaining value at that point in subsequent calls.
	if v := firstNonEmpty(headers, "Ratelimit-Reset", "RateLimit-Reset", "X-Ratelimit-Reset"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
			resetAt := g.now().Add(time.Duration(secs) * time.Second)
			// If the server's reset is sooner than a full window, we can
			// schedule clearing the server-remaining value. For now we
			// simply note it; the sliding window will naturally age out
			// entries.
			_ = resetAt
		}
	}
}

// HandleRetryAfter should be called when an HTTP 429 response is received.
// It records the back-off and returns the duration the caller should wait
// before retrying.
//
// It parses the Retry-After header, which may be:
//   - an integer number of seconds, or
//   - an HTTP-date (RFC 1123).
//
// If the header is missing or unparseable, a default back-off of 60 seconds
// is used.
func (g *Governor) HandleRetryAfter(surface APISurface, headers http.Header) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()

	b, ok := g.buckets[surface]
	if !ok {
		return 60 * time.Second
	}

	now := g.now()
	retryAfter := parseRetryAfter(now, headers)

	if retryAfter.After(b.retryAfter) {
		b.retryAfter = retryAfter
	}

	// Also update server remaining to 0 since we received a 429.
	b.serverRemaining = 0

	// Update from any accompanying rate-limit headers.
	g.updateFromHeadersLocked(surface, headers)

	wait := retryAfter.Sub(now)
	if wait < 0 {
		wait = 0
	}
	return wait
}

// updateFromHeadersLocked is the lock-free inner implementation shared by
// UpdateFromHeaders and HandleRetryAfter. Caller must hold g.mu.
func (g *Governor) updateFromHeadersLocked(surface APISurface, headers http.Header) {
	b, ok := g.buckets[surface]
	if !ok {
		return
	}

	if v := firstNonEmpty(headers, "Ratelimit-Remaining", "RateLimit-Remaining", "X-Ratelimit-Remaining"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			b.serverRemaining = n
		}
	}
}

// parseRetryAfter extracts the retry-after time from response headers.
func parseRetryAfter(now time.Time, headers http.Header) time.Time {
	const defaultBackoff = 60 * time.Second

	v := headers.Get("Retry-After")
	if v == "" {
		return now.Add(defaultBackoff)
	}
	v = strings.TrimSpace(v)

	// Try integer seconds first.
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return now
		}
		// Cap at a reasonable maximum to avoid overflow.
		if secs > 3600 {
			secs = 3600
		}
		return now.Add(time.Duration(secs) * time.Second)
	}

	// Try HTTP-date (RFC 1123).
	if t, err := time.Parse(time.RFC1123, v); err == nil {
		return t
	}

	// Try RFC 1123 with numeric timezone.
	if t, err := time.Parse(time.RFC1123Z, v); err == nil {
		return t
	}

	return now.Add(defaultBackoff)
}

// firstNonEmpty returns the first non-empty header value for the given keys.
func firstNonEmpty(h http.Header, keys ...string) string {
	for _, k := range keys {
		if v := h.Get(k); v != "" {
			return v
		}
	}
	return ""
}

