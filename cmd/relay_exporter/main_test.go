package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/exporter"
)

// --- Source IP filtering ---

func TestSourceIPAllowed(t *testing.T) {
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("test_metric 1\n")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestSourceIPDenied(t *testing.T) {
	sem := make(chan struct{}, 10)
	handler := metricsHandler("192.168.1.1", &http.Client{Timeout: time.Second}, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

// --- Allowed target validation ---

func TestAllowedTargetValid(t *testing.T) {
	valid := []string{
		"10.0.0.1",
		"10.255.255.255",
		"172.16.0.1",
		"172.31.255.255",
		"192.168.0.1",
		"192.168.255.255",
		"127.0.0.1",   // loopback
		"127.0.0.2",   // loopback
		"127.255.255.254", // loopback upper bound
	}
	for _, ip := range valid {
		parsed := net.ParseIP(ip)
		if !isAllowedTarget(parsed) {
			t.Errorf("%s should be an allowed target", ip)
		}
	}
}

func TestAllowedTargetInvalid(t *testing.T) {
	invalid := []string{
		"203.0.113.1",   // RFC 5737 TEST-NET-3
		"203.0.113.50",  // RFC 5737
		"8.8.8.8",       // public
		"169.254.1.1",   // link-local
		"172.32.0.1",    // just outside 172.16/12
		"11.0.0.1",      // just outside 10/8
		"192.167.1.1",   // just outside 192.168/16
	}
	for _, ip := range invalid {
		parsed := net.ParseIP(ip)
		if isAllowedTarget(parsed) {
			t.Errorf("%s should NOT be an allowed target", ip)
		}
	}
}

func TestAllowedTargetIPv6(t *testing.T) {
	parsed := net.ParseIP("::1")
	if isAllowedTarget(parsed) {
		t.Error("IPv6 loopback should not be an allowed target")
	}
}

// --- Port validation ---

func TestPortValidation(t *testing.T) {
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("up 1\n")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	tests := []struct {
		port   string
		status int
	}{
		{"1", http.StatusOK},
		{"65535", http.StatusOK},
		{"0", http.StatusBadRequest},
		{"-1", http.StatusBadRequest},
		{"65536", http.StatusBadRequest},
		{"abc", http.StatusBadRequest},
		{"", http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run("port="+tc.port, func(t *testing.T) {
			url := "/metrics?ip=10.0.0.1"
			if tc.port != "" {
				url += "&port=" + tc.port
			}
			req := httptest.NewRequest("GET", url, nil)
			req.RemoteAddr = "127.0.0.1:12345"
			rr := httptest.NewRecorder()
			handler(rr, req)

			if rr.Code != tc.status {
				t.Errorf("port=%s: expected %d, got %d", tc.port, tc.status, rr.Code)
			}
		})
	}
}

func TestMissingPort(t *testing.T) {
	sem := make(chan struct{}, 10)
	handler := metricsHandler("127.0.0.1", &http.Client{Timeout: time.Second}, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// --- TLS parameter ---

func TestTLSParameterTrue(t *testing.T) {
	sem := make(chan struct{}, 10)
	var gotScheme string
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotScheme = r.URL.Scheme
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("up 1\n")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100&tls=true", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotScheme != "https" {
		t.Errorf("expected https scheme when tls=true, got %q", gotScheme)
	}
}

func TestTLSParameterFalse(t *testing.T) {
	sem := make(chan struct{}, 10)
	var gotScheme string
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotScheme = r.URL.Scheme
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("up 1\n")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100&tls=false", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotScheme != "http" {
		t.Errorf("expected http scheme when tls=false, got %q", gotScheme)
	}
}

func TestTLSParameterMissingDefaultsFalse(t *testing.T) {
	sem := make(chan struct{}, 10)
	var gotScheme string
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotScheme = r.URL.Scheme
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("up 1\n")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotScheme != "http" {
		t.Errorf("expected http scheme when tls is missing, got %q", gotScheme)
	}
}

func TestTLSParameterInvalid(t *testing.T) {
	sem := make(chan struct{}, 10)
	handler := metricsHandler("127.0.0.1", &http.Client{Timeout: time.Second}, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100&tls=maybe", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// --- Proxy behavior ---

func TestProxySuccess(t *testing.T) {
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("target_metric 42\n")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "target_metric 42") {
		t.Error("response should contain target metrics")
	}
	if !strings.Contains(body, "relay_response 1") {
		t.Error("response should contain relay_response 1")
	}
	if !strings.Contains(body, "relay_target_response 1") {
		t.Error("response should contain relay_target_response 1")
	}
	if !strings.Contains(body, "relay_target_http_status 200") {
		t.Error("response should contain relay_target_http_status 200")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestAuthorizationHeaderForwarding(t *testing.T) {
	var gotAuth string
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotAuth = r.Header.Get("Authorization")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok\n")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer test-token-123")
	rr := httptest.NewRecorder()
	handler(rr, req)

	if gotAuth != "Bearer test-token-123" {
		t.Errorf("expected Authorization header to be forwarded, got %q", gotAuth)
	}
}

// --- Timeout handling ---

func TestTimeoutHandling(t *testing.T) {
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			time.Sleep(500 * time.Millisecond)
			return nil, fmt.Errorf("timeout")
		}),
		Timeout: 100 * time.Millisecond,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (relay functioning), got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "relay_target_response 0") {
		t.Error("expected relay_target_response 0 on timeout")
	}
	if !strings.Contains(body, "relay_target_http_status 0") {
		t.Error("expected relay_target_http_status 0 on timeout")
	}
}

// --- Concurrent request limiting ---

func TestConcurrentRequestLimiting(t *testing.T) {
	sem := make(chan struct{}, 1)

	// Fill the semaphore.
	sem <- struct{}{}

	handler := metricsHandler("127.0.0.1", &http.Client{Timeout: time.Second}, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}

	// Drain the semaphore.
	<-sem
}

// --- Response format ---

func TestResponseFormat(t *testing.T) {
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("up 1\n")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	body := rr.Body.String()

	expected := []string{
		"# HELP relay_response",
		"# TYPE relay_response gauge",
		"relay_response 1",
		"# HELP relay_target_response",
		"# TYPE relay_target_response gauge",
		"relay_target_response 1",
		"# HELP relay_target_http_status",
		"# TYPE relay_target_http_status gauge",
		"relay_target_http_status 200",
		"# HELP relay_duration_seconds",
		"# TYPE relay_duration_seconds gauge",
		"relay_duration_seconds",
	}

	for _, s := range expected {
		if !strings.Contains(body, s) {
			t.Errorf("response missing %q", s)
		}
	}
}

func TestResponseFormatTargetFailure(t *testing.T) {
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "relay_response 1") {
		t.Error("relay should always report relay_response 1")
	}
	if !strings.Contains(body, "relay_target_response 0") {
		t.Error("expected relay_target_response 0")
	}
	if !strings.Contains(body, "relay_target_http_status 0") {
		t.Error("expected relay_target_http_status 0")
	}
}

// --- Health endpoint ---

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	healthHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "ok\n" {
		t.Errorf("expected 'ok\\n', got %q", rr.Body.String())
	}
}

// --- Landing page ---

func TestLandingPage(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	landingHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Relay Exporter") {
		t.Error("landing page should contain 'Relay Exporter'")
	}
	if !strings.Contains(body, "/metrics") {
		t.Error("landing page should link to /metrics")
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type, got %q", ct)
	}
}

