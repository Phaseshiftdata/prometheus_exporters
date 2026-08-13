package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/asymmetric-effort/prometheus-exporters/internal/discovery"
)

func TestServer_StartAndShutdown(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	srv := NewServer(Config{
		ListenAddress: ":0",
		MetricsPath:   "/metrics",
	}, logger)

	// Start in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Verify Start returned
	err := <-errCh
	if err != nil && err != http.ErrServerClosed {
		t.Fatalf("Start returned unexpected error: %v", err)
	}
}

func TestServer_SetHealthFunc(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	srv := NewServer(Config{MetricsPath: "/metrics"}, logger)

	// Default is healthy
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.handleHealth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Set unhealthy
	srv.SetHealthFunc(func() bool { return false })
	w = httptest.NewRecorder()
	srv.handleHealth(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestServer_SetCapabilityMatrixFunc(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	srv := NewServer(Config{MetricsPath: "/metrics"}, logger)

	matrix := discovery.NewCapabilityMatrix()
	matrix.TokenValid = true
	matrix.Accounts = []discovery.AccountInfo{{ID: "a1", Name: "Test"}}

	srv.SetCapabilityMatrixFunc(func() *discovery.CapabilityMatrix { return matrix })

	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	w := httptest.NewRecorder()
	srv.handleCapabilities(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServer_MetricsEndpoint(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	srv := NewServer(Config{
		ListenAddress: ":0",
		MetricsPath:   "/metrics",
	}, logger)

	// Start server
	go srv.Start()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer srv.Shutdown(ctx)
}

func TestMarshalCapabilityMatrix_NilMatrix(t *testing.T) {
	// Should handle nil by returning null
	data, err := MarshalCapabilityMatrix(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "null" {
		t.Fatalf("expected 'null', got %s", string(data))
	}
}
