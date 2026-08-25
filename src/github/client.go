package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Client is a GitHub API client with ETag caching and rate limit handling.
// It is safe for concurrent use.
type Client struct {
	httpClient *http.Client
	auth       *Auth
	etagMu sync.RWMutex
	etags  map[string]string

	rateLimitRemaining atomic.Int64

	// onRateLimit, when set, is called for every 403 with the kind of limit
	// that caused it and how long the client is about to wait. It exists so
	// that "we are being throttled, and by which limit" can be a metric
	// instead of only a log line -- the distinction that took hours to
	// establish by hand on 2026-08-11.
	onRateLimit func(kind string, sleep time.Duration)

	sleepFunc func(d time.Duration) // for testing
}

// maxResponseBytes bounds how much of a success response is read.
const maxResponseBytes = 100 << 20 // 100 MiB

// maxErrorBodyBytes bounds how much of an error response is read. Enough for a
// GitHub error message, and not a promise to buffer whatever arrives.
const maxErrorBodyBytes = 8 << 10

// maxETagEntries caps the ETag cache to prevent unbounded memory growth.
const maxETagEntries = 10000

// Rate limit kinds. GitHub answers 403 for both and the difference decides
// whether the right response is seconds or most of an hour.
//
// The PRIMARY limit is quota: so many requests per hour, reported in the
// headers, reset at a timestamp. Exhausting it means waiting for the reset.
//
// The SECONDARY limit is burst and concurrency protection. It is not about
// totals at all, and it is what stopped the exporter on 2026-08-11: measured
// with a freshly minted installation token at the moment of the 403, the
// primary quota read 8 used of 5250, remaining 5242, resetting in 13 minutes.
// Eight requests. The exporter was throttled for sending them too close
// together, not for sending too many.
//
// The trap, and the reason this classification exists at all: GitHub sets
// X-RateLimit-Remaining: 0 in the headers of a SECONDARY 403, with the primary
// budget nearly untouched. Read those headers naively -- which this client did
// -- and a burst throttle that wants a few seconds is mistaken for quota
// exhaustion that wants an hour. PR #78 capped the wait so it could no longer
// hang; this tells the two apart so the wait is the right length, and so that
// the remaining-quota figure the pacer reads is not poisoned by a header that
// is lying.
const (
	rateLimitNone      = "none"
	rateLimitPrimary   = "primary"
	rateLimitSecondary = "secondary"
)

// secondaryBackoff is the wait for a secondary limit with no Retry-After
// header. GitHub's own guidance is to wait at least a minute; the reset
// timestamp is emphatically not the right number here, because the quota it
// refers to was never the problem.
const secondaryBackoff = 60 * time.Second

// How long the client will wait on a single 403, and how many times it will
// retry one URL before giving up.
//
// Both exist because the loop below used to have neither, and that combination
// stopped the exporter dead on 2026-08-11: the first poll collected 25
// repositories, hit a 403 on the next call, slept toward a reset timestamp, and
// never finished. Nine minutes in, poll_duration_seconds_count was still 0 --
// no error, no log line, no metric moved. A wait with no ceiling and no voice
// is indistinguishable from a healthy idle process, which is the worst way for
// a collector to fail.
//
// maxRateLimitSleep caps a single wait. GitHub's primary limit resets hourly,
// and sleeping an hour inside a five-minute poll cycle is never the right
// answer -- returning and letting the next cycle try again is.
const (
	maxRateLimitSleep = 2 * time.Minute
	maxRateLimitRetry = 3
)

// NewClient creates a new GitHub API Client using the provided Auth.
func NewClient(auth *Auth) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		auth:       auth,
		etags:      make(map[string]string),
		sleepFunc:  time.Sleep,
	}
}

// RateLimitRemaining returns the most recently observed PRIMARY rate limit
// remaining value.
//
// Deliberately not updated from a secondary-limit 403, whose header says zero
// while the real quota is nearly untouched. A pacer that reads this number
// needs it to mean what it says; the secondary limit is about request rate, and
// the defense against it is spacing requests out, not watching a counter.
func (c *Client) RateLimitRemaining() int {
	return int(c.rateLimitRemaining.Load())
}

// SetRateLimitObserver installs a callback invoked on every 403, with the kind
// of limit ("primary", "secondary", or "none") and the wait about to be taken.
func (c *Client) SetRateLimitObserver(f func(kind string, sleep time.Duration)) {
	c.onRateLimit = f
}