// --- rootCmd tests ---

func TestRootCmd(t *testing.T) {
	cmd := rootCmd()
	if cmd.Use != "relay_exporter" {
		t.Errorf("expected Use 'relay_exporter', got %q", cmd.Use)
	}
	if cmd.Version == "" {
		t.Error("empty version")
	}
}

func TestRootCmdExecute(t *testing.T) {
	cmd := rootCmd()
	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--allowed-source", "127.0.0.1", "--listen-address", "127.0.0.1:0"})

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("cmd.Execute returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("cmd did not shut down in time")
	}
}

func TestSetupLogging(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "invalid"} {
		exporter.SetupLogging(level) // should not panic
	}
}

func TestExecuteReturnsZero(t *testing.T) {
	code := exporter.Execute(func() *cobra.Command {
		cmd := rootCmd()
		cmd.SetArgs([]string{"--help"})
		return cmd
	})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestExecuteReturnsOneOnError(t *testing.T) {
	code := exporter.Execute(func() *cobra.Command {
		cmd := rootCmd()
		cmd.SetArgs([]string{"--unknown-flag"})
		return cmd
	})
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

// --- Missing --allowed-source ---

func TestMissingAllowedSourceFailsStartup(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"--listen-address", "127.0.0.1:0"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when --allowed-source is missing")
	}
}

func TestInvalidAllowedSource(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"--allowed-source", "not-an-ip"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid --allowed-source")
	}
}

