package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/src/exporter"

	cfclient "github.com/phaseshiftdata/prometheus_exporters/internal/cloudflare"
	"github.com/phaseshiftdata/prometheus_exporters/internal/collector"
	"github.com/phaseshiftdata/prometheus_exporters/internal/discovery"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func TestBuildLogger_Info(t *testing.T) {
	l := buildLogger("info")
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	l.Sync()
}

func TestBuildLogger_Debug(t *testing.T) {
	l := buildLogger("debug")
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	l.Sync()
}

func TestBuildLogger_Warn(t *testing.T) {
	l := buildLogger("warn")
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	l.Sync()
}

func TestBuildLogger_Error(t *testing.T) {
	l := buildLogger("error")
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	l.Sync()
}

func TestBuildLogger_Default(t *testing.T) {
	l := buildLogger("something_else")
	if l == nil {
		t.Fatal("expected non-nil logger for unknown level")
	}
	l.Sync()
}

func TestMakeCollectionStartCallback(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cb := makeCollectionStartCallback(logger)
	// Should not panic
	cb("test_collector")
}

func TestMakeCollectionCompleteCallback_NoError(t *testing.T) {
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	sm.Register(reg)

	cb := makeCollectionCompleteCallback(sm)
	// No error - should be a no-op
	cb("test_collector", 100*time.Millisecond, nil)
}

func TestMakeCollectionCompleteCallback_GenericError(t *testing.T) {
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	sm.Register(reg)

	cb := makeCollectionCompleteCallback(sm)
	// Generic error - reason should be "unknown"
	cb("test_collector", 100*time.Millisecond, fmt.Errorf("some error"))
}

func TestMakeCollectionCompleteCallback_RateLimitError(t *testing.T) {
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	sm.Register(reg)

	cb := makeCollectionCompleteCallback(sm)
	// Rate limit error - reason should be "rate_limited"
	rlErr := &cfclient.RateLimitError{StatusCode: 429}
	cb("test_collector", 100*time.Millisecond, rlErr)
}

func TestMakeCollectionShedCallback(t *testing.T) {
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	sm.Register(reg)

	cb := makeCollectionShedCallback(sm)
	cb("test_collector")
}

func TestUpdateDiscoveryMetrics_NilMatrix(t *testing.T) {
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	sm.Register(reg)

	// Should not panic
	updateDiscoveryMetrics(sm, nil)
}

func TestUpdateDiscoveryMetrics_WithMatrix(t *testing.T) {
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	sm.Register(reg)

	matrix := discovery.NewCapabilityMatrix()
	matrix.DiscoveredAt = time.Now()
	matrix.Accounts = []discovery.AccountInfo{{ID: "a1", Name: "Test"}}
	matrix.Zones = []discovery.ZoneInfo{
		{ID: "z1", Name: "example.com", Plan: "pro", Status: "active"},
		{ID: "z2", Name: "free.com", Plan: "free", Status: "active"},
		{ID: "z3", Name: "freeweb.com", Plan: "Free Website", Status: "active"},
	}
	matrix.SetDataset("ds1", discovery.DatasetCapability{
		Dataset: "ds1",
		Scope:   discovery.ScopeAccount,
		State:   discovery.StateAvailable,
	})
	matrix.SetDataset("ds2", discovery.DatasetCapability{
		Dataset: "ds2",
		Scope:   discovery.ScopeZone,
		State:   discovery.StateNotEntitled,
	})

	updateDiscoveryMetrics(sm, matrix)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	foundDiscovery := false
	foundFreeZones := false
	foundAvail := false
	foundUnavail := false
	for _, f := range families {
		switch f.GetName() {
		case "cloudflare_exporter_discovery_success_timestamp_seconds":
			foundDiscovery = true
		case "cloudflare_zones_skipped_free_tier":
			foundFreeZones = true
			if f.GetMetric()[0].GetGauge().GetValue() != 2 {
				t.Fatalf("expected 2 free zones, got %f", f.GetMetric()[0].GetGauge().GetValue())
			}
		case "cloudflare_exporter_dataset_available":
			foundAvail = true
		case "cloudflare_exporter_datasets_unavailable":
			foundUnavail = true
		}
	}

	if !foundDiscovery {
		t.Error("discovery metric not found")
	}
	if !foundFreeZones {
		t.Error("free zones metric not found")
	}
	if !foundAvail {
		t.Error("dataset_available metric not found")
	}
	if !foundUnavail {
		t.Error("datasets_unavailable metric not found")
	}
}

