package secrets

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	client.Close()

	client.mu.RLock()
	tok = client.token
	client.mu.RUnlock()
	if tok != "s.recovered" {
		t.Errorf("expected token %q after recovery, got %q", "s.recovered", tok)
	}

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