// --- Missing ip parameter ---

func TestMissingIPParameter(t *testing.T) {
	sem := make(chan struct{}, 10)
	handler := metricsHandler("127.0.0.1", &http.Client{Timeout: time.Second}, sem)

	req := httptest.NewRequest("GET", "/metrics?port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// --- Invalid ip parameter ---

func TestInvalidIPParameter(t *testing.T) {
	sem := make(chan struct{}, 10)
	handler := metricsHandler("127.0.0.1", &http.Client{Timeout: time.Second}, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=not-an-ip&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// --- Public IP rejected ---

func TestPublicIPRejected(t *testing.T) {
	sem := make(chan struct{}, 10)
	handler := metricsHandler("127.0.0.1", &http.Client{Timeout: time.Second}, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=203.0.113.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// --- extractSourceIP ---

func TestExtractSourceIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"127.0.0.1:12345", "127.0.0.1"},
		{"[::1]:12345", "::1"},
		{"127.0.0.1", "127.0.0.1"},
	}
	for _, tc := range tests {
		got := extractSourceIP(tc.input)
		if got != tc.expected {
			t.Errorf("extractSourceIP(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// --- buildTargetTLSConfig ---

func TestBuildTargetTLSConfigDefault(t *testing.T) {
	cfg, err := buildTargetTLSConfig("", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be false")
	}
}

func TestBuildTargetTLSConfigSkipVerify(t *testing.T) {
	cfg, err := buildTargetTLSConfig("", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestBuildTargetTLSConfigBadCAFile(t *testing.T) {
	_, err := buildTargetTLSConfig("/nonexistent/ca.pem", false)
	if err == nil {
		t.Error("expected error for nonexistent CA cert")
	}
}

// --- Target returning non-200 ---

func TestTargetNon200Status(t *testing.T) {
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("error\n")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "relay_target_response 0") {
		t.Error("expected relay_target_response 0 for non-200 target")
	}
	if !strings.Contains(body, "relay_target_http_status 500") {
		t.Error("expected relay_target_http_status 500")
	}
}

// --- Target body without trailing newline ---

func TestTargetBodyWithoutTrailingNewline(t *testing.T) {
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("metric_no_newline 1")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	body := rr.Body.String()
	// The relay metrics should be on their own lines, not concatenated.
	if strings.Contains(body, "metric_no_newline 1# HELP") {
		t.Error("relay metrics should be separated from target body by newline")
	}
}

// generateSelfSignedCert creates a temporary self-signed certificate and key
// pair suitable for testing. It returns the paths to the cert and key files.
func generateSelfSignedCert(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}

	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("creating cert file: %v", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("encoding cert: %v", err)
	}
	certOut.Close()

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshaling key: %v", err)
	}
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("creating key file: %v", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatalf("encoding key: %v", err)
	}
	keyOut.Close()

	return certFile, keyFile
}

// --- buildTargetTLSConfig: CA cert loading ---

func TestBuildTargetTLSConfigWithCACert(t *testing.T) {
	dir := t.TempDir()
	certFile, _ := generateSelfSignedCert(t, dir)

	cfg, err := buildTargetTLSConfig(certFile, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}
	if cfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be false")
	}
}

func TestBuildTargetTLSConfigWithInvalidPEM(t *testing.T) {
	dir := t.TempDir()
	badFile := filepath.Join(dir, "bad-ca.pem")
	if err := os.WriteFile(badFile, []byte("not a valid PEM"), 0o600); err != nil {
		t.Fatalf("writing bad cert: %v", err)
	}

	_, err := buildTargetTLSConfig(badFile, false)
	if err == nil {
		t.Error("expected error for invalid PEM data")
	}
	if !strings.Contains(err.Error(), "failed to parse CA cert") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildTargetTLSConfigSkipVerifyWithCACert(t *testing.T) {
	dir := t.TempDir()
	certFile, _ := generateSelfSignedCert(t, dir)

	cfg, err := buildTargetTLSConfig(certFile, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Error("expected RootCAs to be set even with skip-verify")
	}
	if !cfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

// --- run(): TLS listener path ---

func TestRunWithTLSListener(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := generateSelfSignedCert(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- run(ctx, config{
			listenAddr:     "127.0.0.1:0",
			allowedSource:  "127.0.0.1",
			tlsCertFile:    certFile,
			tlsKeyFile:     keyFile,
			proxyTimeout:   5 * time.Second,
			concurrentReqs: 10,
			logLevel:       "error",
		})
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("run returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("run did not shut down in time")
	}
}

// --- run(): TLS listener with bad cert ---

func TestRunWithBadTLSCert(t *testing.T) {
	dir := t.TempDir()
	badCert := filepath.Join(dir, "bad-cert.pem")
	badKey := filepath.Join(dir, "bad-key.pem")
	os.WriteFile(badCert, []byte("not a cert"), 0o600)
	os.WriteFile(badKey, []byte("not a key"), 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := run(ctx, config{
		listenAddr:     "127.0.0.1:0",
		allowedSource:  "127.0.0.1",
		tlsCertFile:    badCert,
		tlsKeyFile:     badKey,
		proxyTimeout:   5 * time.Second,
		concurrentReqs: 10,
		logLevel:       "error",
	})
	if err == nil {
		t.Error("expected error with bad TLS cert/key")
	}
}

// --- run(): bad CA cert causes buildTargetTLSConfig to fail ---

func TestRunWithBadCACert(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := run(ctx, config{
		listenAddr:     "127.0.0.1:0",
		allowedSource:  "127.0.0.1",
		caCert:         "/nonexistent/ca.pem",
		proxyTimeout:   5 * time.Second,
		concurrentReqs: 10,
		logLevel:       "error",
	})
	if err == nil {
		t.Error("expected error with bad CA cert path")
	}
	if !strings.Contains(err.Error(), "building TLS config") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- run(): plain listener error (port already in use) ---

func TestRunListenError(t *testing.T) {
	// Bind a port so run() fails to listen.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("binding port: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := run(ctx, config{
		listenAddr:     ln.Addr().String(),
		allowedSource:  "127.0.0.1",
		proxyTimeout:   5 * time.Second,
		concurrentReqs: 10,
		logLevel:       "error",
	})
	if runErr == nil {
		t.Error("expected error when port is already in use")
	}
}

// --- run(): TLS listener error (port already in use) ---

func TestRunTLSListenError(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := generateSelfSignedCert(t, dir)

	// Bind a port so run() fails to listen.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("binding port: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := run(ctx, config{
		listenAddr:     ln.Addr().String(),
		allowedSource:  "127.0.0.1",
		tlsCertFile:    certFile,
		tlsKeyFile:     keyFile,
		proxyTimeout:   5 * time.Second,
		concurrentReqs: 10,
		logLevel:       "error",
	})
	if runErr == nil {
		t.Error("expected error when port is already in use for TLS")
	}
}

// --- metricsHandler: io.ReadAll error ---

type errReader struct{}

func (errReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("simulated read error")
}

func (errReader) Close() error { return nil }

func TestMetricsHandlerReadBodyError(t *testing.T) {
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       errReader{},
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}
	handler := metricsHandler("127.0.0.1", client, sem)

	req := httptest.NewRequest("GET", "/metrics?ip=10.0.0.1&port=9100", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "relay_target_response 0") {
		t.Error("expected relay_target_response 0 on read error")
	}
	if !strings.Contains(body, "relay_target_http_status 0") {
		t.Error("expected relay_target_http_status 0 on read error")
	}
}

// --- proxyHandler: target path constants ---

func TestProxyHandlerTargetPaths(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		targetPath   string
		expectedPath string
	}{
		{
			name:         "metrics endpoint",
			endpoint:     "/metrics",
			targetPath:   "/metrics",
			expectedPath: "/metrics",
		},
		{
			name:         "host endpoint",
			endpoint:     "/host",
			targetPath:   "/api/v0/component/prometheus.exporter.unix.host/metrics",
			expectedPath: "/api/v0/component/prometheus.exporter.unix.host/metrics",
		},
		{
			name:         "cadvisor endpoint",
			endpoint:     "/cadvisor",
			targetPath:   "/api/v0/component/prometheus.exporter.cadvisor.containers/metrics",
			expectedPath: "/api/v0/component/prometheus.exporter.cadvisor.containers/metrics",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			sem := make(chan struct{}, 10)
			client := &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					gotPath = r.URL.Path
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("test_metric 1\n")),
						Header:     make(http.Header),
					}, nil
				}),
				Timeout: 5 * time.Second,
			}
			handler := proxyHandler("127.0.0.1", client, sem, tc.targetPath)

			req := httptest.NewRequest("GET", tc.endpoint+"?ip=10.0.0.1&port=9100", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			rr := httptest.NewRecorder()
			handler(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rr.Code)
			}
			if gotPath != tc.expectedPath {
				t.Errorf("expected target path %q, got %q", tc.expectedPath, gotPath)
			}
		})
	}
}

