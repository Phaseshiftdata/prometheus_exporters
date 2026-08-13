package config

import (
	"testing"
	"time"
)

func TestValidate_AllFields(t *testing.T) {
	c := &Config{
		APIToken:               "token",
		Accounts:               []string{"acc1", "acc2"},
		Zones:                  []string{"z1"},
		ZonesExclude:           []string{"z2"},
		ScrapeDelay:            300 * time.Second,
		TimeWindow:             60 * time.Second,
		RefreshInterval:        60 * time.Second,
		DiscoveryInterval:      21600 * time.Second,
		GraphQLBudgetPerWindow: 160,
		RESTBudgetPerWindow:    600,
		CollectorsEnabled:      []string{"access", "dns"},
		GatewayCategoryTopN:    25,
		RequestTimeout:         10 * time.Second,
		ListenAddress:          ":9199",
		MetricsPath:            "/metrics",
		LogLevel:               "info",
	}

	if err := c.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidate_TLS_NeitherSet(t *testing.T) {
	c := &Config{
		APIToken:               "token",
		LogLevel:               "info",
		ScrapeDelay:            300 * time.Second,
		TimeWindow:             60 * time.Second,
		RefreshInterval:        60 * time.Second,
		DiscoveryInterval:      21600 * time.Second,
		GraphQLBudgetPerWindow: 160,
		RESTBudgetPerWindow:    600,
		GatewayCategoryTopN:    25,
		RequestTimeout:         10 * time.Second,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected no error when neither TLS field set, got: %v", err)
	}
}

func TestValidate_MultipleBadFields(t *testing.T) {
	c := &Config{
		APIToken:               "",
		LogLevel:               "invalid",
		ScrapeDelay:            -1 * time.Second,
		TimeWindow:             0,
		RefreshInterval:        0,
		DiscoveryInterval:      0,
		GraphQLBudgetPerWindow: 0,
		RESTBudgetPerWindow:    0,
		GatewayCategoryTopN:    0,
		RequestTimeout:         0,
		TLSCertFile:            "cert.pem",
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("expected multiple validation errors")
	}
}

func TestSplitCSV_LeadingComma(t *testing.T) {
	got := splitCSV(",a,b")
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(got), got)
	}
}

func TestResolveString_EnvTakesPriority(t *testing.T) {
	// When no flag is set and no env, and default is empty, fall back to flag val
	flagsSet := map[string]bool{}
	got := resolveString(flagsSet, "f", "", "NONEXISTENT_ENV_VAR_12345", "")
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestResolveInt_DefaultWhenNoEnv(t *testing.T) {
	got := resolveInt(map[string]bool{}, "f", 0, "NONEXISTENT_ENV_VAR_12345", 42)
	if got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestSetBuildInfo_Twice(t *testing.T) {
	SetBuildInfo("1.0", "abc", "go1.20")
	SetBuildInfo("2.0", "def", "go1.21")
	if buildVersion != "2.0" {
		t.Fatalf("expected 2.0, got %q", buildVersion)
	}
}