// Get performs a GET request to the given URL, decoding the JSON response into result.
// It returns modified=true if new data was received (HTTP 200), and modified=false
// if the server returned 304 Not Modified. On rate limiting (403), it sleeps and retries.
func (c *Client) Get(ctx context.Context, url string, result interface{}) (modified bool, err error) {
	attempts := 0
	for {
		token, err := c.auth.Token(ctx)
		if err != nil {
			return false, fmt.Errorf("obtaining auth token: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return false, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		c.etagMu.RLock()
		if etag, ok := c.etags[url]; ok {
			req.Header.Set("If-None-Match", etag)
		}
		c.etagMu.RUnlock()

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return false, fmt.Errorf("executing request: %w", err)
		}

		// Update rate limit tracking from response headers -- except on a 403,
		// where the header may be a secondary-limit zero and is handled below.
		if resp.StatusCode != http.StatusForbidden {
			c.recordRateLimitHeader(resp)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			etag := resp.Header.Get("ETag")
			if etag != "" {
				c.etagMu.Lock()
				if len(c.etags) >= maxETagEntries {
					// Evict all entries to bound memory. The next
					// poll cycle will repopulate with fresh ETags.
					c.etags = make(map[string]string, maxETagEntries/2)
				}
				c.etags[url] = etag
				c.etagMu.Unlock()
			}
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
			resp.Body.Close()
			if readErr != nil {
				return false, fmt.Errorf("reading response body: %w", readErr)
			}
			if err := json.Unmarshal(body, result); err != nil {
				return false, fmt.Errorf("decoding JSON: %w", err)
			}
			return true, nil

		case http.StatusNotModified:
			resp.Body.Close()
			return false, nil

		case http.StatusForbidden:
			// The body is read because it is the only trustworthy signal of
			// which limit was hit: a secondary throttle says so in words
			// ("You have exceeded a secondary rate limit"), while its headers
			// claim the quota is gone. Bounded, because a body arriving on an
			// error path is not something to trust the length of.
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
			resp.Body.Close()

			kind, sleepDur := classifyRateLimit(resp, body)
			if kind != rateLimitSecondary {
				// A secondary 403's X-RateLimit-Remaining is a zero that means
				// nothing. Recording it would tell the pacer the quota is gone
				// when thousands of requests of it remain, and a backfiller
				// that believes that idles for no reason.
				c.recordRateLimitHeader(resp)
			}
			if c.onRateLimit != nil {
				c.onRateLimit(kind, sleepDur)
			}
			if sleepDur == 0 {
				return false, fmt.Errorf("forbidden: %s", url)
			}

			// Give up rather than wait forever. A 403 that keeps coming back is
			// a condition the caller needs to hear about -- the poller counts it
			// and moves on to the next repository, which is strictly better than
			// one stuck URL holding up every collector behind it.
			attempts++
			if attempts > maxRateLimitRetry {
				return false, fmt.Errorf(
					"rate limited on %s: still 403 after %d retries", url, maxRateLimitRetry)
			}

			// Cap the wait. GitHub reports a reset timestamp for the PRIMARY
			// limit that can be most of an hour away; a secondary limit sets
			// Retry-After and is usually seconds. Both arrive as 403, and the
			// difference is not always visible in the headers, so the ceiling
			// protects against mistaking one for the other.
			if sleepDur > maxRateLimitSleep {
				sleepDur = maxRateLimitSleep
			}

			// Say so, and say WHICH limit. The line without the kind sent
			// whoever read it to check the quota, which was fine, which is
			// where the hours went.
			slog.Warn("rate limited by GitHub; backing off",
				"url", url, "kind", kind, "sleep", sleepDur, "attempt", attempts,
				"remaining", c.RateLimitRemaining())

			select {
			case <-ctx.Done():
				return false, ctx.Err()
			default:
				c.sleepFunc(sleepDur)
				continue
			}

		default:
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return false, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
		}
	}
}

// recordRateLimitHeader stores the remaining primary quota reported by a
// response, when it reports one.
func (c *Client) recordRateLimitHeader(resp *http.Response) {
	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			c.rateLimitRemaining.Store(n)
		}
	}
}

// classifyRateLimit decides which limit a 403 came from and how long to wait.
// It returns rateLimitNone and a zero duration for a 403 that is not about
// rate limiting at all -- a permissions problem, say, which no amount of
// waiting will fix.
//
// The order of the checks is the argument:
//
//  1. Retry-After. GitHub sends it for secondary limits and not for primary
//     ones, and when it is there it is the authoritative answer.
//
//  2. The body text. A secondary throttle says "You have exceeded a secondary
//     rate limit" in its message, and this is the discriminator that costs
//     nothing -- no extra request, no second endpoint to be wrong about. It is
//     checked BEFORE the headers because the headers are what mislead: on
//     2026-08-11 the secondary 403 carried X-RateLimit-Remaining: 0 while a
//     direct check of /rate_limit at the same moment showed 8 used of 5250.
//
//  3. Remaining zero, with nothing calling it secondary. That is the genuine
//     primary exhaustion case, and only then is the reset timestamp the right
//     thing to wait for. The caller still caps the wait -- see maxRateLimitSleep
//     and PR #78 -- because an hour-long sleep inside a poll cycle is never the
//     right answer even when the arithmetic says so.
func classifyRateLimit(resp *http.Response, body []byte) (kind string, sleep time.Duration) {
	secondaryText := strings.Contains(
		strings.ToLower(string(body)), "secondary rate limit")

	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return rateLimitSecondary, time.Duration(secs) * time.Second
		}
	}
	if secondaryText {
		return rateLimitSecondary, secondaryBackoff
	}

	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		if resetStr := resp.Header.Get("X-RateLimit-Reset"); resetStr != "" {
			if resetUnix, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
				dur := time.Until(time.Unix(resetUnix, 0))
				if dur < 0 {
					dur = 1 * time.Second
				}
				return rateLimitPrimary, dur
			}
		}
		// Rate limited but no reset header; default backoff.
		return rateLimitPrimary, secondaryBackoff
	}

	return rateLimitNone, 0
}