// TestProxyHandlerPathQueryParameterIgnored verifies that a user-supplied
// "path" query parameter does not influence the target URL.
func TestProxyHandlerPathQueryParameterIgnored(t *testing.T) {
	tests := []struct {
		name         string
		targetPath   string
		queryPath    string
		expectedPath string
	}{
		{
			name:         "metrics ignores path param",
			targetPath:   "/metrics",
			queryPath:    "/etc/passwd",
			expectedPath: "/metrics",
		},
		{
			name:         "host ignores path param",
			targetPath:   "/api/v0/component/prometheus.exporter.unix.host/metrics",
			queryPath:    "/admin",
			expectedPath: "/api/v0/component/prometheus.exporter.unix.host/metrics",
		},
		{
			name:         "cadvisor ignores path param",
			targetPath:   "/api/v0/component/prometheus.exporter.cadvisor.containers/metrics",
			queryPath:    "/../../../etc/shadow",
			expectedPath: "/api/v0/component/prometheus.exporter.cadvisor.containers/metrics",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			sem := make(chan struct{}, 10)
			client := &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					gotPath = r.URL.Path
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("ok\n")),
						Header:     make(http.Header),
					}, nil
				}),
				Timeout: 5 * time.Second,
			}
			handler := proxyHandler("127.0.0.1", client, sem, tc.targetPath)

			req := httptest.NewRequest("GET", "/endpoint?ip=10.0.0.1&port=9100&path="+tc.queryPath, nil)
			req.RemoteAddr = "127.0.0.1:12345"
			rr := httptest.NewRecorder()
			handler(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rr.Code)
			}
			if gotPath != tc.expectedPath {
				t.Errorf("path query param influenced target: got %q, want %q", gotPath, tc.expectedPath)
			}
		})
	}
}

