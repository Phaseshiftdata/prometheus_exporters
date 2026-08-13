// Package config provides configuration loading for the Cloudflare Prometheus exporter.
//
// Configuration is resolved in the following order of precedence:
//
//	CLI flag > environment variable > default value
//
// Required fields are validated by [Config.Validate].
package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the Cloudflare Prometheus exporter.
type Config struct {
	// Cloudflare authentication
	APIToken string // CF_API_TOKEN — scoped API token (required, secret)

	// Cloudflare scope
	Accounts     []string // CF_ACCOUNTS — account IDs; empty means all discoverable
	Zones        []string // CF_ZONES — zone IDs to include; empty means all
	ZonesExclude []string // CF_ZONES_EXCLUDE — zone IDs to exclude

	// Timing
	ScrapeDelay       time.Duration // CF_SCRAPE_DELAY_SECONDS — propagation delay before querying a window
	TimeWindow        time.Duration // CF_TIME_WINDOW_SECONDS — width of each query window
	RefreshInterval   time.Duration // CF_REFRESH_INTERVAL_SECONDS — base collection interval
	DiscoveryInterval time.Duration // CF_DISCOVERY_INTERVAL_SECONDS — capability re-discovery interval

	// Rate-limiting budgets
	GraphQLBudgetPerWindow int // CF_GRAPHQL_BUDGET_PER_WINDOW — self-imposed ceiling (50% of 320)
	RESTBudgetPerWindow    int // CF_REST_BUDGET_PER_WINDOW — self-imposed ceiling (50% of 1200)

	// Collector controls
	CollectorsEnabled []string // CF_COLLECTORS_ENABLED — explicit collector allow-list; empty means all entitled

	// Gateway
	GatewayCategoryAllowlist []string // CF_GATEWAY_CATEGORY_ALLOWLIST — restrict Gateway category label values
	GatewayCategoryTopN      int      // CF_GATEWAY_CATEGORY_TOP_N — bucket categories beyond top N into "other"

	// HTTP client
	RequestTimeout time.Duration // CF_REQUEST_TIMEOUT_SECONDS — per-request upstream timeout

	// Server
	ListenAddress string // LISTEN_ADDRESS — bind address
	MetricsPath   string // METRICS_PATH — exposition path

	// TLS termination
	TLSCertFile string // TLS certificate file path
	TLSKeyFile  string // TLS private key file path

	// Basic authentication for endpoint protection
	BasicAuthUsername string // basic-auth username
	BasicAuthPassword string // basic-auth password

	// Logging
	LogLevel string // LOG_LEVEL — debug, info, warn, error

	// Operational modes
	CapabilitiesOnly bool // --capabilities — print discovered capabilities and exit

	// Build information (injected via ldflags)
	Version   string
	Revision  string
	GoVersion string
}

// defaults
const (
	defaultScrapeDelaySeconds       = 300
	defaultTimeWindowSeconds        = 60
	defaultRefreshIntervalSeconds   = 60
	defaultDiscoveryIntervalSeconds = 21600
	defaultGraphQLBudgetPerWindow   = 160
	defaultRESTBudgetPerWindow      = 600
	defaultGatewayCategoryTopN      = 25
	defaultRequestTimeoutSeconds    = 10
	defaultListenAddress            = ":9199"
	defaultMetricsPath              = "/metrics"
	defaultLogLevel                 = "info"
)

