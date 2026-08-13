package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/phaseshiftdata/prometheus_exporters/internal/discovery"
)

// TestCapabilitiesEndpoint_SerializationError covers the error branch in
// handleCapabilities when ToJSON fails. Since CapabilityMatrix.ToJSON()
// uses json.MarshalIndent which is unlikely to fail for valid matrices,
// we can at least verify the handler responds correctly for valid inputs.
func TestCapabilitiesEndpoint_ContentType(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	srv := NewServer(Config{MetricsPath: "/metrics"}, logger)

	// No matrix set
	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	w := httptest.NewRecorder()
	srv.handleCapabilities(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}
}

// TestCapabilitiesEndpoint_ToJSONError covers the error branch in
// handleCapabilities when matrix.ToJSON() fails. We trigger this by
// setting DiscoveredAt to a year outside the JSON-safe range [0,9999],
// which causes time.Time.MarshalJSON to return an error.
func TestCapabilitiesEndpoint_ToJSONError(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	srv := NewServer(Config{MetricsPath: "/metrics"}, logger)

	matrix := discovery.NewCapabilityMatrix()
	matrix.TokenValid = true
	// Year 10000 causes time.Time.MarshalJSON to fail.
	matrix.DiscoveredAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)

	srv.SetCapabilityMatrixFunc(func() *discovery.CapabilityMatrix { return matrix })

	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	w := httptest.NewRecorder()
	srv.handleCapabilities(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json content type, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "failed to serialize capabilities") {
		t.Fatalf("expected serialization error message, got %q", body)
	}
}
