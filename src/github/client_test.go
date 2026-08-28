package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testAuth creates an Auth that returns a fixed token.
func testAuth(token string) *Auth {
	return NewTestAuth(token, time.Now().Add(1*time.Hour))
}

func TestClient_Get_OK(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode(payload{Name: "hello"})
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	var result payload
	modified, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !modified {
		t.Fatal("expected modified=true")
	}
	if result.Name != "hello" {
		t.Fatalf("expected hello, got %s", result.Name)
	}
	if client.RateLimitRemaining() != 4999 {
		t.Fatalf("expected rate limit 4999, got %d", client.RateLimitRemaining())
	}
}

func TestClient_Get_ETagCaching(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			// First request: return data with ETag.
			w.Header().Set("ETag", `"etag-1"`)
			w.Header().Set("X-RateLimit-Remaining", "4998")
			json.NewEncoder(w).Encode(map[string]string{"data": "first"})
			return
		}
		// Second request: verify ETag is sent and return 304.
		if r.Header.Get("If-None-Match") != `"etag-1"` {
			t.Errorf("expected If-None-Match to be \"etag-1\", got %s", r.Header.Get("If-None-Match"))
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	var result map[string]string

	// First call - should get data.
	modified, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !modified {
		t.Fatal("expected modified=true on first call")
	}

	// Second call - should get 304.
	modified, err = client.Get(context.Background(), server.URL+"/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if modified {
		t.Fatal("expected modified=false on 304")
	}
}

func TestClient_Get_RateLimitBackoff(t *testing.T) {
	var requestCount atomic.Int32
	resetTime := time.Now().Add(1 * time.Second).Unix()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetTime))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"data": "after-backoff"})
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	// Override sleep to not actually wait.
	var sleptDuration time.Duration
	client.sleepFunc = func(d time.Duration) { sleptDuration = d }

	var result map[string]string
	modified, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !modified {
		t.Fatal("expected modified=true after retry")
	}
	if result["data"] != "after-backoff" {
		t.Fatalf("expected after-backoff, got %s", result["data"])
	}
	if sleptDuration == 0 {
		t.Fatal("expected sleep to be called for rate limit backoff")
	}
}

func TestClient_Get_RetryAfterHeader(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	var sleptDuration time.Duration
	client.sleepFunc = func(d time.Duration) { sleptDuration = d }

	var result map[string]string
	_, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sleptDuration != 2*time.Second {
		t.Fatalf("expected 2s sleep, got %v", sleptDuration)
	}
}

func TestClient_Get_ForbiddenNonRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	client.sleepFunc = func(d time.Duration) {}

	var result map[string]string
	_, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err == nil {
		t.Fatal("expected error for non-rate-limit 403")
	}
}

func TestClient_Get_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	var result map[string]string
	_, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

// contextIgnoringTransport wraps a transport to ignore context cancellation.
type contextIgnoringTransport struct {
	inner http.RoundTripper
}

func (t *contextIgnoringTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Strip the context to avoid cancellation.
	newReq := req.Clone(context.Background())
	return t.inner.RoundTrip(newReq)
}

func TestClient_Get_RequestError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately - httpClient.Do will fail.

	client := NewClient(testAuth("test-token"))
	var result map[string]string
	_, err := client.Get(ctx, "http://localhost:1/test", &result)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestClient_Get_ContextCanceledDuringRateLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	// Use a context-ignoring transport so requests succeed even after cancel.
	client.httpClient = &http.Client{
		Transport: &contextIgnoringTransport{inner: http.DefaultTransport},
	}
	// Cancel the context in sleepFunc. On the next loop iteration, the HTTP
	// request succeeds (transport ignores ctx), we get another 403, and
	// the select sees ctx.Done() ready.
	client.sleepFunc = func(d time.Duration) {
		cancel()
	}

	var result map[string]string
	_, err := client.Get(ctx, server.URL+"/test", &result)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestClient_Get_AuthError(t *testing.T) {
	a := &Auth{
		nowFunc: func() time.Time { return time.Now() },
	}
	a.tokenRefresh = func(ctx context.Context) (string, time.Time, error) {
		return "", time.Time{}, fmt.Errorf("auth broken")
	}

	client := NewClient(a)
	var result map[string]string
	_, err := client.Get(context.Background(), "http://example.com/test", &result)
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
}

func TestClient_Get_InvalidURL(t *testing.T) {
	client := NewClient(testAuth("test-token"))
	var result map[string]string
	// A URL with control characters is invalid for http.NewRequestWithContext.
	_, err := client.Get(context.Background(), "http://example.com/\x00", &result)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestClient_Get_ReadBodyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set Content-Length but close connection early to cause ReadAll error.
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("short"))
		// Connection will be closed after handler returns, causing io.ReadAll
		// to fail because it expects 1000 bytes but only gets 5.
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	var result map[string]string
	_, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err == nil {
		t.Fatal("expected error for truncated body")
	}
}

