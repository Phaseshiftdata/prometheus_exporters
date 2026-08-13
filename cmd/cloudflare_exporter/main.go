// cloudflare_exporter is a Prometheus exporter for Cloudflare Zero Trust,
// DNS, and domain metrics.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/prometheus/client_golang/prometheus"

	cfclient "github.com/asymmetric-effort/prometheus-exporters/internal/cloudflare"
	"github.com/asymmetric-effort/prometheus-exporters/internal/collector"
	"github.com/asymmetric-effort/prometheus-exporters/internal/config"
	"github.com/asymmetric-effort/prometheus-exporters/internal/discovery"
	"github.com/asymmetric-effort/prometheus-exporters/internal/governor"
	"github.com/asymmetric-effort/prometheus-exporters/internal/scheduler"
	"github.com/asymmetric-effort/prometheus-exporters/internal/server"
	"github.com/asymmetric-effort/prometheus-exporters/internal/store"
)

// Build-time variables injected via ldflags.
var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	config.SetBuildInfo(version, revision, runtime.Version())

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	logger := buildLogger(cfg.LogLevel)
	defer logger.Sync()

	if err := Run(context.Background(), cfg, logger); err != nil {
		logger.Fatal("exporter error", zap.Error(err))
	}
}

// Run is the main entry point for the exporter, extracted from main() so it
// can be tested. It blocks until the context is canceled or a shutdown signal
// is received. An optional *cfclient.Client may be passed for testing; if nil
// a new client is created from the config.
func Run(ctx context.Context, cfg *config.Config, logger *zap.Logger, clientOverride ...*cfclient.Client) error {
	logger.Info("starting cloudflare_exporter",
		zap.String("version", version),
		zap.String("revision", revision),
		zap.String("go_version", runtime.Version()),
	)

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
	selfMetrics := collector.NewSelfMetrics(version, revision, runtime.Version())

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
