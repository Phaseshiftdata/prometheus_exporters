// cloudflare_exporter is a Prometheus exporter for Cloudflare Zero Trust,
// DNS, and domain metrics.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/prometheus/client_golang/prometheus"

	cfclient "github.com/phaseshiftdata/prometheus_exporters/internal/cloudflare"
	"github.com/phaseshiftdata/prometheus_exporters/internal/collector"
	"github.com/phaseshiftdata/prometheus_exporters/internal/config"
	"github.com/phaseshiftdata/prometheus_exporters/internal/discovery"
	"github.com/phaseshiftdata/prometheus_exporters/internal/governor"
	"github.com/phaseshiftdata/prometheus_exporters/internal/scheduler"
	"github.com/phaseshiftdata/prometheus_exporters/internal/server"
	"github.com/phaseshiftdata/prometheus_exporters/internal/store"
	"github.com/phaseshiftdata/prometheus_exporters/src/secrets"
	"github.com/phaseshiftdata/prometheus_exporters/src/version"
)

func main() {
	os.Exit(execute())
}

func execute() int {
	if err := rootCmd().Execute(); err != nil {
		return 1
	}
	return 0
}

// runConfig holds all configuration needed by the run function, populated
// from Cobra flags and environment variable fallbacks.
type runConfig struct {
	APIToken               string
	Accounts               []string
	Zones                  []string
	ZonesExclude           []string
	ScrapeDelay            time.Duration
	TimeWindow             time.Duration
	RefreshInterval        time.Duration
	DiscoveryInterval      time.Duration
	GraphQLBudgetPerWindow int
	RESTBudgetPerWindow    int
	CollectorsEnabled      []string
	GatewayCategoryAllow   []string
	GatewayCategoryTopN    int
	RequestTimeout         time.Duration
	ListenAddress          string
	MetricsPath            string
	TLSCertFile            string
	TLSKeyFile             string
	BasicAuthUsername      string
	BasicAuthPassword      string
	LogLevel               string
	CapabilitiesOnly       bool
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

func rootCmd() *cobra.Command {
	var (
		apiToken             string
		accounts             string
		zones                string
		zonesExclude         string
		scrapeDelay          int
		timeWindow           int
		refreshInterval      int
		discoveryInterval    int
		graphqlBudget        int
		restBudget           int
		collectorsEnabled    string
		gatewayCategoryAllow string
		gatewayCategoryTopN  int
		requestTimeout       int
		listenAddress        string
		metricsPath          string
		tlsCertFile          string
		tlsKeyFile           string
		basicAuthUsername     string
		basicAuthPassword    string
		apiTokenFile         string
		basicAuthPasswordFile string
		logLevel             string
		capabilitiesOnly     bool
	)

	cmd := &cobra.Command{
		Use:   "cloudflare_exporter",
		Short: "Prometheus exporter for Cloudflare Zero Trust, DNS, and domain metrics",
		Version: fmt.Sprintf("%s (commit: %s, built: %s)",
			version.Version, version.GitCommit, version.BuildDate),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve file-based secrets before building the config.
			resolvedAPIToken := resolveString(cmd, "cf.api-token", apiToken, "CF_API_TOKEN", "")
			if apiTokenFile != "" {
				if resolvedAPIToken != "" {
					return fmt.Errorf("--cf.api-token and --cf.api-token-file are mutually exclusive")
				}
				v, err := secrets.ReadSecretFile(apiTokenFile)
				if err != nil {
					return fmt.Errorf("--cf.api-token-file: %w", err)
				}
				resolvedAPIToken = v
			}

			resolvedBasicAuthPassword := basicAuthPassword
			if basicAuthPasswordFile != "" {
				if resolvedBasicAuthPassword != "" {
					return fmt.Errorf("--web.basic-auth-password and --web.basic-auth-password-file are mutually exclusive")
				}
				v, err := secrets.ReadSecretFile(basicAuthPasswordFile)
				if err != nil {
					return fmt.Errorf("--web.basic-auth-password-file: %w", err)
				}
				resolvedBasicAuthPassword = v
			}

			rc := runConfig{
				APIToken:               resolvedAPIToken,
				Accounts:               splitCSV(resolveString(cmd, "cf.accounts", accounts, "CF_ACCOUNTS", "")),
				Zones:                  splitCSV(resolveString(cmd, "cf.zones", zones, "CF_ZONES", "")),
				ZonesExclude:           splitCSV(resolveString(cmd, "cf.zones-exclude", zonesExclude, "CF_ZONES_EXCLUDE", "")),
				ScrapeDelay:            time.Duration(resolveInt(cmd, "cf.scrape-delay", scrapeDelay, "CF_SCRAPE_DELAY_SECONDS", defaultScrapeDelaySeconds)) * time.Second,
				TimeWindow:             time.Duration(resolveInt(cmd, "cf.time-window", timeWindow, "CF_TIME_WINDOW_SECONDS", defaultTimeWindowSeconds)) * time.Second,
				RefreshInterval:        time.Duration(resolveInt(cmd, "cf.refresh-interval", refreshInterval, "CF_REFRESH_INTERVAL_SECONDS", defaultRefreshIntervalSeconds)) * time.Second,
				DiscoveryInterval:      time.Duration(resolveInt(cmd, "cf.discovery-interval", discoveryInterval, "CF_DISCOVERY_INTERVAL_SECONDS", defaultDiscoveryIntervalSeconds)) * time.Second,
				GraphQLBudgetPerWindow: resolveInt(cmd, "cf.graphql-budget", graphqlBudget, "CF_GRAPHQL_BUDGET_PER_WINDOW", defaultGraphQLBudgetPerWindow),
				RESTBudgetPerWindow:    resolveInt(cmd, "cf.rest-budget", restBudget, "CF_REST_BUDGET_PER_WINDOW", defaultRESTBudgetPerWindow),
				CollectorsEnabled:      splitCSV(resolveString(cmd, "cf.collectors-enabled", collectorsEnabled, "CF_COLLECTORS_ENABLED", "")),
				GatewayCategoryAllow:   splitCSV(resolveString(cmd, "cf.gateway-category-allowlist", gatewayCategoryAllow, "CF_GATEWAY_CATEGORY_ALLOWLIST", "")),
				GatewayCategoryTopN:    resolveInt(cmd, "cf.gateway-category-top-n", gatewayCategoryTopN, "CF_GATEWAY_CATEGORY_TOP_N", defaultGatewayCategoryTopN),
				RequestTimeout:         time.Duration(resolveInt(cmd, "cf.request-timeout", requestTimeout, "CF_REQUEST_TIMEOUT_SECONDS", defaultRequestTimeoutSeconds)) * time.Second,
				ListenAddress:          resolveString(cmd, "web.listen-address", listenAddress, "LISTEN_ADDRESS", defaultListenAddress),
				MetricsPath:            resolveString(cmd, "web.metrics-path", metricsPath, "METRICS_PATH", defaultMetricsPath),
				TLSCertFile:            tlsCertFile,
				TLSKeyFile:             tlsKeyFile,
				BasicAuthUsername:       basicAuthUsername,
				BasicAuthPassword:       resolvedBasicAuthPassword,
				LogLevel:               resolveString(cmd, "log.level", logLevel, "LOG_LEVEL", defaultLogLevel),
				CapabilitiesOnly:       capabilitiesOnly,
			}
			return run(cmd.Context(), rc)
		},
	}

	cmd.Flags().StringVar(&apiToken, "cf.api-token", "", "Cloudflare scoped API token (env: CF_API_TOKEN)")
	cmd.Flags().StringVar(&apiTokenFile, "cf.api-token-file", "", "Path to file containing Cloudflare API token")
	cmd.Flags().StringVar(&accounts, "cf.accounts", "", "Comma-separated account IDs (env: CF_ACCOUNTS)")
	cmd.Flags().StringVar(&zones, "cf.zones", "", "Comma-separated zone IDs to include (env: CF_ZONES)")
	cmd.Flags().StringVar(&zonesExclude, "cf.zones-exclude", "", "Comma-separated zone IDs to exclude (env: CF_ZONES_EXCLUDE)")

	cmd.Flags().IntVar(&scrapeDelay, "cf.scrape-delay", defaultScrapeDelaySeconds, "Propagation delay in seconds (env: CF_SCRAPE_DELAY_SECONDS)")
	cmd.Flags().IntVar(&timeWindow, "cf.time-window", defaultTimeWindowSeconds, "Query window width in seconds (env: CF_TIME_WINDOW_SECONDS)")
	cmd.Flags().IntVar(&refreshInterval, "cf.refresh-interval", defaultRefreshIntervalSeconds, "Base collection interval in seconds (env: CF_REFRESH_INTERVAL_SECONDS)")
	cmd.Flags().IntVar(&discoveryInterval, "cf.discovery-interval", defaultDiscoveryIntervalSeconds, "Capability re-discovery interval in seconds (env: CF_DISCOVERY_INTERVAL_SECONDS)")

	cmd.Flags().IntVar(&graphqlBudget, "cf.graphql-budget", defaultGraphQLBudgetPerWindow, "GraphQL budget per window (env: CF_GRAPHQL_BUDGET_PER_WINDOW)")
	cmd.Flags().IntVar(&restBudget, "cf.rest-budget", defaultRESTBudgetPerWindow, "REST budget per window (env: CF_REST_BUDGET_PER_WINDOW)")

	cmd.Flags().StringVar(&collectorsEnabled, "cf.collectors-enabled", "", "Comma-separated collector allow-list (env: CF_COLLECTORS_ENABLED)")

	cmd.Flags().StringVar(&gatewayCategoryAllow, "cf.gateway-category-allowlist", "", "Comma-separated Gateway category allowlist (env: CF_GATEWAY_CATEGORY_ALLOWLIST)")
	cmd.Flags().IntVar(&gatewayCategoryTopN, "cf.gateway-category-top-n", defaultGatewayCategoryTopN, "Bucket categories beyond top N into other (env: CF_GATEWAY_CATEGORY_TOP_N)")

	cmd.Flags().IntVar(&requestTimeout, "cf.request-timeout", defaultRequestTimeoutSeconds, "Per-request upstream timeout in seconds (env: CF_REQUEST_TIMEOUT_SECONDS)")

	cmd.Flags().StringVar(&listenAddress, "web.listen-address", defaultListenAddress, "Bind address (env: LISTEN_ADDRESS)")
	cmd.Flags().StringVar(&metricsPath, "web.metrics-path", defaultMetricsPath, "Exposition path (env: METRICS_PATH)")

	cmd.Flags().StringVar(&tlsCertFile, "web.tls-cert-file", "", "TLS certificate file path")
	cmd.Flags().StringVar(&tlsKeyFile, "web.tls-key-file", "", "TLS private key file path")

	cmd.Flags().StringVar(&basicAuthUsername, "web.basic-auth-username", "", "Basic auth username")
	cmd.Flags().StringVar(&basicAuthPassword, "web.basic-auth-password", "", "Basic auth password")
	cmd.Flags().StringVar(&basicAuthPasswordFile, "web.basic-auth-password-file", "", "Path to file containing basic auth password")

	cmd.Flags().StringVar(&logLevel, "log.level", defaultLogLevel, "Log level: debug, info, warn, error (env: LOG_LEVEL)")

	cmd.Flags().BoolVar(&capabilitiesOnly, "capabilities", false, "Print discovered capabilities and exit")

	return cmd
}

