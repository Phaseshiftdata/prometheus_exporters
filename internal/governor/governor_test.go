package governor

import (
	"net/http"
	"testing"
	"time"
)

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestNewGovernor_Defaults(t *testing.T) {
	g := NewGovernor(0, 0)
	if r := g.Remaining(GraphQL); r != DefaultGraphQLBudget {
		t.Fatalf("expected %d, got %d", DefaultGraphQLBudget, r)
	}
	if r := g.Remaining(REST); r != DefaultRESTBudget {
		t.Fatalf("expected %d, got %d", DefaultRESTBudget, r)
	}
}

func TestNewGovernor_CustomBudgets(t *testing.T) {
	g := NewGovernor(50, 100)
	if r := g.Remaining(GraphQL); r != 50 {
		t.Fatalf("expected 50, got %d", r)
	}
	if r := g.Remaining(REST); r != 100 {
		t.Fatalf("expected 100, got %d", r)
	}
}

func TestAllow_CriticalAlways(t *testing.T) {
	g := NewGovernor(10, 10)
	if !g.Allow(GraphQL, 1, PriorityCritical) {
		t.Fatal("expected critical to be allowed")
	}
}

func TestAllow_ExhaustsBudget(t *testing.T) {
	g := NewGovernor(5, 5)
	now := time.Now()
	g.now = fixedClock(now)

	g.Record(GraphQL, 5)
	if g.Allow(GraphQL, 1, PriorityCritical) {
		t.Fatal("expected rejection when budget exhausted")
	}
}

func TestAllow_BackgroundShedding(t *testing.T) {
	// Budget = 100, threshold for background = 20% = 20 remaining
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	// Consume 81 so remaining = 19, which is below 20%
	g.Record(GraphQL, 81)

	if g.Allow(GraphQL, 1, PriorityBackground) {
		t.Fatal("expected background to be shed when below 20%")
	}
	// Standard should still be allowed (threshold is 5%)
	if !g.Allow(GraphQL, 1, PriorityStandard) {
		t.Fatal("expected standard to be allowed when above 5%")
	}
	// Critical should still be allowed
	if !g.Allow(GraphQL, 1, PriorityCritical) {
		t.Fatal("expected critical to be allowed")
	}
}

func TestAllow_StandardShedding(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	// Consume 96 so remaining = 4, below 5%
	g.Record(GraphQL, 96)

	if g.Allow(GraphQL, 1, PriorityStandard) {
		t.Fatal("expected standard to be shed when below 5%")
	}
	if g.Allow(GraphQL, 1, PriorityBackground) {
		t.Fatal("expected background to be shed when below 5%")
	}
	// Critical still allowed (4 remaining > 1 cost)
	if !g.Allow(GraphQL, 1, PriorityCritical) {
		t.Fatal("expected critical to be allowed when remaining > cost")
	}
}

func TestAllow_UnknownSurface(t *testing.T) {
	g := NewGovernor(10, 10)
	if g.Allow("unknown", 1, PriorityCritical) {
		t.Fatal("expected false for unknown surface")
	}
}

func TestRecord_UnknownSurface(t *testing.T) {
	g := NewGovernor(10, 10)
	// Should not panic
	g.Record("unknown", 1)
}

func TestRemaining_SlidingWindow(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	g.Record(GraphQL, 10)

	// Move time forward past the 5-minute window
	g.now = fixedClock(now.Add(6 * time.Minute))

	if r := g.Remaining(GraphQL); r != 100 {
		t.Fatalf("expected 100 after entries expire, got %d", r)
	}
}

func TestUpdateFromHeaders(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	h := http.Header{}
	h.Set("Ratelimit-Remaining", "42")
	h.Set("Ratelimit-Reset", "60")

	g.UpdateFromHeaders(REST, h)

	// Server remaining = 42, local remaining = 100
	// Should use min(100, 42) = 42
	if r := g.Remaining(REST); r != 42 {
		t.Fatalf("expected 42, got %d", r)
	}
}

func TestUpdateFromHeaders_AlternativeCase(t *testing.T) {
	g := NewGovernor(100, 100)
	h := http.Header{}
	h.Set("RateLimit-Remaining", "50")

	g.UpdateFromHeaders(GraphQL, h)
	if r := g.Remaining(GraphQL); r != 50 {
		t.Fatalf("expected 50, got %d", r)
	}
}

func TestUpdateFromHeaders_XPrefix(t *testing.T) {
	g := NewGovernor(100, 100)
	h := http.Header{}
	h.Set("X-Ratelimit-Remaining", "30")

	g.UpdateFromHeaders(REST, h)
	if r := g.Remaining(REST); r != 30 {
		t.Fatalf("expected 30, got %d", r)
	}
}

