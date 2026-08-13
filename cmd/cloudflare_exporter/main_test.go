package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	cfclient "github.com/phaseshiftdata/prometheus_exporters/internal/cloudflare"
	"github.com/phaseshiftdata/prometheus_exporters/internal/collector"
	"github.com/phaseshiftdata/prometheus_exporters/internal/discovery"
	"github.com/prometheus/client_golang/prometheus"
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