// toInternalConfig converts a runConfig to the internal config.Config struct
// used by registerCollectors and other internal plumbing.
func toInternalConfig(rc runConfig) *config.Config {
	return &config.Config{
		APIToken:                 rc.APIToken,
		Accounts:                 rc.Accounts,
		Zones:                    rc.Zones,
		ZonesExclude:             rc.ZonesExclude,
		ScrapeDelay:              rc.ScrapeDelay,
		TimeWindow:               rc.TimeWindow,
		RefreshInterval:          rc.RefreshInterval,
		DiscoveryInterval:        rc.DiscoveryInterval,
		GraphQLBudgetPerWindow:   rc.GraphQLBudgetPerWindow,
		RESTBudgetPerWindow:      rc.RESTBudgetPerWindow,
		CollectorsEnabled:        rc.CollectorsEnabled,
		GatewayCategoryAllowlist: rc.GatewayCategoryAllow,
		GatewayCategoryTopN:      rc.GatewayCategoryTopN,
		RequestTimeout:           rc.RequestTimeout,
		ListenAddress:            rc.ListenAddress,
		MetricsPath:              rc.MetricsPath,
		TLSCertFile:              rc.TLSCertFile,
		TLSKeyFile:               rc.TLSKeyFile,
		BasicAuthUsername:         rc.BasicAuthUsername,
		BasicAuthPassword:         rc.BasicAuthPassword,
		LogLevel:                 rc.LogLevel,
		CapabilitiesOnly:         rc.CapabilitiesOnly,
	}
}

