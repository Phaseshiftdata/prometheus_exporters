package config

import (
	"os"
	"testing"
)

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "a", []string{"a"}},
		{"multiple", "a,b,c", []string{"a", "b", "c"}},
		{"trimmed", " a , b , c ", []string{"a", "b", "c"}},
		{"empty_parts", "a,,b", []string{"a", "b"}},
		{"all_whitespace", " , , ", nil},
		{"trailing_comma", "a,b,", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCSV(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("splitCSV(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitCSV(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestResolveString(t *testing.T) {
	// CLI flag set takes priority
	flagsSet := map[string]bool{"my-flag": true}
	got := resolveString(flagsSet, "my-flag", "flag-val", "MY_ENV", "default-val")
	if got != "flag-val" {
		t.Errorf("expected flag-val, got %q", got)
	}

	// Env var takes priority over default
	flagsSet2 := map[string]bool{}
	os.Setenv("MY_TEST_ENV_RS", "env-val")
	defer os.Unsetenv("MY_TEST_ENV_RS")
	got = resolveString(flagsSet2, "my-flag", "", "MY_TEST_ENV_RS", "default-val")
	if got != "env-val" {
		t.Errorf("expected env-val, got %q", got)
	}

	// Default is used when no flag and no env
	os.Unsetenv("MY_TEST_ENV_RS")
	got = resolveString(flagsSet2, "my-flag", "", "MY_TEST_ENV_NONEXIST", "default-val")
	if got != "default-val" {
		t.Errorf("expected default-val, got %q", got)
	}

	// Falls back to flagVal when defaultVal is empty and no env
	got = resolveString(flagsSet2, "my-flag", "flag-fallback", "MY_TEST_ENV_NONEXIST", "")
	if got != "flag-fallback" {
		t.Errorf("expected flag-fallback, got %q", got)
	}
}

func TestResolveInt(t *testing.T) {
	// CLI flag set
	flagsSet := map[string]bool{"my-int-flag": true}
	got := resolveInt(flagsSet, "my-int-flag", 42, "MY_INT_ENV", 10)
	if got != 42 {
		t.Errorf("expected 42, got %d", got)
	}

	// Env var
	os.Setenv("MY_INT_ENV_RI", "99")
	defer os.Unsetenv("MY_INT_ENV_RI")
	got = resolveInt(map[string]bool{}, "my-int-flag", 0, "MY_INT_ENV_RI", 10)
	if got != 99 {
		t.Errorf("expected 99, got %d", got)
	}

	// Invalid env var falls back to default
	os.Setenv("MY_INT_ENV_BAD", "not-a-number")
	defer os.Unsetenv("MY_INT_ENV_BAD")
	got = resolveInt(map[string]bool{}, "my-int-flag", 0, "MY_INT_ENV_BAD", 10)
	if got != 10 {
		t.Errorf("expected 10, got %d", got)
	}

	// Default
	got = resolveInt(map[string]bool{}, "my-int-flag", 0, "MY_INT_ENV_NONEXIST", 10)
	if got != 10 {
		t.Errorf("expected 10, got %d", got)
	}
}

func TestValidate_MissingAPIToken(t *testing.T) {
	c := &Config{
		LogLevel:              "info",
		ScrapeDelay:           300,
		TimeWindow:            60,
		RefreshInterval:       60,
		DiscoveryInterval:     21600,
		GraphQLBudgetPerWindow: 160,
		RESTBudgetPerWindow:   600,
		GatewayCategoryTopN:   25,
		RequestTimeout:        10,
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for missing API token")
	}
}

func TestValidate_CapabilitiesOnly_NoToken(t *testing.T) {
	c := &Config{
		CapabilitiesOnly:      true,
		LogLevel:              "info",
		ScrapeDelay:           300,
		TimeWindow:            60,
		RefreshInterval:       60,
		DiscoveryInterval:     21600,
		GraphQLBudgetPerWindow: 160,
		RESTBudgetPerWindow:   600,
		GatewayCategoryTopN:   25,
		RequestTimeout:        10,
	}
	err := c.Validate()
	if err != nil {
		t.Fatalf("expected no error with CapabilitiesOnly, got: %v", err)
	}
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	c := &Config{
		APIToken:              "token",
		LogLevel:              "verbose",
		ScrapeDelay:           300,
		TimeWindow:            60,
		RefreshInterval:       60,
		DiscoveryInterval:     21600,
		GraphQLBudgetPerWindow: 160,
		RESTBudgetPerWindow:   600,
		GatewayCategoryTopN:   25,
		RequestTimeout:        10,
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestValidate_ValidLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "DEBUG", "Info", "WARN"} {
		c := &Config{
			APIToken:              "token",
			LogLevel:              level,
			ScrapeDelay:           300,
			TimeWindow:            60,
			RefreshInterval:       60,
			DiscoveryInterval:     21600,
			GraphQLBudgetPerWindow: 160,
			RESTBudgetPerWindow:   600,
			GatewayCategoryTopN:   25,
			RequestTimeout:        10,
		}
		if err := c.Validate(); err != nil {
			t.Errorf("expected no error for log level %q, got: %v", level, err)
		}
	}
}

func TestValidate_NegativeFields(t *testing.T) {
	c := &Config{
		APIToken:              "token",
		LogLevel:              "info",
		ScrapeDelay:           -1,
		TimeWindow:            0,
		RefreshInterval:       0,
		DiscoveryInterval:     0,
		GraphQLBudgetPerWindow: 0,
		RESTBudgetPerWindow:   0,
		GatewayCategoryTopN:   0,
		RequestTimeout:        0,
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected errors for negative/zero values")
	}
}

func TestValidate_TLS_OnlyOne(t *testing.T) {
	c := &Config{
		APIToken:              "token",
		LogLevel:              "info",
		ScrapeDelay:           300,
		TimeWindow:            60,
		RefreshInterval:       60,
		DiscoveryInterval:     21600,
		GraphQLBudgetPerWindow: 160,
		RESTBudgetPerWindow:   600,
		GatewayCategoryTopN:   25,
		RequestTimeout:        10,
		TLSCertFile:           "/path/cert.pem",
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error when only TLS cert is specified")
	}
}

func TestValidate_TLS_BothSet(t *testing.T) {
	c := &Config{
		APIToken:              "token",
		LogLevel:              "info",
		ScrapeDelay:           300,
		TimeWindow:            60,
		RefreshInterval:       60,
		DiscoveryInterval:     21600,
		GraphQLBudgetPerWindow: 160,
		RESTBudgetPerWindow:   600,
		GatewayCategoryTopN:   25,
		RequestTimeout:        10,
		TLSCertFile:           "/path/cert.pem",
		TLSKeyFile:            "/path/key.pem",
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected no error when both TLS files set, got: %v", err)
	}
}

func TestSetBuildInfo(t *testing.T) {
	SetBuildInfo("1.0.0", "abc123", "go1.21")
	if buildVersion != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %q", buildVersion)
	}
	if buildRevision != "abc123" {
		t.Errorf("expected revision abc123, got %q", buildRevision)
	}
	if buildGoVersion != "go1.21" {
		t.Errorf("expected goVersion go1.21, got %q", buildGoVersion)
	}
}

func TestValidate_AllValid(t *testing.T) {
	c := &Config{
		APIToken:              "token",
		LogLevel:              "info",
		ScrapeDelay:           0,
		TimeWindow:            60,
		RefreshInterval:       60,
		DiscoveryInterval:     21600,
		GraphQLBudgetPerWindow: 160,
		RESTBudgetPerWindow:   600,
		GatewayCategoryTopN:   25,
		RequestTimeout:        10,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestLoadFromEnv(t *testing.T) {
	// We cannot easily test Load() because it calls flag.Parse() which
	// conflicts with the test framework. But we can test the resolve functions
	// and Validate, which cover the important paths.
	os.Setenv("CF_API_TOKEN", "test-token")
	defer os.Unsetenv("CF_API_TOKEN")

	flagsSet := map[string]bool{}
	got := resolveString(flagsSet, "cf.api-token", "", "CF_API_TOKEN", "")
	if got != "test-token" {
		t.Errorf("expected test-token, got %q", got)
	}
}

func TestValidate_TLS_OnlyKey(t *testing.T) {
	c := &Config{
		APIToken:              "token",
		LogLevel:              "info",
		ScrapeDelay:           300,
		TimeWindow:            60,
		RefreshInterval:       60,
		DiscoveryInterval:     21600,
		GraphQLBudgetPerWindow: 160,
		RESTBudgetPerWindow:   600,
		GatewayCategoryTopN:   25,
		RequestTimeout:        10,
		TLSKeyFile:            "/path/key.pem",
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error when only TLS key is specified")
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	c := &Config{
		APIToken:              "",
		LogLevel:              "invalid",
		ScrapeDelay:           -1,
		TimeWindow:            -1,
		RefreshInterval:       -1,
		DiscoveryInterval:     -1,
		GraphQLBudgetPerWindow: -1,
		RESTBudgetPerWindow:   -1,
		GatewayCategoryTopN:   -1,
		RequestTimeout:        -1,
		TLSCertFile:           "cert-only",
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected multiple errors")
	}
	// Should contain multiple error messages
	errStr := err.Error()
	if !contains(errStr, "CF_API_TOKEN") {
		t.Error("expected API token error")
	}
	if !contains(errStr, "invalid log level") {
		t.Error("expected log level error")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