// TestProxyHandlerTLSScheme verifies that TLS parameter works for all endpoints.
func TestProxyHandlerTLSScheme(t *testing.T) {
	for _, targetPath := range []string{
		"/metrics",
		"/api/v0/component/prometheus.exporter.unix.host/metrics",
		"/api/v0/component/prometheus.exporter.cadvisor.containers/metrics",
	} {
		t.Run("tls=true path="+targetPath, func(t *testing.T) {
			var gotScheme string
			sem := make(chan struct{}, 10)
			client := &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					gotScheme = r.URL.Scheme
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("ok\n")),
						Header:     make(http.Header),
					}, nil
				}),
				Timeout: 5 * time.Second,
			}
			handler := proxyHandler("127.0.0.1", client, sem, targetPath)

			req := httptest.NewRequest("GET", "/endpoint?ip=10.0.0.1&port=9100&tls=true", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			rr := httptest.NewRecorder()
			handler(rr, req)

			if gotScheme != "https" {
				t.Errorf("expected https, got %q", gotScheme)
			}
		})
	}
}

// --- Validation parity: /host and /cadvisor share the same validation as /metrics ---

func TestHostAndCadvisorValidationParity(t *testing.T) {
	endpoints := []struct {
		name       string
		targetPath string
	}{
		{"host", "/api/v0/component/prometheus.exporter.unix.host/metrics"},
		{"cadvisor", "/api/v0/component/prometheus.exporter.cadvisor.containers/metrics"},
	}

	validationCases := []struct {
		name   string
		url    string
		status int
	}{
		{"missing ip", "/endpoint?port=9100", http.StatusBadRequest},
		{"missing port", "/endpoint?ip=10.0.0.1", http.StatusBadRequest},
		{"invalid ip", "/endpoint?ip=not-an-ip&port=9100", http.StatusBadRequest},
		{"public ip", "/endpoint?ip=203.0.113.1&port=9100", http.StatusBadRequest},
		{"port 0", "/endpoint?ip=10.0.0.1&port=0", http.StatusBadRequest},
		{"port 65536", "/endpoint?ip=10.0.0.1&port=65536", http.StatusBadRequest},
		{"port abc", "/endpoint?ip=10.0.0.1&port=abc", http.StatusBadRequest},
		{"invalid tls", "/endpoint?ip=10.0.0.1&port=9100&tls=maybe", http.StatusBadRequest},
	}

	for _, ep := range endpoints {
		for _, vc := range validationCases {
			t.Run(ep.name+"/"+vc.name, func(t *testing.T) {
				sem := make(chan struct{}, 10)
				handler := proxyHandler("127.0.0.1", &http.Client{Timeout: time.Second}, sem, ep.targetPath)

				req := httptest.NewRequest("GET", vc.url, nil)
				req.RemoteAddr = "127.0.0.1:12345"
				rr := httptest.NewRecorder()
				handler(rr, req)

				if rr.Code != vc.status {
					t.Errorf("expected %d, got %d", vc.status, rr.Code)
				}
			})
		}
	}
}