func TestUpdateDiscoveryMetrics_EmptyMatrix(t *testing.T) {
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	sm.Register(reg)

	matrix := discovery.NewCapabilityMatrix()
	updateDiscoveryMetrics(sm, matrix)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}
	_ = families
}

// newMockCFServer returns an httptest.Server that responds to Cloudflare REST
// and GraphQL endpoints used during discovery.
func newMockCFServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			switch r.URL.Path {
			case "/client/v4/user/tokens/verify":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"result":  map[string]string{"status": "active"},
				})
				return
			case "/client/v4/accounts":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"result": []map[string]string{
						{"id": "acc1", "name": "Test Account"},
					},
				})
				return
			case "/client/v4/zones":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"result": []map[string]interface{}{
						{"id": "z1", "name": "example.com", "status": "active", "plan": map[string]string{"name": "pro"}},
					},
				})
				return
			}
		}
		if r.Method == http.MethodPost {
			// GraphQL: return minimal valid response for all queries
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"__schema": map[string]interface{}{
						"queryType": map[string]interface{}{
							"fields": []map[string]string{{"name": "viewer"}},
						},
					},
					"__type": map[string]interface{}{
						"fields": []map[string]interface{}{
							{
								"name": "accounts",
								"type": map[string]interface{}{
									"fields": []map[string]interface{}{},
								},
							},
						},
					},
					"viewer": map[string]interface{}{
						"accounts": []map[string]interface{}{},
						"zones":    []map[string]interface{}{},
					},
				},
			})
			return
		}
		w.WriteHeader(500)
	}))
}

func testRunConfig() runConfig {
	return runConfig{
		APIToken:               "test-token",
		ScrapeDelay:            300 * time.Second,
		TimeWindow:             60 * time.Second,
		RefreshInterval:        60 * time.Second,
		DiscoveryInterval:      6 * time.Hour,
		GraphQLBudgetPerWindow: 160,
		RESTBudgetPerWindow:    600,
		GatewayCategoryTopN:    25,
		RequestTimeout:         5 * time.Second,
		ListenAddress:          "127.0.0.1:0",
		MetricsPath:            "/metrics",
		LogLevel:               "info",
		CapabilitiesOnly:       true,
	}
}

func TestRun_CapabilitiesOnly(t *testing.T) {
	// With CapabilitiesOnly=true, run should perform discovery (which will
	// fail with connection errors to the real Cloudflare API), handle the
	// failure gracefully, serialize the matrix, and return nil.
	rc := testRunConfig()
	rc.CapabilitiesOnly = true

	err := run(context.Background(), rc)
	if err != nil {
		t.Fatalf("run with CapabilitiesOnly should not return error, got: %v", err)
	}
}

func createMockClient(ts *httptest.Server) *cfclient.Client {
	c := cfclient.NewClient("test-token", 5*time.Second)
	c.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = ts.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	})
	return c
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRun_WithMockServer(t *testing.T) {
	// Test run with a mock Cloudflare API server that makes discovery
	// succeed, enabling the successful re-discovery path.
	ts := newMockCFServer()
	defer ts.Close()

	client := createMockClient(ts)

	rc := testRunConfig()
	rc.CapabilitiesOnly = false
	rc.ListenAddress = "127.0.0.1:19202"
	rc.DiscoveryInterval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		// Wait for server to start
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := http.Get("http://127.0.0.1:19202/health")
			if err == nil {
				resp.Body.Close()
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Hit capabilities endpoint to exercise matrixFunc
		resp, err := http.Get("http://127.0.0.1:19202/capabilities")
		if err == nil {
			resp.Body.Close()
		}

		// Wait for re-discovery goroutine to fire and succeed
		time.Sleep(300 * time.Millisecond)

		cancel()
	}()

	err := run(ctx, rc, client)
	if err != nil {
		t.Fatalf("run should return nil on clean shutdown, got: %v", err)
	}
}

func TestRun_SignalShutdown(t *testing.T) {
	// Test that run shuts down gracefully when receiving SIGINT.
	rc := testRunConfig()
	rc.CapabilitiesOnly = false
	rc.ListenAddress = "127.0.0.1:19201"

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(context.Background(), rc)
	}()

	// Wait for the server to be ready
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:19201/health")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Send SIGINT to trigger the signal handler path
	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGINT)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run should return nil on signal shutdown, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return within timeout after SIGINT")
	}
}