// Load reads configuration from CLI flags and environment variables.
// Resolution order: CLI flag > environment variable > default.
func Load() (*Config, error) {
	c := &Config{}

	// Track which flags are explicitly set on the command line.
	flagsSet := make(map[string]bool)

	// Define all flags. We use flag.StringVar / flag.IntVar / flag.BoolVar
	// with temporary holders, then apply env fallback logic after parsing.
	var (
		apiToken                string
		accounts                string
		zones                   string
		zonesExclude            string
		scrapeDelay             int
		timeWindow              int
		refreshInterval         int
		discoveryInterval       int
		graphqlBudget           int
		restBudget              int
		collectorsEnabled       string
		gatewayCategoryAllow    string
		gatewayCategoryTopN     int
		requestTimeout          int
		listenAddress           string
		metricsPath             string
		tlsCertFile             string
		tlsKeyFile              string
		basicAuthUsername        string
		basicAuthPassword       string
		logLevel                string
		capabilitiesOnly        bool
	)

	flag.StringVar(&apiToken, "cf.api-token", "", "Cloudflare scoped API token (env: CF_API_TOKEN)")
	flag.StringVar(&accounts, "cf.accounts", "", "Comma-separated account IDs (env: CF_ACCOUNTS)")
	flag.StringVar(&zones, "cf.zones", "", "Comma-separated zone IDs to include (env: CF_ZONES)")
	flag.StringVar(&zonesExclude, "cf.zones-exclude", "", "Comma-separated zone IDs to exclude (env: CF_ZONES_EXCLUDE)")

	flag.IntVar(&scrapeDelay, "cf.scrape-delay", defaultScrapeDelaySeconds, "Propagation delay in seconds (env: CF_SCRAPE_DELAY_SECONDS)")
	flag.IntVar(&timeWindow, "cf.time-window", defaultTimeWindowSeconds, "Query window width in seconds (env: CF_TIME_WINDOW_SECONDS)")
	flag.IntVar(&refreshInterval, "cf.refresh-interval", defaultRefreshIntervalSeconds, "Base collection interval in seconds (env: CF_REFRESH_INTERVAL_SECONDS)")
	flag.IntVar(&discoveryInterval, "cf.discovery-interval", defaultDiscoveryIntervalSeconds, "Capability re-discovery interval in seconds (env: CF_DISCOVERY_INTERVAL_SECONDS)")

	flag.IntVar(&graphqlBudget, "cf.graphql-budget", defaultGraphQLBudgetPerWindow, "GraphQL budget per window (env: CF_GRAPHQL_BUDGET_PER_WINDOW)")
	flag.IntVar(&restBudget, "cf.rest-budget", defaultRESTBudgetPerWindow, "REST budget per window (env: CF_REST_BUDGET_PER_WINDOW)")

	flag.StringVar(&collectorsEnabled, "cf.collectors-enabled", "", "Comma-separated collector allow-list (env: CF_COLLECTORS_ENABLED)")

	flag.StringVar(&gatewayCategoryAllow, "cf.gateway-category-allowlist", "", "Comma-separated Gateway category allowlist (env: CF_GATEWAY_CATEGORY_ALLOWLIST)")
	flag.IntVar(&gatewayCategoryTopN, "cf.gateway-category-top-n", defaultGatewayCategoryTopN, "Bucket categories beyond top N into other (env: CF_GATEWAY_CATEGORY_TOP_N)")

	flag.IntVar(&requestTimeout, "cf.request-timeout", defaultRequestTimeoutSeconds, "Per-request upstream timeout in seconds (env: CF_REQUEST_TIMEOUT_SECONDS)")

	flag.StringVar(&listenAddress, "web.listen-address", defaultListenAddress, "Bind address (env: LISTEN_ADDRESS)")
	flag.StringVar(&metricsPath, "web.metrics-path", defaultMetricsPath, "Exposition path (env: METRICS_PATH)")

	flag.StringVar(&tlsCertFile, "web.tls-cert-file", "", "TLS certificate file path")
	flag.StringVar(&tlsKeyFile, "web.tls-key-file", "", "TLS private key file path")

	flag.StringVar(&basicAuthUsername, "web.basic-auth-username", "", "Basic auth username")
	flag.StringVar(&basicAuthPassword, "web.basic-auth-password", "", "Basic auth password")

	flag.StringVar(&logLevel, "log.level", defaultLogLevel, "Log level: debug, info, warn, error (env: LOG_LEVEL)")

	flag.BoolVar(&capabilitiesOnly, "capabilities", false, "Print discovered capabilities and exit")

	flag.Parse()

	// Record which flags were explicitly set on the command line.
	flag.Visit(func(f *flag.Flag) {
		flagsSet[f.Name] = true
	})

	// Resolve each field: CLI flag > env var > default.
	c.APIToken = resolveString(flagsSet, "cf.api-token", apiToken, "CF_API_TOKEN", "")
	c.Accounts = splitCSV(resolveString(flagsSet, "cf.accounts", accounts, "CF_ACCOUNTS", ""))
	c.Zones = splitCSV(resolveString(flagsSet, "cf.zones", zones, "CF_ZONES", ""))
	c.ZonesExclude = splitCSV(resolveString(flagsSet, "cf.zones-exclude", zonesExclude, "CF_ZONES_EXCLUDE", ""))

	c.ScrapeDelay = time.Duration(resolveInt(flagsSet, "cf.scrape-delay", scrapeDelay, "CF_SCRAPE_DELAY_SECONDS", defaultScrapeDelaySeconds)) * time.Second
	c.TimeWindow = time.Duration(resolveInt(flagsSet, "cf.time-window", timeWindow, "CF_TIME_WINDOW_SECONDS", defaultTimeWindowSeconds)) * time.Second
	c.RefreshInterval = time.Duration(resolveInt(flagsSet, "cf.refresh-interval", refreshInterval, "CF_REFRESH_INTERVAL_SECONDS", defaultRefreshIntervalSeconds)) * time.Second
	c.DiscoveryInterval = time.Duration(resolveInt(flagsSet, "cf.discovery-interval", discoveryInterval, "CF_DISCOVERY_INTERVAL_SECONDS", defaultDiscoveryIntervalSeconds)) * time.Second

	c.GraphQLBudgetPerWindow = resolveInt(flagsSet, "cf.graphql-budget", graphqlBudget, "CF_GRAPHQL_BUDGET_PER_WINDOW", defaultGraphQLBudgetPerWindow)
	c.RESTBudgetPerWindow = resolveInt(flagsSet, "cf.rest-budget", restBudget, "CF_REST_BUDGET_PER_WINDOW", defaultRESTBudgetPerWindow)

	c.CollectorsEnabled = splitCSV(resolveString(flagsSet, "cf.collectors-enabled", collectorsEnabled, "CF_COLLECTORS_ENABLED", ""))

	c.GatewayCategoryAllowlist = splitCSV(resolveString(flagsSet, "cf.gateway-category-allowlist", gatewayCategoryAllow, "CF_GATEWAY_CATEGORY_ALLOWLIST", ""))
	c.GatewayCategoryTopN = resolveInt(flagsSet, "cf.gateway-category-top-n", gatewayCategoryTopN, "CF_GATEWAY_CATEGORY_TOP_N", defaultGatewayCategoryTopN)

	c.RequestTimeout = time.Duration(resolveInt(flagsSet, "cf.request-timeout", requestTimeout, "CF_REQUEST_TIMEOUT_SECONDS", defaultRequestTimeoutSeconds)) * time.Second

	c.ListenAddress = resolveString(flagsSet, "web.listen-address", listenAddress, "LISTEN_ADDRESS", defaultListenAddress)
	c.MetricsPath = resolveString(flagsSet, "web.metrics-path", metricsPath, "METRICS_PATH", defaultMetricsPath)

	c.TLSCertFile = tlsCertFile
	c.TLSKeyFile = tlsKeyFile

	c.BasicAuthUsername = basicAuthUsername
	c.BasicAuthPassword = basicAuthPassword

	c.LogLevel = resolveString(flagsSet, "log.level", logLevel, "LOG_LEVEL", defaultLogLevel)

	c.CapabilitiesOnly = capabilitiesOnly

	if err := c.Validate(); err != nil {
		return nil, err
	}

	return c, nil
}

