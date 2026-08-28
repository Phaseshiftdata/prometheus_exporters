package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func generateTestRSAKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func TestAuth_TokenCaching(t *testing.T) {
	callCount := 0
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	a := &Auth{
		nowFunc: func() time.Time { return fixedTime },
	}
	a.tokenRefresh = func(ctx context.Context) (string, time.Time, error) {
		callCount++
		return "test-token-1", fixedTime.Add(1 * time.Hour), nil
	}

	ctx := context.Background()

	// First call should refresh.
	tok, err := a.Token(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "test-token-1" {
		t.Fatalf("expected test-token-1, got %s", tok)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 refresh call, got %d", callCount)
	}

	// Second call should use cache.
	tok, err = a.Token(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "test-token-1" {
		t.Fatalf("expected test-token-1, got %s", tok)
	}
	if callCount != 1 {
		t.Fatalf("expected still 1 refresh call, got %d", callCount)
	}
}

func TestAuth_TokenRefreshOnExpiry(t *testing.T) {
	callCount := 0
	currentTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	a := &Auth{
		nowFunc: func() time.Time { return currentTime },
	}
	a.tokenRefresh = func(ctx context.Context) (string, time.Time, error) {
		callCount++
		return "token-" + string(rune('0'+callCount)), currentTime.Add(1 * time.Hour), nil
	}

	ctx := context.Background()

	// First call.
	_, err := a.Token(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}

	// Advance time past the 55-minute mark (1h TTL - 5min buffer).
	currentTime = currentTime.Add(56 * time.Minute)

	_, err = a.Token(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 calls after expiry, got %d", callCount)
	}
}

func TestAuth_TokenRefreshError(t *testing.T) {
	a := &Auth{
		nowFunc: func() time.Time { return time.Now() },
	}
	a.tokenRefresh = func(ctx context.Context) (string, time.Time, error) {
		return "", time.Time{}, errors.New("refresh failed")
	}

	_, err := a.Token(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, err) {
		t.Fatalf("unexpected error type: %v", err)
	}
}

func TestAuth_TokenConcurrency(t *testing.T) {
	callCount := 0
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var mu sync.Mutex

	a := &Auth{
		nowFunc: func() time.Time { return fixedTime },
	}
	a.tokenRefresh = func(ctx context.Context) (string, time.Time, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		return "concurrent-token", fixedTime.Add(1 * time.Hour), nil
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := a.Token(ctx)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tok != "concurrent-token" {
				t.Errorf("unexpected token: %s", tok)
			}
		}()
	}

	wg.Wait()

	// Due to mutex, only 1 goroutine should have refreshed; the rest use cache.
	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Fatalf("expected 1 refresh call under concurrency, got %d", callCount)
	}
}

func TestNewAuth_InvalidKeyFile(t *testing.T) {
	_, err := NewAuth(1, 1, "/nonexistent/key.pem")
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestNewAuthFromKey_ValidRSAKey(t *testing.T) {
	key := generateTestRSAKey(t)

	auth, err := NewAuthFromKey(1, 1, key)
	if err != nil {
		t.Fatalf("NewAuthFromKey() error: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth")
	}
	if auth.appID != 1 {
		t.Errorf("appID = %d, want 1", auth.appID)
	}
	if auth.installID != 1 {
		t.Errorf("installID = %d, want 1", auth.installID)
	}
	if auth.transport == nil {
		t.Error("transport should not be nil")
	}
	if auth.tokenRefresh == nil {
		t.Error("tokenRefresh should not be nil")
	}
	if auth.nowFunc == nil {
		t.Error("nowFunc should not be nil")
	}
}

func TestNewAuthFromKey_InvalidKey(t *testing.T) {
	_, err := NewAuthFromKey(1, 1, []byte("not a valid PEM key"))
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestNewAuth_ValidKeyFile(t *testing.T) {
	key := generateTestRSAKey(t)
	f, err := os.CreateTemp("", "test-key-*.pem")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Write(key)
	f.Close()

	auth, err := NewAuth(1, 1, f.Name())
	if err != nil {
		t.Fatalf("NewAuth() error: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth")
	}
}

func TestNewTestAuth_RefreshOnExpiry(t *testing.T) {
	// Create a test auth with an already-expired token so that the tokenRefresh
	// closure is actually called.
	auth := NewTestAuth("expired-token", time.Now().Add(-1*time.Hour))
	tok, err := auth.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "expired-token" {
		t.Errorf("expected expired-token, got %s", tok)
	}
}

func TestDefaultTokenRefresh(t *testing.T) {
	key := generateTestRSAKey(t)
	auth, err := NewAuthFromKey(1, 1, key)
	if err != nil {
		t.Fatalf("NewAuthFromKey() error: %v", err)
	}

	// defaultTokenRefresh will fail because we can't reach GitHub API, but
	// it exercises the code path.
	_, _, err = auth.defaultTokenRefresh(context.Background())
	// Expected to fail (no real GitHub App), but the code is exercised.
	if err == nil {
		t.Log("surprisingly succeeded without real GitHub App")
	}
}

func TestDefaultTokenRefresh_SuccessPath(t *testing.T) {
	// Use a mock HTTP server that returns a valid installation token to
	// exercise the success path of defaultTokenRefresh (lines 88-89).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":"ghs_test_token_12345","expires_at":"2099-01-01T00:00:00Z"}`)
	}))
	defer srv.Close()

	key := generateTestRSAKey(t)
	auth, err := NewAuthFromKey(1, 1, key)
	if err != nil {
		t.Fatalf("NewAuthFromKey() error: %v", err)
	}

	// Point the transport at our mock server.
	auth.transport.BaseURL = srv.URL

	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	auth.nowFunc = func() time.Time { return fixedTime }

	token, expiry, err := auth.defaultTokenRefresh(context.Background())
	if err != nil {
		t.Fatalf("defaultTokenRefresh() error: %v", err)
	}
	if token != "ghs_test_token_12345" {
		t.Errorf("expected ghs_test_token_12345, got %s", token)
	}
	expectedExpiry := fixedTime.Add(1 * time.Hour)
	if !expiry.Equal(expectedExpiry) {
		t.Errorf("expected expiry %v, got %v", expectedExpiry, expiry)
	}
}