// run is the main entry point for the exporter. It blocks until the context
// is canceled or a shutdown signal is received.
func run(ctx context.Context, rc runConfig, clientOverride ...*cfclient.Client) error {
	logger := buildLogger(rc.LogLevel)
	defer logger.Sync()

	logger.Info("starting cloudflare_exporter",
		zap.String("version", version.Version),
		zap.String("commit", version.GitCommit),
		zap.String("built", version.BuildDate),
		zap.String("go_version", runtime.Version()),
	)

	cfg := toInternalConfig(rc)

	// Create Cloudflare client
	var client *cfclient.Client
	if len(clientOverride) > 0 && clientOverride[0] != nil {
		client = clientOverride[0]
	} else {
		client = cfclient.NewClient(cfg.APIToken, cfg.RequestTimeout)
	}

	// Create aggregation store
	pruneAfter := 2 * cfg.ScrapeDelay
	aggregationStore := store.NewStore(pruneAfter)

	// Create quota governor
	gov := governor.NewGovernor(cfg.GraphQLBudgetPerWindow, cfg.RESTBudgetPerWindow)

	// Create self-instrumentation metrics
	selfMetrics := collector.NewSelfMetrics(version.Version, version.GitCommit, runtime.Version())

	// Create HTTP server
	srv := server.NewServer(server.Config{
		ListenAddress:     cfg.ListenAddress,
		MetricsPath:       cfg.MetricsPath,
		TLSCertFile:       cfg.TLSCertFile,
		TLSKeyFile:        cfg.TLSKeyFile,
		BasicAuthUsername:  cfg.BasicAuthUsername,
		BasicAuthPassword: cfg.BasicAuthPassword,
	}, logger)

	// Register self-metrics
	if err := selfMetrics.Register(srv.Registry()); err != nil {
		return fmt.Errorf("failed to register self-metrics: %w", err)
	}

	// Run capability discovery
	disc := discovery.NewDiscovery(client, logger, discovery.DiscoveryOptions{
		AccountIDs:         cfg.Accounts,
		ZoneIDs:            cfg.Zones,
		ZoneExcludeIDs:     cfg.ZonesExclude,
		ScrapeDelaySeconds: int(cfg.ScrapeDelay.Seconds()),
		TimeWindowSeconds:  int(cfg.TimeWindow.Seconds()),
	})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	matrix, err := disc.Run(ctx)
	if err != nil {
		logger.Error("initial discovery failed, starting with self-metrics only", zap.Error(err))
		if matrix == nil {
			matrix = discovery.NewCapabilityMatrix()
		}
	}

	// Handle --capabilities flag
	if cfg.CapabilitiesOnly {
		data, err := server.MarshalCapabilityMatrix(matrix)
		if err != nil {
			return fmt.Errorf("failed to serialize capabilities: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Set up capability matrix access for the server
	var matrixMu sync.RWMutex
	currentMatrix := matrix
	srv.SetCapabilityMatrixFunc(func() *discovery.CapabilityMatrix {
		matrixMu.RLock()
		defer matrixMu.RUnlock()
		return currentMatrix
	})

	// Update discovery metrics
	updateDiscoveryMetrics(selfMetrics, matrix)

	// Create scheduler
	sched := scheduler.NewScheduler(gov, logger)

	// Wire up scheduler callbacks
	sched.OnCollectionStart = makeCollectionStartCallback(logger)
	sched.OnCollectionComplete = makeCollectionCompleteCallback(selfMetrics)
	sched.OnCollectionShed = makeCollectionShedCallback(selfMetrics)

	// Register collectors based on discovery results
	registerCollectors(cfg, client, aggregationStore, selfMetrics, srv.Registry(), sched, matrix, logger)

	// Update budget metrics periodically
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				selfMetrics.SetAPIBudgetRemaining("graphql", float64(gov.Remaining(governor.GraphQL)))
				selfMetrics.SetAPIBudgetRemaining("rest", float64(gov.Remaining(governor.REST)))
			}
		}
	}()

	// Start the store pruning loop
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				aggregationStore.Prune()
			}
		}
	}()

	// Start periodic re-discovery
	go func() {
		ticker := time.NewTicker(cfg.DiscoveryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				logger.Info("running periodic re-discovery")
				newMatrix, err := disc.Run(ctx)
				if err != nil {
					logger.Error("periodic discovery failed", zap.Error(err))
					continue
				}
				matrixMu.Lock()
				currentMatrix = newMatrix
				matrixMu.Unlock()
				updateDiscoveryMetrics(selfMetrics, newMatrix)
				selfMetrics.DiscoverySuccess.Set(float64(time.Now().Unix()))
			}
		}
	}()

	// Start scheduler
	sched.Start(ctx)
	defer sched.Stop()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
			logger.Info("received shutdown signal")
		case <-ctx.Done():
		}
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("server shutdown error", zap.Error(err))
		}
	}()

	// Start HTTP server (blocks)
	if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
		return fmt.Errorf("server error: %w", err)
	}

	logger.Info("cloudflare_exporter stopped")
	return nil
}

