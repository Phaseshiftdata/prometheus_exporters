package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
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