func TestClient_Get_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	var result map[string]string
	_, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestClient_Get_ETagEviction(t *testing.T) {
	// Pre-fill the ETag cache to maxETagEntries so the next successful GET
	// triggers the eviction path (line 166-170 of client.go).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"new-etag"`)
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode(map[string]string{"data": "ok"})
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))

	// Fill the ETag cache to the limit.
	client.etagMu.Lock()
	for i := 0; i < maxETagEntries; i++ {
		client.etags[fmt.Sprintf("url-%d", i)] = fmt.Sprintf("etag-%d", i)
	}
	client.etagMu.Unlock()

	var result map[string]string
	modified, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !modified {
		t.Fatal("expected modified=true")
	}

	// After eviction, the cache should be much smaller (new map with capacity
	// maxETagEntries/2, plus the one new entry).
	client.etagMu.RLock()
	size := len(client.etags)
	client.etagMu.RUnlock()
	if size >= maxETagEntries {
		t.Fatalf("expected etag cache to be evicted, but size is %d", size)
	}
}

func TestClient_RateLimitObserver(t *testing.T) {
	// Test the rate limit observer callback (covers the SetRateLimitObserver
	// callback path used by github_exporter run() line 232-234).
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	client.sleepFunc = func(d time.Duration) {} // don't actually sleep

	var observedKind string
	client.SetRateLimitObserver(func(kind string, _ time.Duration) {
		observedKind = kind
	})

	var result map[string]string
	_, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if observedKind == "" {
		t.Fatal("expected rate limit observer to be called")
	}
}

func TestClient_RateLimitSleep_NoResetHeader(t *testing.T) {
	// 403 with X-RateLimit-Remaining=0 but no X-RateLimit-Reset header.
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			// No X-RateLimit-Reset header
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	var sleptDuration time.Duration
	client.sleepFunc = func(d time.Duration) { sleptDuration = d }

	var result map[string]string
	_, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sleptDuration != 60*time.Second {
		t.Errorf("expected 60s default backoff, got %v", sleptDuration)
	}
}

func TestClient_RateLimitSleep_PastResetTime(t *testing.T) {
	// 403 with X-RateLimit-Reset in the past.
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			// Set reset time in the past.
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(-10*time.Second).Unix()))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	var sleptDuration time.Duration
	client.sleepFunc = func(d time.Duration) { sleptDuration = d }

	var result map[string]string
	_, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sleptDuration != 1*time.Second {
		t.Errorf("expected 1s min backoff for past reset time, got %v", sleptDuration)
	}
}

func TestClient_RateLimitRemaining(t *testing.T) {
	client := NewClient(testAuth("test-token"))
	if client.RateLimitRemaining() != 0 {
		t.Fatalf("expected 0 initial rate limit remaining, got %d", client.RateLimitRemaining())
	}

	client.rateLimitRemaining.Store(42)
	if client.RateLimitRemaining() != 42 {
		t.Fatalf("expected 42, got %d", client.RateLimitRemaining())
	}
}

// The regression test for the stall that stopped the exporter on 2026-08-11.
//
// A server that returns 403 forever used to hang Get() indefinitely: it slept
// toward the reset timestamp and retried, with no ceiling and no log line. The
// first poll never finished, poll_duration_seconds_count stayed at 0, and
// nothing in the logs or metrics said why. Without this test that behavior
// comes straight back, because it looks like correct backoff until you wait.
func TestClient_Get_RateLimitGivesUpRatherThanHanging(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix()))
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	var slept []time.Duration
	client.sleepFunc = func(d time.Duration) { slept = append(slept, d) }

	var result map[string]string
	_, err := client.Get(context.Background(), server.URL+"/test", &result)
	if err == nil {
		t.Fatal("expected an error once the retry budget is spent, got nil -- this is the hang")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected a rate-limit error naming the cause, got: %v", err)
	}
	// One initial attempt plus maxRateLimitRetry retries, then it gives up.
	if got := int(requestCount.Load()); got != maxRateLimitRetry+1 {
		t.Fatalf("expected %d requests, got %d", maxRateLimitRetry+1, got)
	}
	if len(slept) != maxRateLimitRetry {
		t.Fatalf("expected %d sleeps, got %d", maxRateLimitRetry, len(slept))
	}
}

// The reset timestamp GitHub reports for the primary limit can be most of an
// hour out. Sleeping that long inside a five-minute poll cycle is never right:
// the cap is what keeps one throttled URL from holding every collector behind
// it.
func TestClient_Get_RateLimitSleepIsCapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(45*time.Minute).Unix()))
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(testAuth("test-token"))
	var slept []time.Duration
	client.sleepFunc = func(d time.Duration) { slept = append(slept, d) }

	var result map[string]string
	_, _ = client.Get(context.Background(), server.URL+"/test", &result)

	if len(slept) == 0 {
		t.Fatal("expected at least one backoff")
	}
	for i, d := range slept {
		if d > maxRateLimitSleep {
			t.Fatalf("sleep %d was %s, above the %s ceiling", i, d, maxRateLimitSleep)
		}
	}
}
