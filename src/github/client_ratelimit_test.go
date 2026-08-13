package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// GitHub answers 403 for two entirely different conditions and the headers do
// not separate them. These tests are the record of the measurement that proved
// it, taken on the live host after PR #78 went in:
//
//	level=WARN msg="rate limited by GitHub; backing off"
//	  url=.../actions/runs/30193589922/jobs sleep=2m0s attempt=1 remaining=0
//
// and, at the same moment, from /rate_limit with a fresh installation token for
// the same App:
//
//	core: used 8 of 5250, remaining 5242, resets in 13 min
//
// Eight requests of five thousand, and the 403's own header said zero. Treating
// that zero as quota exhaustion buys a two-minute sleep where a few seconds
// would do, and -- worse -- poisons the figure the backfiller reads to decide
// whether it is safe to keep working.

// secondaryBody is what GitHub actually sends when the secondary limiter fires.
const secondaryBody = `{"message":"You have exceeded a secondary rate limit. ` +
	`Please wait a few minutes before you try again.",` +
	`"documentation_url":"https://docs.github.com/rest/overview/rate-limits"}`

func TestClassifyRateLimit(t *testing.T) {
	reset := time.Now().Add(45 * time.Minute)

	cases := []struct {
		name      string
		headers   map[string]string
		body      string
		wantKind  string
		wantSleep time.Duration
		// wantApprox allows the primary case, whose sleep is computed from a
		// wall-clock reset timestamp, to be checked loosely.
		wantApprox bool
	}{
		{
			name:      "secondary limit says so in the body, whatever the headers claim",
			headers:   map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(reset.Unix())},
			body:      secondaryBody,
			wantKind:  rateLimitSecondary,
			wantSleep: secondaryBackoff,
		},
		{
			name:      "retry-after is authoritative and means secondary",
			headers:   map[string]string{"Retry-After": "7"},
			body:      secondaryBody,
			wantKind:  rateLimitSecondary,
			wantSleep: 7 * time.Second,
		},
		{
			name:       "remaining zero with nothing calling it secondary is primary exhaustion",
			headers:    map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(reset.Unix())},
			wantKind:   rateLimitPrimary,
			wantApprox: true,
		},
		{
			name:      "primary with no reset header falls back to a fixed wait",
			headers:   map[string]string{"X-RateLimit-Remaining": "0"},
			wantKind:  rateLimitPrimary,
			wantSleep: secondaryBackoff,
		},
		{
			name:      "a reset already in the past waits a second, not a negative one",
			headers:   map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(time.Now().Add(-time.Minute).Unix())},
			wantKind:  rateLimitPrimary,
			wantSleep: time.Second,
		},
		{
			name:      "an unparseable reset is still primary, with the fallback wait",
			headers:   map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "soon"},
			wantKind:  rateLimitPrimary,
			wantSleep: secondaryBackoff,
		},
		{
			name:      "an unparseable retry-after falls through rather than sleeping zero",
			headers:   map[string]string{"Retry-After": "in a bit", "X-RateLimit-Remaining": "0"},
			wantKind:  rateLimitPrimary,
			wantSleep: secondaryBackoff,
		},
		{
			name:      "a 403 that is not about rate limiting at all",
			headers:   map[string]string{"X-RateLimit-Remaining": "4000"},
			body:      `{"message":"Resource not accessible by integration"}`,
			wantKind:  rateLimitNone,
			wantSleep: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			for k, v := range tc.headers {
				resp.Header.Set(k, v)
			}

			kind, sleep := classifyRateLimit(resp, []byte(tc.body))
			if kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", kind, tc.wantKind)
			}
			if tc.wantApprox {
				if sleep < 44*time.Minute || sleep > 45*time.Minute {
					t.Errorf("sleep = %v, want about 45m (the reset)", sleep)
				}
				return
			}
			if sleep != tc.wantSleep {
				t.Errorf("sleep = %v, want %v", sleep, tc.wantSleep)
			}
		})
	}
}

// The heart of it: a secondary throttle must not overwrite the remaining-quota
// figure with its meaningless zero, because the backfiller reads that figure to
// decide whether to keep working. Believing the zero means idling while five
// thousand requests of quota go unused.
func TestClient_SecondaryLimitDoesNotPoisonTheRemainingQuota(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("X-RateLimit-Remaining", "4200")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		// The secondary 403, exactly as measured: remaining zero, quota fine.
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Hour).Unix()))
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(secondaryBody))
	}))
	defer server.Close()

	client := NewClient(testAuth("t"))
	client.sleepFunc = func(time.Duration) {}

	var result map[string]any
	if _, err := client.Get(context.Background(), server.URL+"/first", &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.RateLimitRemaining(); got != 4200 {
		t.Fatalf("remaining = %d, want 4200 after a good response", got)
	}

	_, _ = client.Get(context.Background(), server.URL+"/second", &result)

	if got := client.RateLimitRemaining(); got != 4200 {
		t.Errorf("remaining = %d, want 4200 -- a secondary 403's zero is not a quota reading", got)
	}
}

// Primary exhaustion, by contrast, reports the truth and should be recorded.
func TestClient_PrimaryExhaustionIsRecorded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Minute).Unix()))
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for installation ID 1."}`))
	}))
	defer server.Close()

	client := NewClient(testAuth("t"))
	client.rateLimitRemaining.Store(4200)
	client.sleepFunc = func(time.Duration) {}

	var result map[string]any
	_, _ = client.Get(context.Background(), server.URL+"/x", &result)

	if got := client.RateLimitRemaining(); got != 0 {
		t.Errorf("remaining = %d, want 0 -- genuine exhaustion is worth recording", got)
	}
}

// A secondary throttle waits seconds, not the reset timestamp. Before the
// classification it waited the capped maximum -- two minutes per run, with 163
// runs in one repository.
func TestClient_SecondaryLimitWaitsSecondsNotTheReset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(50*time.Minute).Unix()))
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(secondaryBody))
	}))
	defer server.Close()

	client := NewClient(testAuth("t"))
	var slept []time.Duration
	client.sleepFunc = func(d time.Duration) { slept = append(slept, d) }

	var result map[string]any
	_, _ = client.Get(context.Background(), server.URL+"/x", &result)

	if len(slept) == 0 {
		t.Fatal("expected a backoff")
	}
	for i, d := range slept {
		if d != 3*time.Second {
			t.Errorf("sleep %d = %v, want the 3s Retry-After rather than the reset", i, d)
		}
	}
}

// Every 403 is reported to the observer, including the ones that are not about
// rate limiting, so that "throttled" and "forbidden" can be told apart on a
// dashboard rather than in a log search.
func TestClient_RateLimitObserverSeesEveryKind(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "secondary",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(secondaryBody))
			},
			want: rateLimitSecondary,
		},
		{
			name: "primary",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Minute).Unix()))
				w.WriteHeader(http.StatusForbidden)
			},
			want: rateLimitPrimary,
		},
		{
			name: "not a rate limit",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
			},
			want: rateLimitNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			client := NewClient(testAuth("t"))
			client.sleepFunc = func(time.Duration) {}

			var kinds []string
			client.SetRateLimitObserver(func(kind string, _ time.Duration) {
				kinds = append(kinds, kind)
			})

			var result map[string]any
			_, _ = client.Get(context.Background(), server.URL+"/x", &result)

			if len(kinds) == 0 {
				t.Fatal("the observer was never called")
			}
			for _, k := range kinds {
				if k != tc.want {
					t.Errorf("kind = %q, want %q", k, tc.want)
				}
			}
		})
	}
}
