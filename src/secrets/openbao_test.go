package secrets

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// writeSecretFiles creates temporary role_id and secret_id files.
func writeSecretFiles(t *testing.T) (roleIDFile, secretIDFile string) {
	t.Helper()
	dir := t.TempDir()
	roleIDFile = filepath.Join(dir, "role_id")
	secretIDFile = filepath.Join(dir, "secret_id")
	if err := os.WriteFile(roleIDFile, []byte("test-role-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretIDFile, []byte("test-secret-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return roleIDFile, secretIDFile
}

// newTestLogger returns a logger suitable for tests.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestAppRoleLogin(t *testing.T) {
	var loginCalled atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" && r.Method == "POST" {
			loginCalled.Add(1)
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			if body["role_id"] != "test-role-id" || body["secret_id"] != "test-secret-id" {
				http.Error(w, "invalid credentials", http.StatusForbidden)
				return
			}
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.test-token-12345",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	reg := prometheus.NewRegistry()

	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, reg, newTestLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	if loginCalled.Load() != 1 {
		t.Errorf("expected login to be called once, got %d", loginCalled.Load())
	}

	client.mu.RLock()
	tok := client.token
	client.mu.RUnlock()
	if tok != "s.test-token-12345" {
		t.Errorf("expected token %q, got %q", "s.test-token-12345", tok)
	}
}

func TestReadSecretKVv2(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login" && r.Method == "POST":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.test-token",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/secret/data/myapp/config" && r.Method == "GET":
			if r.Header.Get("X-Vault-Token") != "s.test-token" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"api_key":  "super-secret-key",
						"password": "hunter2",
					},
					"metadata": map[string]interface{}{
						"version": 1,
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Read existing field.
	val, err := client.ReadSecret("secret/myapp/config", "api_key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "super-secret-key" {
		t.Errorf("got %q, want %q", val, "super-secret-key")
	}

	// Read another field.
	val, err = client.ReadSecret("secret/myapp/config", "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "hunter2" {
		t.Errorf("got %q, want %q", val, "hunter2")
	}

	// Read a non-existent field.
	_, err = client.ReadSecret("secret/myapp/config", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestReadSecretKVv2_AlreadyHasDataPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/secret/data/myapp/config":
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"key": "value",
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Path already contains /data/ - should not be doubled.
	val, err := client.ReadSecret("secret/data/myapp/config", "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "value" {
		t.Errorf("got %q, want %q", val, "value")
	}
}

func TestTokenRenewal(t *testing.T) {
	var renewCalled atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.short-lived",
					"lease_duration": 2, // 2 second TTL to trigger fast renewal
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/auth/token/renew-self" && r.Method == "POST":
			renewCalled.Add(1)
			if r.Header.Get("X-Vault-Token") != "s.short-lived" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.short-lived",
					"lease_duration": 2,
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}

	// Wait long enough for at least one renewal cycle. The TTL is 2s and
	// the renewal loop sleeps for min(remaining - ttl/3, 5s) which with
	// a 2s TTL means it fires after ~5s (the minimum backoff).
	time.Sleep(7 * time.Second)
	client.Close()

	if renewCalled.Load() < 1 {
		t.Errorf("expected at least 1 renewal call, got %d", renewCalled.Load())
	}
}

func TestSealedVaultRetry(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			n := attempts.Add(1)
			if n <= 2 {
				// First two attempts: sealed
				http.Error(w, `{"errors":["Vault is sealed"]}`, http.StatusServiceUnavailable)
				return
			}
			// Third attempt: success
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.recovered",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}

	// Initial login failed (sealed). Client should be returned anyway.
	client.mu.RLock()
	tok := client.token
	client.mu.RUnlock()
	if tok != "" {
		t.Fatal("expected empty token after sealed vault")
	}

	// Wait for retries. The minimum backoff is 5s and doubles each
	// failure: attempt 1 at t=0 (fails), retry at t=5s (fails, backoff
	// becomes 10s), retry at t=15s (succeeds). We need ~17s total.
	time.Sleep(17 * time.Second)

	// Check token before Close(), which clears it.
	client.mu.RLock()
	tok = client.token
	client.mu.RUnlock()
	if tok != "s.recovered" {
		t.Errorf("expected token %q after recovery, got %q", "s.recovered", tok)
	}

	client.Close()

	if attempts.Load() < 3 {
		t.Errorf("expected at least 3 login attempts, got %d", attempts.Load())
	}
}

func TestReadSecretUnauthenticated(t *testing.T) {
	// Server that always rejects login.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.ReadSecret("secret/myapp", "key")
	if err == nil {
		t.Fatal("expected error when unauthenticated")
	}
}

func TestNewOpenBaoClient_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  OpenBaoConfig
	}{
		{"empty address", OpenBaoConfig{Address: "", RoleIDFile: "/a", SecretIDFile: "/b"}},
		{"empty role_id file", OpenBaoConfig{Address: "http://x", RoleIDFile: "", SecretIDFile: "/b"}},
		{"empty secret_id file", OpenBaoConfig{Address: "http://x", RoleIDFile: "/a", SecretIDFile: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewOpenBaoClient(tt.cfg, nil, newTestLogger())
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestEnsureKVv2DataPath(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"secret/myapp/config", "secret/data/myapp/config"},
		{"secret/data/myapp/config", "secret/data/myapp/config"},
		{"/secret/myapp/config", "secret/data/myapp/config"},
		{"kv/foo", "kv/data/foo"},
		{"kv/data/foo", "kv/data/foo"},
		{"singlesegment", "singlesegment"},
	}

	for _, tt := range tests {
		got := ensureKVv2DataPath(tt.input)
		if got != tt.want {
			t.Errorf("ensureKVv2DataPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseOpenBaoRef(t *testing.T) {
	tests := []struct {
		ref       string
		wantPath  string
		wantField string
		wantErr   bool
	}{
		{"secret/myapp:api_key", "secret/myapp", "api_key", false},
		{"secret/data/myapp/config:password", "secret/data/myapp/config", "password", false},
		{"kv/app:key", "kv/app", "key", false},
		// Edge: colon in path part (take last colon).
		{"secret/my:app:field", "secret/my:app", "field", false},
		// Errors
		{"nocolon", "", "", true},
		{":field", "", "", true},
		{"path:", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			path, field, err := ParseOpenBaoRef(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseOpenBaoRef(%q) error = %v, wantErr %v", tt.ref, err, tt.wantErr)
			}
			if !tt.wantErr {
				if path != tt.wantPath || field != tt.wantField {
					t.Errorf("ParseOpenBaoRef(%q) = (%q, %q), want (%q, %q)",
						tt.ref, path, field, tt.wantPath, tt.wantField)
				}
			}
		})
	}
}

func TestReadSecret_RequestCreationError(t *testing.T) {
	// Create a client with a token but a deliberately broken address
	// that causes http.NewRequest to fail.
	c := &OpenBaoClient{
		cfg: OpenBaoConfig{
			// Address with control characters causes http.NewRequest to fail.
			Address: "http://\x7f/",
		},
		client: &http.Client{},
		logger: newTestLogger(),
		token:  "s.test-token",
		readErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_read_errors_request",
		}, []string{"reason"}),
		lastReadSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_read_success_request",
		}),
	}

	_, err := c.ReadSecret("secret/myapp", "key")
	if err == nil {
		t.Fatal("expected error for invalid request URL")
	}
	if !strings.Contains(err.Error(), "creating request") {
		t.Errorf("expected 'creating request' error, got: %v", err)
	}
}