func TestRun_ShortLivedContext(t *testing.T) {
	// With CapabilitiesOnly=false, run starts the HTTP server and blocks.
	// We cancel the context after the server starts to trigger the shutdown
	// path. Use very short intervals so the goroutine ticker bodies fire at
	// least once before the context is canceled.
	rc := testRunConfig()
	rc.CapabilitiesOnly = false
	rc.DiscoveryInterval = 50 * time.Millisecond
	rc.ListenAddress = "127.0.0.1:19199"

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after enough time for the server to start and
	// goroutine tickers to fire at least once.
	go func() {
		// Wait for the server to be ready
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := http.Get("http://127.0.0.1:19199/health")
			if err == nil {
				resp.Body.Close()
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Hit the /capabilities endpoint to exercise the matrixFunc closure
		resp, err := http.Get("http://127.0.0.1:19199/capabilities")
		if err == nil {
			resp.Body.Close()
		}

		// Wait a bit for the re-discovery goroutine to fire
		time.Sleep(200 * time.Millisecond)

		cancel()
	}()

	err := run(ctx, rc)
	if err != nil {
		t.Fatalf("run should return nil on clean shutdown, got: %v", err)
	}
}

func TestRootCmd_Version(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--version should not return error, got: %v", err)
	}
}

func TestRootCmd_Help(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help should not return error, got: %v", err)
	}
}

func TestToInternalConfig(t *testing.T) {
	rc := testRunConfig()
	cfg := toInternalConfig(rc)
	if cfg.APIToken != rc.APIToken {
		t.Errorf("APIToken mismatch: got %q, want %q", cfg.APIToken, rc.APIToken)
	}
	if cfg.ListenAddress != rc.ListenAddress {
		t.Errorf("ListenAddress mismatch: got %q, want %q", cfg.ListenAddress, rc.ListenAddress)
	}
	if cfg.ScrapeDelay != rc.ScrapeDelay {
		t.Errorf("ScrapeDelay mismatch: got %v, want %v", cfg.ScrapeDelay, rc.ScrapeDelay)
	}
	if cfg.CapabilitiesOnly != rc.CapabilitiesOnly {
		t.Errorf("CapabilitiesOnly mismatch: got %v, want %v", cfg.CapabilitiesOnly, rc.CapabilitiesOnly)
	}
}

// ---------------------------------------------------------------------------
// execute tests
// ---------------------------------------------------------------------------

func TestExecute_Success(t *testing.T) {
	code := exporter.Execute(func() *cobra.Command {
		cmd := rootCmd()
		cmd.SetArgs([]string{"--version"})
		return cmd
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}

func TestExecute_Error(t *testing.T) {
	code := exporter.Execute(func() *cobra.Command {
		cmd := rootCmd()
		cmd.SetArgs([]string{"--no-such-flag"})
		return cmd
	})
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

// ---------------------------------------------------------------------------
// splitCSV tests
// ---------------------------------------------------------------------------

func TestSplitCSV_Empty(t *testing.T) {
	result := splitCSV("")
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestSplitCSV_SingleValue(t *testing.T) {
	result := splitCSV("hello")
	if len(result) != 1 || result[0] != "hello" {
		t.Fatalf("expected [hello], got %v", result)
	}
}

func TestSplitCSV_MultipleValues(t *testing.T) {
	result := splitCSV("a, b ,c")
	if len(result) != 3 || result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Fatalf("expected [a b c], got %v", result)
	}
}

func TestSplitCSV_OnlyWhitespace(t *testing.T) {
	// Comma-separated empty/whitespace parts should return nil.
	result := splitCSV(" , , ")
	if result != nil {
		t.Fatalf("expected nil for whitespace-only CSV, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// resolveString / resolveInt tests
// ---------------------------------------------------------------------------

func TestResolveString_FlagChanged(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"--cf.api-token", "from-flag"})
	// Parse flags without running RunE.
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return nil }
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	got := resolveString(cmd, "cf.api-token", "from-flag", "CF_API_TOKEN", "default-val")
	if got != "from-flag" {
		t.Fatalf("expected from-flag, got %q", got)
	}
}

func TestResolveString_EnvFallback(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{})
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return nil }
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CF_API_TOKEN", "from-env")
	got := resolveString(cmd, "cf.api-token", "", "CF_API_TOKEN", "default-val")
	if got != "from-env" {
		t.Fatalf("expected from-env, got %q", got)
	}
}

func TestResolveString_DefaultFallback(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{})
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return nil }
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	got := resolveString(cmd, "cf.api-token", "", "CF_API_TOKEN_NONEXISTENT", "default-val")
	if got != "default-val" {
		t.Fatalf("expected default-val, got %q", got)
	}
}