func buildLogger(level string) *zap.Logger {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	cfg := zap.Config{
		Level:       zap.NewAtomicLevelAt(zapLevel),
		Development: false,
		Encoding:    "json",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, err := cfg.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build logger: %v", err))
	}
	return logger
}

func registerCollectors(
	cfg *config.Config,
	client *cfclient.Client,
	aggregationStore *store.Store,
	selfMetrics *collector.SelfMetrics,
	registry *prometheus.Registry,
	sched *scheduler.Scheduler,
	matrix *discovery.CapabilityMatrix,
	logger *zap.Logger,
) {
	scrapeDelay := int(cfg.ScrapeDelay.Seconds())
	timeWindow := int(cfg.TimeWindow.Seconds())
	refreshInterval := int(cfg.RefreshInterval.Seconds())

	accountIDs := make([]string, len(matrix.Accounts))
	for i, a := range matrix.Accounts {
		accountIDs[i] = a.ID
	}

	type zoneInfo struct {
		ID   string
		Name string
	}
	zones := make([]zoneInfo, len(matrix.Zones))
	for i, z := range matrix.Zones {
		zones[i] = zoneInfo{ID: z.ID, Name: z.Name}
	}

	isEnabled := func(name string) bool {
		if len(cfg.CollectorsEnabled) == 0 {
			return true
		}
		for _, e := range cfg.CollectorsEnabled {
			if e == name {
				return true
			}
		}
		return false
	}

	isAvailable := func(name string) bool {
		cap, ok := matrix.GetDataset(name)
		return ok && cap.State == discovery.StateAvailable
	}

	// Build ZoneInfo and ZoneStatusInfo slices for collectors
	zoneInfos := make([]collector.ZoneInfo, len(zones))
	for i, z := range zones {
		zoneInfos[i] = collector.ZoneInfo{ID: z.ID, Name: z.Name}
	}
	zoneStatusInfos := make([]collector.ZoneStatusInfo, len(matrix.Zones))
	for i, z := range matrix.Zones {
		zoneStatusInfos[i] = collector.ZoneStatusInfo{ID: z.ID, Name: z.Name, Status: z.Status}
	}

	accountScoped := 0
	zoneScoped := 0

	// Zero Trust collectors (account-scoped, self-registering with prometheus)
	if isEnabled("access") && isAvailable("accessLoginRequestsAdaptiveGroups") {
		c, err := collector.NewAccessCollector(client, aggregationStore, selfMetrics, logger, scrapeDelay, timeWindow, refreshInterval, accountIDs, registry)
		if err != nil {
			logger.Error("failed to register access collector", zap.Error(err))
		} else {
			sched.Register(c)
			accountScoped++
		}
	}

	if isEnabled("gateway_dns") && isAvailable("gatewayResolverByCategoryAdaptiveGroups") {
		c, err := collector.NewGatewayDNSCollector(client, aggregationStore, selfMetrics, logger, scrapeDelay, timeWindow, refreshInterval, accountIDs, registry)
		if err != nil {
			logger.Error("failed to register gateway_dns collector", zap.Error(err))
		} else {
			sched.Register(c)
			accountScoped++
		}
	}

	if isEnabled("gateway_network") && isAvailable("gatewayL4SessionsAdaptiveGroups") {
		c, err := collector.NewGatewayNetworkCollector(client, aggregationStore, selfMetrics, logger, scrapeDelay, timeWindow, refreshInterval, accountIDs, registry)
		if err != nil {
			logger.Error("failed to register gateway_network collector", zap.Error(err))
		} else {
			sched.Register(c)
			accountScoped++
		}
	}

	if isEnabled("browser_isolation") && isAvailable("browserIsolationSessionsAdaptiveGroups") {
		c, err := collector.NewBrowserIsolationCollector(client, aggregationStore, selfMetrics, logger, scrapeDelay, timeWindow, refreshInterval, accountIDs, registry)
		if err != nil {
			logger.Error("failed to register browser_isolation collector", zap.Error(err))
		} else {
			sched.Register(c)
			accountScoped++
		}
	}

	if isEnabled("tunnel") && isAvailable("cloudflareTunnelsAnalyticsAdaptiveGroups") {
		c, err := collector.NewTunnelCollector(client, aggregationStore, selfMetrics, logger, scrapeDelay, timeWindow, refreshInterval, accountIDs, registry)
		if err != nil {
			logger.Error("failed to register tunnel collector", zap.Error(err))
		} else {
			sched.Register(c)
			accountScoped++
		}
	}

	// DNS/domain collectors (use ZoneInfo)
	if isEnabled("dns") && isAvailable("dnsAnalyticsAdaptiveGroups") && len(zoneInfos) > 0 {
		c, err := collector.NewDNSCollector(client, aggregationStore, selfMetrics, logger, scrapeDelay, timeWindow, refreshInterval, accountIDs, zoneInfos, registry)
		if err != nil {
			logger.Error("failed to register dns collector", zap.Error(err))
		} else {
			sched.Register(c)
			zoneScoped++
		}
	}

	if isEnabled("dns_firewall") && isAvailable("dnsFirewallAnalyticsAdaptiveGroups") {
		c, err := collector.NewDNSFirewallCollector(client, aggregationStore, selfMetrics, logger, scrapeDelay, timeWindow, refreshInterval, accountIDs, zoneInfos, registry)
		if err != nil {
			logger.Error("failed to register dns_firewall collector", zap.Error(err))
		} else {
			sched.Register(c)
			accountScoped++
		}
	}

	if isEnabled("domain") && len(accountIDs) > 0 {
		c, err := collector.NewDomainCollector(client, aggregationStore, selfMetrics, logger, scrapeDelay, timeWindow, refreshInterval, accountIDs, zoneInfos, registry)
		if err != nil {
			logger.Error("failed to register domain collector", zap.Error(err))
		} else {
			sched.Register(c)
			accountScoped++
		}
	}

	if isEnabled("certificate") && len(zoneInfos) > 0 {
		c, err := collector.NewCertificateCollector(client, aggregationStore, selfMetrics, logger, scrapeDelay, timeWindow, refreshInterval, accountIDs, zoneInfos, registry)
		if err != nil {
			logger.Error("failed to register certificate collector", zap.Error(err))
		} else {
			sched.Register(c)
			zoneScoped++
		}
	}

	if isEnabled("zone_status") && len(zoneInfos) > 0 {
		c, err := collector.NewZoneStatusCollector(client, aggregationStore, selfMetrics, logger, scrapeDelay, timeWindow, refreshInterval, accountIDs, zoneInfos, zoneStatusInfos, registry)
		if err != nil {
			logger.Error("failed to register zone_status collector", zap.Error(err))
		} else {
			sched.Register(c)
			zoneScoped++
		}
	}

	selfMetrics.CollectorsReg.WithLabelValues("account").Set(float64(accountScoped))
	selfMetrics.CollectorsReg.WithLabelValues("zone").Set(float64(zoneScoped))

	logger.Info("collectors registered",
		zap.Int("account_scoped", accountScoped),
		zap.Int("zone_scoped", zoneScoped),
		zap.Int("total", accountScoped+zoneScoped),
	)
}