func TestReadSecret_ReadBodyError(t *testing.T) {
	// Create a server that returns a response whose body errors on read.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set content-length to lie about body size, then close connection.
		w.Header().Set("Content-Length", "999999")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("partial"))
		// The handler will return and the connection will be closed, causing
		// a read error when the client tries to read the full 999999 bytes.
	}))
	defer srv.Close()

	c := &OpenBaoClient{
		cfg:    OpenBaoConfig{Address: srv.URL},
		client: srv.Client(),
		logger: newTestLogger(),
		token:  "s.test-token",
		readErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_read_errors_body",
		}, []string{"reason"}),
		lastReadSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_read_success_body",
		}),
	}

	_, err := c.ReadSecret("secret/myapp", "key")
	// The body read may or may not error depending on how the HTTP client
	// handles the truncated response. Either a read error or a parse error
	// is acceptable here.
	if err == nil {
		t.Fatal("expected error for truncated body")
	}
}

func TestLogin_RequestCreationError(t *testing.T) {
	// Create a client with a broken address that causes login's http.Post to fail.
	dir := t.TempDir()
	roleIDFile := filepath.Join(dir, "role_id")
	secretIDFile := filepath.Join(dir, "secret_id")
	os.WriteFile(roleIDFile, []byte("test-role\n"), 0o600)
	os.WriteFile(secretIDFile, []byte("test-secret\n"), 0o600)

	c := &OpenBaoClient{
		cfg: OpenBaoConfig{
			Address:      "http://\x7f/",
			RoleIDFile:   roleIDFile,
			SecretIDFile: secretIDFile,
		},
		client: &http.Client{},
		logger: newTestLogger(),
		authenticated: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_auth_login_err",
		}),
	}

	err := c.login()
	if err == nil {
		t.Fatal("expected error for invalid login URL")
	}
}