func TestResolveString_FlagValFallback(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{})
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return nil }
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	got := resolveString(cmd, "cf.api-token", "flag-default", "CF_API_TOKEN_NONEXISTENT", "")
	if got != "flag-default" {
		t.Fatalf("expected flag-default, got %q", got)
	}
}

func TestResolveInt_FlagChanged(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"--cf.scrape-delay", "999"})
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return nil }
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	got := resolveInt(cmd, "cf.scrape-delay", 999, "CF_SCRAPE_DELAY_SECONDS", 300)
	if got != 999 {
		t.Fatalf("expected 999, got %d", got)
	}
}

func TestResolveInt_EnvFallback(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{})
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return nil }
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CF_SCRAPE_DELAY_SECONDS", "777")
	got := resolveInt(cmd, "cf.scrape-delay", 300, "CF_SCRAPE_DELAY_SECONDS", 300)
	if got != 777 {
		t.Fatalf("expected 777, got %d", got)
	}
}

func TestResolveInt_EnvInvalid(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{})
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return nil }
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CF_SCRAPE_DELAY_SECONDS", "not-a-number")
	got := resolveInt(cmd, "cf.scrape-delay", 300, "CF_SCRAPE_DELAY_SECONDS", 42)
	if got != 42 {
		t.Fatalf("expected default 42 when env is invalid, got %d", got)
	}
}

func TestResolveInt_DefaultFallback(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{})
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return nil }
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	got := resolveInt(cmd, "cf.scrape-delay", 300, "CF_NONEXISTENT_VAR", 42)
	if got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// rootCmd RunE closure test (exercises resolveString/resolveInt through cobra)
// ---------------------------------------------------------------------------

func TestRootCmd_RunE_WithFlags(t *testing.T) {
	// Execute rootCmd with --capabilities so it runs the RunE closure
	// (which calls resolveString/resolveInt to build runConfig) and then
	// run() exits early in capabilities mode.
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token", "tok",
		"--cf.accounts", "a1,a2",
		"--cf.zones", "z1",
		"--cf.zones-exclude", "z2",
		"--cf.scrape-delay", "60",
		"--cf.time-window", "30",
		"--cf.refresh-interval", "15",
		"--cf.discovery-interval", "100",
		"--cf.graphql-budget", "50",
		"--cf.rest-budget", "200",
		"--cf.collectors-enabled", "access,dns",
		"--cf.gateway-category-allowlist", "cat1,cat2",
		"--cf.gateway-category-top-n", "10",
		"--cf.request-timeout", "3",
		"--web.listen-address", "127.0.0.1:0",
		"--web.metrics-path", "/m",
		"--web.tls-cert-file", "",
		"--web.tls-key-file", "",
		"--web.basic-auth-username", "",
		"--web.basic-auth-password", "",
		"--log.level", "debug",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rootCmd with flags should succeed in capabilities mode: %v", err)
	}
}

func TestRootCmd_RunE_WithEnvVars(t *testing.T) {
	// Exercise the env-var fallback paths inside the RunE closure.
	t.Setenv("CF_API_TOKEN", "env-token")
	t.Setenv("CF_ACCOUNTS", "ea1")
	t.Setenv("CF_ZONES", "ez1")
	t.Setenv("CF_ZONES_EXCLUDE", "ez2")
	t.Setenv("CF_SCRAPE_DELAY_SECONDS", "120")
	t.Setenv("CF_TIME_WINDOW_SECONDS", "45")
	t.Setenv("CF_REFRESH_INTERVAL_SECONDS", "20")
	t.Setenv("CF_DISCOVERY_INTERVAL_SECONDS", "500")
	t.Setenv("CF_GRAPHQL_BUDGET_PER_WINDOW", "80")
	t.Setenv("CF_REST_BUDGET_PER_WINDOW", "400")
	t.Setenv("CF_COLLECTORS_ENABLED", "dns")
	t.Setenv("CF_GATEWAY_CATEGORY_ALLOWLIST", "cat1")
	t.Setenv("CF_GATEWAY_CATEGORY_TOP_N", "5")
	t.Setenv("CF_REQUEST_TIMEOUT_SECONDS", "7")
	t.Setenv("LISTEN_ADDRESS", "127.0.0.1:0")
	t.Setenv("METRICS_PATH", "/prom")
	t.Setenv("LOG_LEVEL", "warn")

	cmd := rootCmd()
	cmd.SetArgs([]string{"--capabilities"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rootCmd with env vars should succeed in capabilities mode: %v", err)
	}
}

// ---------------------------------------------------------------------------
// File-based secret flag tests
// ---------------------------------------------------------------------------

func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRootCmd_APITokenFile(t *testing.T) {
	path := writeSecretFile(t, "file-token\n")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token-file", path,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected success with --cf.api-token-file, got: %v", err)
	}
}

