package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector/openbao"
	"github.com/phaseshiftdata/prometheus_exporters/src/exporter"
)

// --- rootCmd tests ---

func TestRootCmd(t *testing.T) {
	cmd := rootCmd()
	if cmd.Use != "openbao_exporter" {
		t.Errorf("expected Use 'openbao_exporter', got %q", cmd.Use)
	}
	if cmd.Version == "" {
		t.Error("empty version")
	}
}

func TestRootCmdExecute(t *testing.T) {
	srv := mockOpenBao(t)

	cmd := rootCmd()
	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--openbao-addr", srv.URL, "--listen-address", "127.0.0.1:0"})

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()
	time.Sleep(200 * time.Millisecond)
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

func TestMissingOpenbaoAddr(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"--listen-address", "127.0.0.1:0"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when --openbao-addr is missing")
	}
}

func TestInvalidOpenbaoAddr(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"--openbao-addr", "not-a-url"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid --openbao-addr")
	}
}

func TestOpenbaoAddrHTTP(t *testing.T) {
	srv := mockOpenBao(t)

	cmd := rootCmd()
	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--openbao-addr", srv.URL, "--listen-address", "127.0.0.1:0"})

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
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

// --- run() tests ---

func TestRunWithTokenFile(t *testing.T) {
	srv := mockOpenBao(t)

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte("test-token\n"), 0o600); err != nil {
		t.Fatalf("writing token file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- run(ctx, config{
			listenAddr:   "127.0.0.1:0",
			openbaoAddr:  srv.URL,
			tokenFile:    tokenPath,
			logLevel:     "error",
			pollInterval: 0,
		})
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("run did not shut down in time")
	}
}

func TestRunWithBadTokenFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := run(ctx, config{
		listenAddr:   "127.0.0.1:0",
		openbaoAddr:  "http://localhost:8200",
		tokenFile:    "/nonexistent/token",
		logLevel:     "error",
		pollInterval: 0,
	})
	if err == nil {
		t.Error("expected error for nonexistent token file")
	}
	if !strings.Contains(err.Error(), "--openbao-token-file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunListenError(t *testing.T) {
	// Bind a port so run() fails to listen.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("binding port: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := mockOpenBao(t)

	runErr := run(ctx, config{
		listenAddr:   ln.Addr().String(),
		openbaoAddr:  srv.URL,
		logLevel:     "error",
		pollInterval: 0,
	})
	if runErr == nil {
		t.Error("expected error when port is already in use")
	}
}

func TestRunWithExplicitToken(t *testing.T) {
	srv := mockOpenBao(t)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- run(ctx, config{
			listenAddr:   "127.0.0.1:0",
			openbaoAddr:  srv.URL,
			logLevel:     "error",
			pollInterval: 0,
		})
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("run did not shut down in time")
	}
}

func TestRunWithRegistryOverride(t *testing.T) {
	srv := mockOpenBao(t)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	reg := prometheus.NewRegistry()
	go func() {
		errCh <- run(ctx, config{
			listenAddr:   "127.0.0.1:0",
			openbaoAddr:  srv.URL,
			logLevel:     "error",
			pollInterval: 0,
		}, reg)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("run did not shut down in time")
	}
}

func TestRunTokenPrecedence(t *testing.T) {
	srv := mockOpenBao(t)
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	os.WriteFile(tokenPath, []byte("file-token"), 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	// Token read from file.
	go func() {
		errCh <- run(ctx, config{
			listenAddr:   "127.0.0.1:0",
			openbaoAddr:  srv.URL,
			tokenFile:    tokenPath,
			logLevel:     "error",
			pollInterval: 0,
		})
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("run did not shut down in time")
	}
}

func TestCreateAllCollectors(t *testing.T) {
	collectors := createAllCollectors("http://localhost:8200", "token", 30*time.Second)
	if len(collectors) != 1 {
		t.Errorf("expected 1 collector, got %d", len(collectors))
	}
	if collectors[0].Name() != "openbao" {
		t.Errorf("expected collector name 'openbao', got %q", collectors[0].Name())
	}
}

func TestServeMetricsEndpoint(t *testing.T) {
	srv := mockOpenBao(t)

	client := openbao.NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := openbao.New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)

	ctx, cancel := context.WithCancel(context.Background())

	// Find a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ctx, addr, "Test Exporter", reg, coll)
	}()

	// Wait for server to start.
	time.Sleep(300 * time.Millisecond)

	// Hit /metrics endpoint.
	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		cancel()
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		cancel()
		t.Fatalf("reading response body: %v", readErr)
	}
	bodyStr := string(body)

	// Should contain registry metrics.
	if !strings.Contains(bodyStr, "openbao_up") {
		t.Errorf("expected openbao_up in response, got:\n%s", bodyStr)
	}

	// Should contain native metrics.
	if !strings.Contains(bodyStr, "test_metric") {
		t.Errorf("expected native test_metric in response, got:\n%s", bodyStr)
	}

	// Hit / landing page.
	resp2, err := http.Get("http://" + addr + "/")
	if err != nil {
		cancel()
		t.Fatalf("GET /: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}

	body2, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body2), "Test Exporter") {
		t.Error("landing page should contain exporter name")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("serve did not shut down in time")
	}
}

func TestServeListenError(t *testing.T) {
	// Bind a port so serve() fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("binding port: %v", err)
	}
	defer ln.Close()

	srv := mockOpenBao(t)
	client := openbao.NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := openbao.New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = serve(ctx, ln.Addr().String(), "Test", reg, coll)
	if err == nil {
		t.Error("expected error when port is in use")
	}
}

func TestResponseRecorder(t *testing.T) {
	rec := &responseRecorder{header: make(http.Header)}
	rec.Header().Set("Content-Type", "text/plain")
	rec.WriteHeader(200)
	n, err := rec.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5, got %d", n)
	}
	if string(rec.body) != "hello" {
		t.Errorf("expected 'hello', got %q", string(rec.body))
	}
	if rec.status != 200 {
		t.Errorf("expected status 200, got %d", rec.status)
	}
}

func TestServeMetricsNoNativeMetrics(t *testing.T) {
	// Server that returns 403 for metrics (no native metrics).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sys/health":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"test"}`))
		case "/v1/sys/metrics":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client := openbao.NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := openbao.New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)

	ctx, cancel := context.WithCancel(context.Background())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ctx, addr, "Test", reg, coll)
	}()

	time.Sleep(300 * time.Millisecond)

	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		cancel()
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("serve did not shut down in time")
	}
}

// mockOpenBao creates a test server that simulates the OpenBao API.
func mockOpenBao(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sys/health":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"test-cluster"}`))
		case "/v1/sys/metrics":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`# HELP test_metric A test metric.
# TYPE test_metric gauge
test_metric 1
`))
		case "/v1/sys/storage/raft/configuration":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