// Validate checks that required configuration fields are present and that
// values are within acceptable ranges.
func (c *Config) Validate() error {
	var errs []string

	if c.APIToken == "" && !c.CapabilitiesOnly {
		errs = append(errs, "CF_API_TOKEN is required")
	}

	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
		// valid
	default:
		errs = append(errs, fmt.Sprintf("invalid log level %q: must be one of debug, info, warn, error", c.LogLevel))
	}

	if c.ScrapeDelay < 0 {
		errs = append(errs, "CF_SCRAPE_DELAY_SECONDS must be non-negative")
	}
	if c.TimeWindow <= 0 {
		errs = append(errs, "CF_TIME_WINDOW_SECONDS must be positive")
	}
	if c.RefreshInterval <= 0 {
		errs = append(errs, "CF_REFRESH_INTERVAL_SECONDS must be positive")
	}
	if c.DiscoveryInterval <= 0 {
		errs = append(errs, "CF_DISCOVERY_INTERVAL_SECONDS must be positive")
	}
	if c.GraphQLBudgetPerWindow <= 0 {
		errs = append(errs, "CF_GRAPHQL_BUDGET_PER_WINDOW must be positive")
	}
	if c.RESTBudgetPerWindow <= 0 {
		errs = append(errs, "CF_REST_BUDGET_PER_WINDOW must be positive")
	}
	if c.GatewayCategoryTopN <= 0 {
		errs = append(errs, "CF_GATEWAY_CATEGORY_TOP_N must be positive")
	}
	if c.RequestTimeout <= 0 {
		errs = append(errs, "CF_REQUEST_TIMEOUT_SECONDS must be positive")
	}

	// TLS: both or neither must be specified.
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		errs = append(errs, "both TLS cert and key files must be specified, or neither")
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration errors:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// resolveString returns the CLI flag value if it was explicitly set, otherwise
// the environment variable value if present, otherwise the provided default.
func resolveString(flagsSet map[string]bool, flagName, flagVal, envKey, defaultVal string) string {
	if flagsSet[flagName] {
		return flagVal
	}
	if v, ok := os.LookupEnv(envKey); ok {
		return v
	}
	if defaultVal != "" {
		return defaultVal
	}
	return flagVal
}

// resolveInt returns the CLI flag value if it was explicitly set, otherwise
// parses the environment variable if present, otherwise returns the default.
func resolveInt(flagsSet map[string]bool, flagName string, flagVal int, envKey string, defaultVal int) int {
	if flagsSet[flagName] {
		return flagVal
	}
	if v, ok := os.LookupEnv(envKey); ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return defaultVal
}

// SetBuildInfo sets the build information on the global defaults. These are
// typically injected via ldflags at compile time.
func SetBuildInfo(version, revision, goVersion string) {
	// Build info is applied to Config instances after Load() returns.
	buildVersion = version
	buildRevision = revision
	buildGoVersion = goVersion
}

var (
	buildVersion   string
	buildRevision  string
	buildGoVersion string
)

// splitCSV splits a comma-separated string into trimmed, non-empty parts.
// An empty input returns nil.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