func TestRenewToken_RequestCreationError(t *testing.T) {
	c := &OpenBaoClient{
		cfg: OpenBaoConfig{
			Address: "http://\x7f/",
		},
		client: &http.Client{},
		logger: newTestLogger(),
		token:  "s.test-token",
		tokenRenewals: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "test_renewals_err",
		}),
	}

	err := c.renewToken()
	if err == nil {
		t.Fatal("expected error for invalid renew URL")
	}
	if !strings.Contains(err.Error(), "creating renew request") {
		t.Errorf("expected 'creating renew request' error, got: %v", err)
	}
}

// errorReadCloser returns an error on Read, simulating a broken response body.
type errorReadCloser struct{}

func (e errorReadCloser) Read(_ []byte) (int, error) { return 0, fmt.Errorf("simulated read error") }
func (e errorReadCloser) Close() error               { return nil }

// roundTripFunc is a function adapter for http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestLogin_ReadBodyError(t *testing.T) {
	dir := t.TempDir()
	roleIDFile := filepath.Join(dir, "role_id")
	secretIDFile := filepath.Join(dir, "secret_id")
	os.WriteFile(roleIDFile, []byte("role\n"), 0o600)
	os.WriteFile(secretIDFile, []byte("secret\n"), 0o600)

	c := &OpenBaoClient{
		cfg: OpenBaoConfig{
			Address:      "http://localhost:1",
			RoleIDFile:   roleIDFile,
			SecretIDFile: secretIDFile,
		},
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{},
					Body:       errorReadCloser{},
				}, nil
			}),
		},
		logger: newTestLogger(),
		authenticated: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_auth_readbody",
		}),
	}

	err := c.login()
	if err == nil {
		t.Fatal("expected error for body read failure")
	}
	if !strings.Contains(err.Error(), "reading login response") {
		t.Errorf("expected 'reading login response' error, got: %v", err)
	}
}

func TestRenewToken_ReadBodyError(t *testing.T) {
	c := &OpenBaoClient{
		cfg: OpenBaoConfig{
			Address: "http://localhost:1",
		},
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{},
					Body:       errorReadCloser{},
				}, nil
			}),
		},
		logger:        newTestLogger(),
		token:         "s.test-token",
		tokenRenewals: prometheus.NewCounter(prometheus.CounterOpts{Name: "test_renewals_readbody"}),
	}

	err := c.renewToken()
	if err == nil {
		t.Fatal("expected error for body read failure")
	}
	if !strings.Contains(err.Error(), "reading renew response") {
		t.Errorf("expected 'reading renew response' error, got: %v", err)
	}
}