// TestHostAndCadvisorSourceIPDenied verifies source IP filtering on new endpoints.
func TestHostAndCadvisorSourceIPDenied(t *testing.T) {
	for _, targetPath := range []string{
		"/api/v0/component/prometheus.exporter.unix.host/metrics",
		"/api/v0/component/prometheus.exporter.cadvisor.containers/metrics",
	} {
		t.Run(targetPath, func(t *testing.T) {
			sem := make(chan struct{}, 10)
			handler := proxyHandler("192.168.1.1", &http.Client{Timeout: time.Second}, sem, targetPath)

			req := httptest.NewRequest("GET", "/endpoint?ip=10.0.0.1&port=9100", nil)
			req.RemoteAddr = "203.0.113.1:12345"
			rr := httptest.NewRecorder()
			handler(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("expected 403, got %d", rr.Code)
			}
		})
	}
}

// TestHostAndCadvisorConcurrencyLimit verifies concurrency limiting on new endpoints.
func TestHostAndCadvisorConcurrencyLimit(t *testing.T) {
	for _, targetPath := range []string{
		"/api/v0/component/prometheus.exporter.unix.host/metrics",
		"/api/v0/component/prometheus.exporter.cadvisor.containers/metrics",
	} {
		t.Run(targetPath, func(t *testing.T) {
			sem := make(chan struct{}, 1)
			sem <- struct{}{} // fill the semaphore

			handler := proxyHandler("127.0.0.1", &http.Client{Timeout: time.Second}, sem, targetPath)

			req := httptest.NewRequest("GET", "/endpoint?ip=10.0.0.1&port=9100", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			rr := httptest.NewRecorder()
			handler(rr, req)

			if rr.Code != http.StatusTooManyRequests {
				t.Errorf("expected 429, got %d", rr.Code)
			}

			<-sem
		})
	}
}

// --- 404 handling: target returns 404 for missing component ---

func TestTarget404SurfacesInGauges(t *testing.T) {
	sem := make(chan struct{}, 10)
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("404 page not found\n")),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 5 * time.Second,
	}

	for _, targetPath := range []string{
		"/api/v0/component/prometheus.exporter.unix.host/metrics",
		"/api/v0/component/prometheus.exporter.cadvisor.containers/metrics",
	} {
		t.Run(targetPath, func(t *testing.T) {
			handler := proxyHandler("127.0.0.1", client, sem, targetPath)

			req := httptest.NewRequest("GET", "/endpoint?ip=10.0.0.1&port=9100", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			rr := httptest.NewRecorder()
			handler(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected 200 from relay, got %d", rr.Code)
			}

			body := rr.Body.String()
			if !strings.Contains(body, "relay_response 1") {
				t.Error("expected relay_response 1")
			}
			if !strings.Contains(body, "relay_target_response 0") {
				t.Error("expected relay_target_response 0 for 404")
			}
			if !strings.Contains(body, "relay_target_http_status 404") {
				t.Error("expected relay_target_http_status 404")
			}
			// The response must still contain relay_duration_seconds (valid Prometheus text).
			if !strings.Contains(body, "relay_duration_seconds") {
				t.Error("expected relay_duration_seconds in response")
			}
		})
	}
}

// TestLandingPageLinksNewEndpoints verifies the landing page includes /host and /cadvisor.
func TestLandingPageLinksNewEndpoints(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	landingHandler(rr, req)

	body := rr.Body.String()
	for _, link := range []string{"/metrics", "/host", "/cadvisor", "/health"} {
		if !strings.Contains(body, link) {
			t.Errorf("landing page should link to %s", link)
		}
	}
}
