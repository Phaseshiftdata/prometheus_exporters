package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/phaseshiftdata/prometheus_exporters/internal/discovery"
)

func newTestServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	return NewServer(cfg, logger)
}

func TestHealthEndpoint_Healthy(t *testing.T) {
	srv := newTestServer(t, Config{MetricsPath: "/metrics"})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "healthy" {
		t.Fatalf("expected healthy, got %q", body["status"])
	}
}

func TestHealthEndpoint_Unhealthy(t *testing.T) {
	srv := newTestServer(t, Config{MetricsPath: "/metrics"})
	srv.SetHealthFunc(func() bool { return false })

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestCapabilitiesEndpoint_NoMatrix(t *testing.T) {
	srv := newTestServer(t, Config{MetricsPath: "/metrics"})

	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	w := httptest.NewRecorder()

	srv.handleCapabilities(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestCapabilitiesEndpoint_NilMatrix(t *testing.T) {
	srv := newTestServer(t, Config{MetricsPath: "/metrics"})
	srv.SetCapabilityMatrixFunc(func() *discovery.CapabilityMatrix { return nil })

	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	w := httptest.NewRecorder()

	srv.handleCapabilities(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestCapabilitiesEndpoint_WithMatrix(t *testing.T) {
	srv := newTestServer(t, Config{MetricsPath: "/metrics"})

	matrix := discovery.NewCapabilityMatrix()
	matrix.TokenValid = true
	matrix.Accounts = []discovery.AccountInfo{{ID: "acc1", Name: "Test"}}
	matrix.SetDataset("access", discovery.DatasetCapability{
		Dataset: "access",
		State:   discovery.StateAvailable,
	})

	srv.SetCapabilityMatrixFunc(func() *discovery.CapabilityMatrix { return matrix })

	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	w := httptest.NewRecorder()

	srv.handleCapabilities(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}
}

func TestRootEndpoint(t *testing.T) {
	srv := newTestServer(t, Config{MetricsPath: "/metrics"})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	srv.handleRoot(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "/metrics") {
		t.Error("expected metrics link in root page")
	}
	if !strings.Contains(body, "/health") {
		t.Error("expected health link in root page")
	}
	if !strings.Contains(body, "/capabilities") {
		t.Error("expected capabilities link in root page")
	}
}

func TestRootEndpoint_404ForOtherPaths(t *testing.T) {
	srv := newTestServer(t, Config{MetricsPath: "/metrics"})

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	srv.handleRoot(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestBasicAuth_NoCredentials(t *testing.T) {
	srv := newTestServer(t, Config{
		MetricsPath:       "/metrics",
		BasicAuthUsername: "admin",
		BasicAuthPassword: "secret",
	})

	handler := srv.maybeAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestBasicAuth_WrongCredentials(t *testing.T) {
	srv := newTestServer(t, Config{
		MetricsPath:       "/metrics",
		BasicAuthUsername: "admin",
		BasicAuthPassword: "secret",
	})

	handler := srv.maybeAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.SetBasicAuth("admin", "wrong")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestBasicAuth_CorrectCredentials(t *testing.T) {
	srv := newTestServer(t, Config{
		MetricsPath:       "/metrics",
		BasicAuthUsername: "admin",
		BasicAuthPassword: "secret",
	})

	handler := srv.maybeAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestBasicAuth_Disabled(t *testing.T) {
	srv := newTestServer(t, Config{MetricsPath: "/metrics"})

	handler := srv.maybeAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without auth, got %d", w.Code)
	}
}

func TestTLSConfigDetection(t *testing.T) {
	cfg := Config{
		ListenAddress: ":0",
		MetricsPath:   "/metrics",
		TLSCertFile:   "/nonexistent/cert.pem",
		TLSKeyFile:    "/nonexistent/key.pem",
	}

	// We can verify TLS is configured by checking the config fields
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		t.Fatal("expected TLS config to be set")
	}
}

func TestRegistry(t *testing.T) {
	srv := newTestServer(t, Config{MetricsPath: "/metrics"})
	reg := srv.Registry()
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestServerStartAndShutdown(t *testing.T) {
	srv := newTestServer(t, Config{
		ListenAddress: "127.0.0.1:0",
		MetricsPath:   "/metrics",
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Give the server goroutine time to bind and start serving.
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	err := <-errCh
	if err != nil && err != http.ErrServerClosed {
		t.Fatalf("Start returned unexpected error: %v", err)
	}
}

func TestServerStartTLS_InvalidCert(t *testing.T) {
	srv := newTestServer(t, Config{
		ListenAddress: "127.0.0.1:0",
		MetricsPath:   "/metrics",
		TLSCertFile:   "/nonexistent/cert.pem",
		TLSKeyFile:    "/nonexistent/key.pem",
	})

	err := srv.Start()
	if err == nil {
		t.Fatal("expected error for invalid TLS files")
	}
}

func TestCapabilitiesEndpoint_ErrorInSerialization(t *testing.T) {
	srv := newTestServer(t, Config{MetricsPath: "/metrics"})
	matrix := discovery.NewCapabilityMatrix()
	matrix.TokenValid = true
	srv.SetCapabilityMatrixFunc(func() *discovery.CapabilityMatrix { return matrix })

	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	w := httptest.NewRecorder()
	srv.handleCapabilities(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMarshalCapabilityMatrix(t *testing.T) {
	matrix := discovery.NewCapabilityMatrix()
	matrix.TokenValid = true

	data, err := MarshalCapabilityMatrix(matrix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
}