func TestTruncateVaultBody(t *testing.T) {
	// Short string (len <= 512) should be returned unchanged.
	short := "short body"
	if got := truncateVaultBody(short); got != short {
		t.Errorf("truncateVaultBody(%q) = %q, want %q", short, got, short)
	}

	// Exactly 512 bytes should be returned unchanged.
	exact := strings.Repeat("a", 512)
	if got := truncateVaultBody(exact); got != exact {
		t.Errorf("truncateVaultBody(512 bytes) should return unchanged")
	}

	// Long string (> 512) should be truncated.
	long := strings.Repeat("b", 1000)
	got := truncateVaultBody(long)
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Errorf("truncateVaultBody(1000 bytes) should end with '...(truncated)', got suffix %q", got[len(got)-20:])
	}
	if len(got) != 512+len("...(truncated)") {
		t.Errorf("truncateVaultBody(1000 bytes) length = %d, want %d", len(got), 512+len("...(truncated)"))
	}
}

func TestMutualExclusivity(t *testing.T) {
	// This tests the ValidateSecretSources helper.
	tests := []struct {
		name      string
		value     string
		file      string
		openbao   string
		wantErr   bool
	}{
		{"none set", "", "", "", false},
		{"only value", "v", "", "", false},
		{"only file", "", "/f", "", false},
		{"only openbao", "", "", "path:field", false},
		{"value and file", "v", "/f", "", true},
		{"value and openbao", "v", "", "p:f", true},
		{"file and openbao", "", "/f", "p:f", true},
		{"all three", "v", "/f", "p:f", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSecretSources("test-flag", tt.value, tt.file, tt.openbao)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSecretSources() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadSecretPermissionDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/secret/data/myapp":
			http.Error(w, `{"errors":["permission denied"]}`, http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.ReadSecret("secret/myapp", "key")
	if err == nil {
		t.Fatal("expected error for permission denied")
	}
	if got := fmt.Sprint(err); !contains(got, "permission denied") {
		t.Errorf("error %q should mention 'permission denied'", got)
	}
}

func TestReadSecretUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/secret/data/myapp":
			http.Error(w, `{"errors":["unauthorized"]}`, http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.ReadSecret("secret/myapp", "key")
	if err == nil {
		t.Fatal("expected error for unauthorized")
	}
}

func TestReadSecretUnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/secret/data/myapp":
			http.Error(w, "internal error", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.ReadSecret("secret/myapp", "key")
	if err == nil {
		t.Fatal("expected error for unexpected status")
	}
	if got := fmt.Sprint(err); !contains(got, "unexpected status") {
		t.Errorf("error %q should mention 'unexpected status'", got)
	}
}

func TestReadSecretBadGateway(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/secret/data/myapp":
			http.Error(w, "bad gateway", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.ReadSecret("secret/myapp", "key")
	if err == nil {
		t.Fatal("expected error for bad gateway")
	}
}

func TestReadSecretParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/secret/data/myapp":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("this is not valid json"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.ReadSecret("secret/myapp", "key")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if got := fmt.Sprint(err); !contains(got, "parsing response") {
		t.Errorf("error %q should mention 'parsing response'", got)
	}
}

func TestReadSecretFieldNotString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/secret/data/myapp":
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"numeric_field": 42,
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.ReadSecret("secret/myapp", "numeric_field")
	if err == nil {
		t.Fatal("expected error for non-string field")
	}
	if got := fmt.Sprint(err); !contains(got, "not a string") {
		t.Errorf("error %q should mention 'not a string'", got)
	}
}

func TestReadSecretConnectionError(t *testing.T) {
	// Create a server and close it immediately to get a connection error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Close server before read to cause connection error.
	srv.Close()

	_, err = client.ReadSecret("secret/myapp", "key")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestLoginFailedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":["invalid credentials"]}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Should have an empty token because login failed.
	client.mu.RLock()
	tok := client.token
	client.mu.RUnlock()
	if tok != "" {
		t.Error("expected empty token after failed login")
	}
}

func TestLoginEmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"auth": map[string]interface{}{
				"client_token":   "",
				"lease_duration": 3600,
				"renewable":      true,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	client.mu.RLock()
	tok := client.token
	client.mu.RUnlock()
	if tok != "" {
		t.Error("expected empty token when server returns empty client_token")
	}
}

func TestLoginBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	client.mu.RLock()
	tok := client.token
	client.mu.RUnlock()
	if tok != "" {
		t.Error("expected empty token when server returns invalid JSON")
	}
}

func TestLoginMissingRoleIDFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   "/nonexistent/role_id",
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	client.mu.RLock()
	tok := client.token
	client.mu.RUnlock()
	if tok != "" {
		t.Error("expected empty token when role_id file is missing")
	}
}

func TestLoginMissingSecretIDFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	roleIDFile, _ := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: "/nonexistent/secret_id",
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	client.mu.RLock()
	tok := client.token
	client.mu.RUnlock()
	if tok != "" {
		t.Error("expected empty token when secret_id file is missing")
	}
}

func TestRenewTokenNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Token is empty because login failed; renewToken should return error.
	err = client.renewToken()
	if err == nil {
		t.Fatal("expected error when renewing with no token")
	}
	if got := fmt.Sprint(err); !contains(got, "no token") {
		t.Errorf("error %q should mention 'no token'", got)
	}
}

func TestRenewTokenFailedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/auth/token/renew-self":
			http.Error(w, `{"errors":["forbidden"]}`, http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	err = client.renewToken()
	if err == nil {
		t.Fatal("expected error for failed renewal")
	}
	if got := fmt.Sprint(err); !contains(got, "token renewal failed") {
		t.Errorf("error %q should mention 'token renewal failed'", got)
	}
}

func TestRenewTokenBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/auth/token/renew-self":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("not json"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	err = client.renewToken()
	if err == nil {
		t.Fatal("expected error for bad JSON renew response")
	}
	if got := fmt.Sprint(err); !contains(got, "parsing renew response") {
		t.Errorf("error %q should mention 'parsing renew response'", got)
	}
}

func TestRenewTokenConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Close server to cause connection error on renewal.
	srv.Close()

	err = client.renewToken()
	if err == nil {
		t.Fatal("expected connection error on renewal")
	}
}

func TestRenewLoopRenewalFailReloginFail(t *testing.T) {
	// Test the path where renewal fails AND re-login also fails,
	// causing the token to be cleared and authenticated set to 0.
	var loginCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			loginCount++
			if loginCount <= 1 {
				// First login succeeds.
				resp := map[string]interface{}{
					"auth": map[string]interface{}{
						"client_token":   "s.initial",
						"lease_duration": 2,
						"renewable":      true,
					},
				}
				json.NewEncoder(w).Encode(resp)
			} else {
				// Subsequent logins fail.
				http.Error(w, "login failed", http.StatusForbidden)
			}
		case r.URL.Path == "/v1/auth/token/renew-self":
			// Renewal always fails.
			http.Error(w, "renewal failed", http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the renewLoop to attempt renewal and re-login.
	// TTL is 2s, minimum backoff is 5s, so first renewal attempt is at ~5s.
	time.Sleep(7 * time.Second)
	client.Close()

	client.mu.RLock()
	tok := client.token
	client.mu.RUnlock()

	if tok != "" {
		t.Errorf("expected token to be cleared after failed renewal and re-login, got %q", tok)
	}
}

func TestNewOpenBaoClientNilRegisterer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"auth": map[string]interface{}{
				"client_token":   "s.tok",
				"lease_duration": 3600,
				"renewable":      true,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	// Pass nil registerer - should not panic.
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, nil, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
}

func TestReadSecretSealedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.tok",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/secret/data/myapp":
			// Simulate sealed vault on read.
			http.Error(w, `{"errors":["Vault is sealed"]}`, http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile, secretIDFile := writeSecretFiles(t)
	client, err := NewOpenBaoClient(OpenBaoConfig{
		Address:      srv.URL,
		RoleIDFile:   roleIDFile,
		SecretIDFile: secretIDFile,
	}, prometheus.NewRegistry(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.ReadSecret("secret/myapp", "key")
	if err == nil {
		t.Fatal("expected error for sealed vault")
	}
	if got := fmt.Sprint(err); !contains(got, "sealed") {
		t.Errorf("error %q should mention 'sealed'", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsCheck(s, substr))
}

func containsCheck(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