func TestRootCmd_APITokenFile_MissingFile(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token-file", "/nonexistent/token",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing secret file")
	}
}

func TestRootCmd_APITokenFile_EmptyFile(t *testing.T) {
	path := writeSecretFile(t, "")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token-file", path,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for empty secret file")
	}
}

func TestRootCmd_APITokenFile_ConflictWithFlag(t *testing.T) {
	path := writeSecretFile(t, "file-token\n")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token", "flag-token",
		"--cf.api-token-file", path,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when both --cf.api-token and --cf.api-token-file are set")
	}
}

func TestRootCmd_APITokenFile_ConflictWithEnv(t *testing.T) {
	path := writeSecretFile(t, "file-token\n")
	t.Setenv("CF_API_TOKEN", "env-token")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token-file", path,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when both CF_API_TOKEN env and --cf.api-token-file are set")
	}
}

func TestRootCmd_BasicAuthPasswordFile(t *testing.T) {
	path := writeSecretFile(t, "secret-password\n")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token", "tok",
		"--web.basic-auth-password-file", path,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected success with --web.basic-auth-password-file, got: %v", err)
	}
}

func TestRootCmd_BasicAuthPasswordFile_ConflictWithFlag(t *testing.T) {
	path := writeSecretFile(t, "secret-password\n")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token", "tok",
		"--web.basic-auth-password", "inline-password",
		"--web.basic-auth-password-file", path,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when both --web.basic-auth-password and --web.basic-auth-password-file are set")
	}
}

func TestBuildSlogLogger(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "unknown"} {
		l := buildSlogLogger(level)
		if l == nil {
			t.Fatalf("expected non-nil logger for level %q", level)
		}
	}
}

func TestRootCmd_APITokenOpenBao(t *testing.T) {
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
		case r.URL.Path == "/v1/secret/data/cf" && r.Method == "GET":
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"api_token": "vault-cf-token",
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	roleIDFile := writeSecretFile(t, "role-id\n")
	secretIDFile := writeSecretFile(t, "secret-id\n")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token-openbao", "secret/cf:api_token",
		"--openbao-address", srv.URL,
		"--openbao-approle-role-id-file", roleIDFile,
		"--openbao-approle-secret-id-file", secretIDFile,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected success with --cf.api-token-openbao, got: %v", err)
	}
}

func TestRootCmd_APITokenOpenBao_BadRef(t *testing.T) {
	// Bad ref format triggers ParseOpenBaoRef error before the OpenBao client is created.
	// We still need a valid init to get past NewOpenBaoClient.
	// Use the init failure path instead to avoid default registerer conflicts.
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token-openbao", "bad-ref-no-colon",
		"--openbao-address", "",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRootCmd_APITokenOpenBao_InitFails(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token-openbao", "secret/cf:api_token",
		"--openbao-address", "",
		"--openbao-approle-role-id-file", "/nonexistent/role",
		"--openbao-approle-secret-id-file", "/nonexistent/secret",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when OpenBao client initialization fails")
	}
}

func TestRootCmd_APITokenOpenBao_ConflictWithFlag(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token", "flag-token",
		"--cf.api-token-openbao", "secret/cf:api_token",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when both --cf.api-token and --cf.api-token-openbao are set")
	}
}

func TestRootCmd_BasicAuthPasswordInlineWarning(t *testing.T) {
	// Exercise the inline password warning path (line 182-184).
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token", "tok",
		"--web.basic-auth-password", "inline-pw",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestRootCmd_BasicAuthPasswordFile_MissingFile(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--capabilities",
		"--cf.api-token", "tok",
		"--web.basic-auth-password-file", "/nonexistent/password",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing password file")
	}
}