func TestUpdateFromHeaders_UnknownSurface(t *testing.T) {
	g := NewGovernor(10, 10)
	h := http.Header{}
	h.Set("Ratelimit-Remaining", "5")
	g.UpdateFromHeaders("unknown", h) // should not panic
}

func TestHandleRetryAfter_Seconds(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	h := http.Header{}
	h.Set("Retry-After", "30")

	wait := g.HandleRetryAfter(GraphQL, h)
	if wait != 30*time.Second {
		t.Fatalf("expected 30s, got %v", wait)
	}

	// Should reject requests until retry-after expires
	if g.Allow(GraphQL, 1, PriorityCritical) {
		t.Fatal("expected rejection during retry-after period")
	}

	// Move past the retry-after window and reset server remaining
	g.now = fixedClock(now.Add(31 * time.Second))
	// Update headers to reflect fresh budget after retry period
	resetHeaders := http.Header{}
	resetHeaders.Set("Ratelimit-Remaining", "100")
	g.UpdateFromHeaders(GraphQL, resetHeaders)
	if !g.Allow(GraphQL, 1, PriorityCritical) {
		t.Fatal("expected allow after retry-after expired")
	}
}

func TestHandleRetryAfter_HTTPDate(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	g.now = fixedClock(now)

	retryTime := now.Add(45 * time.Second)
	h := http.Header{}
	h.Set("Retry-After", retryTime.Format(time.RFC1123))

	wait := g.HandleRetryAfter(REST, h)
	if wait != 45*time.Second {
		t.Fatalf("expected 45s, got %v", wait)
	}
}

func TestHandleRetryAfter_Missing(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	h := http.Header{}
	wait := g.HandleRetryAfter(GraphQL, h)
	if wait != 60*time.Second {
		t.Fatalf("expected default 60s, got %v", wait)
	}
}

func TestHandleRetryAfter_UnknownSurface(t *testing.T) {
	g := NewGovernor(10, 10)
	h := http.Header{}
	wait := g.HandleRetryAfter("unknown", h)
	if wait != 60*time.Second {
		t.Fatalf("expected 60s, got %v", wait)
	}
}

func TestHandleRetryAfter_ZeroSeconds(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	h := http.Header{}
	h.Set("Retry-After", "0")

	wait := g.HandleRetryAfter(GraphQL, h)
	if wait != 0 {
		t.Fatalf("expected 0, got %v", wait)
	}
}

func TestHandleRetryAfter_CappedAt3600(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	h := http.Header{}
	h.Set("Retry-After", "9999")

	wait := g.HandleRetryAfter(GraphQL, h)
	if wait != 3600*time.Second {
		t.Fatalf("expected 3600s, got %v", wait)
	}
}

func TestHandleRetryAfter_Unparseable(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	h := http.Header{}
	h.Set("Retry-After", "not-a-date-or-number")

	wait := g.HandleRetryAfter(REST, h)
	if wait != 60*time.Second {
		t.Fatalf("expected default 60s, got %v", wait)
	}
}

func TestHandleRetryAfter_SetsServerRemainingZero(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	h := http.Header{}
	h.Set("Retry-After", "10")

	g.HandleRetryAfter(GraphQL, h)

	// Move past retry-after
	g.now = fixedClock(now.Add(11 * time.Second))

	// Server remaining should be 0
	r := g.Remaining(GraphQL)
	if r != 0 {
		t.Fatalf("expected 0 remaining (server reported 0), got %d", r)
	}
}

func TestRemaining_UnknownSurface(t *testing.T) {
	g := NewGovernor(10, 10)
	if r := g.Remaining("unknown"); r != 0 {
		t.Fatalf("expected 0 for unknown surface, got %d", r)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	h := http.Header{}
	if v := firstNonEmpty(h, "A", "B"); v != "" {
		t.Fatalf("expected empty, got %q", v)
	}

	h.Set("B", "found")
	if v := firstNonEmpty(h, "A", "B"); v != "found" {
		t.Fatalf("expected found, got %q", v)
	}
}

func TestHandleRetryAfter_RFC1123Z(t *testing.T) {
	g := NewGovernor(100, 100)
	// Use a non-UTC timezone so that RFC1123 parsing fails (it expects
	// named timezone like "UTC") but RFC1123Z succeeds (it uses numeric
	// offset like "+0530").
	loc := time.FixedZone("IST", 5*3600+30*60) // +0530
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, loc)
	g.now = fixedClock(now)

	retryTime := now.Add(30 * time.Second)
	h := http.Header{}
	h.Set("Retry-After", retryTime.Format(time.RFC1123Z))

	wait := g.HandleRetryAfter(REST, h)
	if wait != 30*time.Second {
		t.Fatalf("expected 30s, got %v", wait)
	}
}

