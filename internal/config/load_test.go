package config

import (
	"flag"
	"os"
	"testing"
)

func TestLoad_WithEnvVars(t *testing.T) {
	// Save and restore original command line and env
	origArgs := os.Args
	origFlagCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagCommandLine
	}()

	// Reset flag parsing
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test"}

	// Set environment variables
	os.Setenv("CF_API_TOKEN", "test-token-123")
	os.Setenv("CF_ACCOUNTS", "acc1,acc2")
	os.Setenv("CF_ZONES", "z1,z2")
	os.Setenv("CF_ZONES_EXCLUDE", "z3")
	os.Setenv("CF_COLLECTORS_ENABLED", "access,dns")
	os.Setenv("LOG_LEVEL", "debug")
	defer func() {
		os.Unsetenv("CF_API_TOKEN")
		os.Unsetenv("CF_ACCOUNTS")
		os.Unsetenv("CF_ZONES")
		os.Unsetenv("CF_ZONES_EXCLUDE")
		os.Unsetenv("CF_COLLECTORS_ENABLED")
		os.Unsetenv("LOG_LEVEL")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.APIToken != "test-token-123" {
		t.Fatalf("expected test-token-123, got %q", cfg.APIToken)
	}
	if len(cfg.Accounts) != 2 || cfg.Accounts[0] != "acc1" {
		t.Fatalf("unexpected accounts: %v", cfg.Accounts)
	}
	if len(cfg.Zones) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(cfg.Zones))
	}
	if len(cfg.ZonesExclude) != 1 || cfg.ZonesExclude[0] != "z3" {
		t.Fatalf("unexpected zones exclude: %v", cfg.ZonesExclude)
	}
	if len(cfg.CollectorsEnabled) != 2 {
		t.Fatalf("expected 2 collectors enabled, got %d", len(cfg.CollectorsEnabled))
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("expected debug, got %q", cfg.LogLevel)
	}
}

func TestLoad_Defaults(t *testing.T) {
	origArgs := os.Args
	origFlagCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test"}

	os.Setenv("CF_API_TOKEN", "token-for-defaults")
	defer os.Unsetenv("CF_API_TOKEN")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.ScrapeDelay.Seconds() != 300 {
		t.Fatalf("expected default scrape delay 300s, got %v", cfg.ScrapeDelay)
	}
	if cfg.TimeWindow.Seconds() != 60 {
		t.Fatalf("expected default time window 60s, got %v", cfg.TimeWindow)
	}
	if cfg.RefreshInterval.Seconds() != 60 {
		t.Fatalf("expected default refresh interval 60s, got %v", cfg.RefreshInterval)
	}
	if cfg.ListenAddress != ":9199" {
		t.Fatalf("expected :9199, got %q", cfg.ListenAddress)
	}
	if cfg.MetricsPath != "/metrics" {
		t.Fatalf("expected /metrics, got %q", cfg.MetricsPath)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected info, got %q", cfg.LogLevel)
	}
}

func TestLoad_MissingToken(t *testing.T) {
	origArgs := os.Args
	origFlagCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test"}

	// Ensure CF_API_TOKEN is not set
	os.Unsetenv("CF_API_TOKEN")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing API token")
	}
}

func TestLoad_WithIntEnvVars(t *testing.T) {
	origArgs := os.Args
	origFlagCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test"}

	os.Setenv("CF_API_TOKEN", "token")
	os.Setenv("CF_SCRAPE_DELAY_SECONDS", "600")
	os.Setenv("CF_TIME_WINDOW_SECONDS", "120")
	os.Setenv("CF_REFRESH_INTERVAL_SECONDS", "30")
	os.Setenv("CF_DISCOVERY_INTERVAL_SECONDS", "3600")
	os.Setenv("CF_GRAPHQL_BUDGET_PER_WINDOW", "200")
	os.Setenv("CF_REST_BUDGET_PER_WINDOW", "800")
	os.Setenv("CF_GATEWAY_CATEGORY_TOP_N", "50")
	os.Setenv("CF_REQUEST_TIMEOUT_SECONDS", "20")
	os.Setenv("LISTEN_ADDRESS", ":8080")
	os.Setenv("METRICS_PATH", "/custom-metrics")
	defer func() {
		os.Unsetenv("CF_API_TOKEN")
		os.Unsetenv("CF_SCRAPE_DELAY_SECONDS")
		os.Unsetenv("CF_TIME_WINDOW_SECONDS")
		os.Unsetenv("CF_REFRESH_INTERVAL_SECONDS")
		os.Unsetenv("CF_DISCOVERY_INTERVAL_SECONDS")
		os.Unsetenv("CF_GRAPHQL_BUDGET_PER_WINDOW")
		os.Unsetenv("CF_REST_BUDGET_PER_WINDOW")
		os.Unsetenv("CF_GATEWAY_CATEGORY_TOP_N")
		os.Unsetenv("CF_REQUEST_TIMEOUT_SECONDS")
		os.Unsetenv("LISTEN_ADDRESS")
		os.Unsetenv("METRICS_PATH")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.ScrapeDelay.Seconds() != 600 {
		t.Fatalf("expected 600s, got %v", cfg.ScrapeDelay)
	}
	if cfg.TimeWindow.Seconds() != 120 {
		t.Fatalf("expected 120s, got %v", cfg.TimeWindow)
	}
	if cfg.GraphQLBudgetPerWindow != 200 {
		t.Fatalf("expected 200, got %d", cfg.GraphQLBudgetPerWindow)
	}
	if cfg.ListenAddress != ":8080" {
		t.Fatalf("expected :8080, got %q", cfg.ListenAddress)
	}
}

func TestLoad_CapabilitiesOnly(t *testing.T) {
	origArgs := os.Args
	origFlagCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test", "-capabilities"}

	os.Unsetenv("CF_API_TOKEN")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with --capabilities failed: %v", err)
	}

	if !cfg.CapabilitiesOnly {
		t.Fatal("expected CapabilitiesOnly to be true")
	}
}