func makeCollectionStartCallback(logger *zap.Logger) func(string) {
	return func(name string) {
		logger.Debug("collection starting", zap.String("collector", name))
	}
}

func makeCollectionCompleteCallback(selfMetrics *collector.SelfMetrics) func(string, time.Duration, error) {
	return func(name string, duration time.Duration, err error) {
		if err != nil {
			reason := "unknown"
			if _, ok := err.(*cfclient.RateLimitError); ok {
				reason = "rate_limited"
			}
			selfMetrics.RecordCollectionError(name, reason, duration)
		}
	}
}

func makeCollectionShedCallback(selfMetrics *collector.SelfMetrics) func(string) {
	return func(name string) {
		selfMetrics.RecordCollectionShed(name)
	}
}

func updateDiscoveryMetrics(selfMetrics *collector.SelfMetrics, matrix *discovery.CapabilityMatrix) {
	if matrix == nil {
		return
	}

	selfMetrics.DiscoverySuccess.Set(float64(matrix.DiscoveredAt.Unix()))

	freeZones := 0
	for _, z := range matrix.Zones {
		if z.Plan == "free" || z.Plan == "Free Website" {
			freeZones++
		}
	}
	selfMetrics.ZonesSkippedFree.Set(float64(freeZones))

	for _, cap := range matrix.AllDatasets() {
		selfMetrics.DatasetAvailable.WithLabelValues(string(cap.Scope), cap.Dataset, string(cap.State)).Set(1)
		if cap.State != discovery.StateAvailable {
			selfMetrics.DatasetsUnavail.WithLabelValues(cap.Dataset, string(cap.State)).Set(1)
		}
	}
}

// resolveString returns the flag value if it was explicitly set on the command
// line, otherwise the environment variable value if present, otherwise the
// provided default.
func resolveString(cmd *cobra.Command, flagName, flagVal, envKey, defaultVal string) string {
	if cmd.Flags().Changed(flagName) {
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

// resolveInt returns the flag value if it was explicitly set on the command
// line, otherwise parses the environment variable if present, otherwise
// returns the default.
func resolveInt(cmd *cobra.Command, flagName string, flagVal int, envKey string, defaultVal int) int {
	if cmd.Flags().Changed(flagName) {
		return flagVal
	}
	if v, ok := os.LookupEnv(envKey); ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return defaultVal
}

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