func TestUpdateFromHeaders_AccompanyingHeadersInRetryAfter(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	h := http.Header{}
	h.Set("Retry-After", "10")
	h.Set("Ratelimit-Remaining", "5")

	g.HandleRetryAfter(REST, h)

	// Move past retry-after
	g.now = fixedClock(now.Add(11 * time.Second))

	// The accompanying Ratelimit-Remaining header value is parsed after
	// the 429 handler sets serverRemaining to 0, so the header value (5)
	// overrides. Remaining should reflect the header value.
	r := g.Remaining(REST)
	if r != 5 {
		t.Fatalf("expected 5, got %d", r)
	}
}

func TestHandleRetryAfter_DoesNotShortenExisting(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	// Set a long retry-after first
	h1 := http.Header{}
	h1.Set("Retry-After", "120")
	g.HandleRetryAfter(GraphQL, h1)

	// Now try a shorter one - it should NOT shorten the existing retryAfter
	h2 := http.Header{}
	h2.Set("Retry-After", "10")
	g.HandleRetryAfter(GraphQL, h2)

	// At now+15 we should still be blocked (original 120s is in effect)
	g.now = fixedClock(now.Add(15 * time.Second))
	// Server remaining is 0 from the 429, so we need to reset it for this check
	resetH := http.Header{}
	resetH.Set("Ratelimit-Remaining", "100")
	g.UpdateFromHeaders(GraphQL, resetH)
	if g.Allow(GraphQL, 1, PriorityCritical) {
		t.Fatal("expected rejection - longer retry-after should be preserved")
	}
}

func TestUpdateFromHeadersLocked_NoMatchingHeader(t *testing.T) {
	g := NewGovernor(100, 100)
	h := http.Header{} // no rate limit headers
	g.UpdateFromHeaders(REST, h)
	// Should not change anything
	if r := g.Remaining(REST); r != 100 {
		t.Fatalf("expected 100, got %d", r)
	}
}

func TestHandleRetryAfter_PastDate(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	g.now = fixedClock(now)

	// Use an RFC1123 date in the past
	pastTime := now.Add(-30 * time.Second)
	h := http.Header{}
	h.Set("Retry-After", pastTime.Format(time.RFC1123))

	wait := g.HandleRetryAfter(REST, h)
	if wait != 0 {
		t.Fatalf("expected 0 for past retry-after date, got %v", wait)
	}
}

func TestParseRetryAfter_NegativeSeconds(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	h := http.Header{}
	h.Set("Retry-After", "-5")
	wait := g.HandleRetryAfter(GraphQL, h)
	if wait != 0 {
		t.Fatalf("expected 0 for negative retry-after, got %v", wait)
	}
}

func TestUpdateFromHeaders_NonNumericRemaining(t *testing.T) {
	g := NewGovernor(100, 100)
	h := http.Header{}
	h.Set("Ratelimit-Remaining", "not-a-number")

	g.UpdateFromHeaders(REST, h)
	// Non-numeric header should be ignored; remaining should stay at 100
	if r := g.Remaining(REST); r != 100 {
		t.Fatalf("expected 100, got %d", r)
	}
}

func TestUpdateFromHeaders_NegativeRemaining(t *testing.T) {
	g := NewGovernor(100, 100)
	h := http.Header{}
	h.Set("Ratelimit-Remaining", "-1")

	g.UpdateFromHeaders(REST, h)
	// Negative values should be ignored (n >= 0 check)
	if r := g.Remaining(REST); r != 100 {
		t.Fatalf("expected 100, got %d", r)
	}
}

func TestHandleRetryAfter_WithAccompanyingNonNumericRemaining(t *testing.T) {
	g := NewGovernor(100, 100)
	now := time.Now()
	g.now = fixedClock(now)

	h := http.Header{}
	h.Set("Retry-After", "10")
	h.Set("Ratelimit-Remaining", "abc") // non-numeric

	g.HandleRetryAfter(GraphQL, h)

	// Move past retry-after
	g.now = fixedClock(now.Add(11 * time.Second))

	// Server remaining should be 0 (from the 429 handling, since
	// the non-numeric header was ignored)
	r := g.Remaining(GraphQL)
	if r != 0 {
		t.Fatalf("expected 0 remaining, got %d", r)
	}
}
