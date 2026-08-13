package collector

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// SelfMetrics holds all self-instrumentation metrics for the exporter.
type SelfMetrics struct {
	CollectionDuration *prometheus.HistogramVec
	LastUpdated        *prometheus.GaugeVec
	LastUpdatedGlobal  prometheus.Gauge
	DataMaxTimestamp   *prometheus.GaugeVec
	DataAge            *prometheus.GaugeVec
	CollectionErrors   *prometheus.CounterVec
	APIRequests        *prometheus.CounterVec
	APIBudgetRemaining *prometheus.GaugeVec
	CollectionsShed    *prometheus.CounterVec
	DatasetsUnavail    *prometheus.GaugeVec
	DatasetAvailable   *prometheus.GaugeVec
	DiscoverySuccess   prometheus.Gauge
	CollectorsReg      *prometheus.GaugeVec
	ZonesSkippedFree   prometheus.Gauge
	BuildInfo          *prometheus.GaugeVec

	mu                sync.RWMutex
	lastUpdatedTimes  map[string]time.Time
	dataMaxTimestamps map[string]time.Time
}

// NewSelfMetrics creates and returns all self-instrumentation metrics.
func NewSelfMetrics(version, revision, goVersion string) *SelfMetrics {
	m := &SelfMetrics{
		lastUpdatedTimes:  make(map[string]time.Time),
		dataMaxTimestamps: make(map[string]time.Time),

		CollectionDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cloudflare_exporter_collection_duration_seconds",
			Help:    "Duration of collection runs by collector.",
			Buckets: prometheus.DefBuckets,
		}, []string{"collector"}),

		LastUpdated: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cloudflare_exporter_last_updated_timestamp_seconds",
			Help: "Unix timestamp of the last successful collection for each collector.",
		}, []string{"collector"}),

		LastUpdatedGlobal: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cloudflare_exporter_last_updated_global_timestamp_seconds",
			Help: "Minimum of per-collector last_updated timestamps. Reflects the worst-case staleness across all collectors.",
		}),

		DataMaxTimestamp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cloudflare_data_max_timestamp_seconds",
			Help: "Unix timestamp of the end of the most recent query window whose rows were applied. Answers how current the data is, not when it was fetched.",
		}, []string{"collector"}),

		DataAge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cloudflare_exporter_data_age_seconds",
			Help: "Seconds since the latest data point. Under healthy operation this sits near CF_SCRAPE_DELAY_SECONDS + CF_TIME_WINDOW_SECONDS, not near zero.",
		}, []string{"collector"}),

		CollectionErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cloudflare_exporter_collection_errors_total",
			Help: "Total collection errors by collector and reason.",
		}, []string{"collector", "reason"}),

		APIRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cloudflare_exporter_api_requests_total",
			Help: "Total API requests by surface (graphql or rest) and HTTP status.",
		}, []string{"api", "status"}),

		APIBudgetRemaining: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cloudflare_exporter_api_budget_remaining",
			Help: "Remaining API budget by surface in the current 5-minute window.",
		}, []string{"api"}),

		CollectionsShed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cloudflare_exporter_collections_shed_total",
			Help: "Total collections shed due to budget exhaustion by collector.",
		}, []string{"collector"}),

		DatasetsUnavail: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cloudflare_exporter_datasets_unavailable",
			Help: "Number of datasets unavailable by reason.",
		}, []string{"dataset", "reason"}),

		DatasetAvailable: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cloudflare_exporter_dataset_available",
			Help: "Dataset availability state from capability discovery.",
		}, []string{"scope", "dataset", "state"}),

		DiscoverySuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cloudflare_exporter_discovery_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful capability discovery.",
		}),

		CollectorsReg: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cloudflare_exporter_collectors_registered",
			Help: "Number of registered collectors by scope.",
		}, []string{"scope"}),

		ZonesSkippedFree: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cloudflare_zones_skipped_free_tier",
			Help: "Number of zones skipped because they are on the Free plan.",
		}),

		BuildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cloudflare_exporter_build_info",
			Help: "Build information for the cloudflare_exporter.",
		}, []string{"version", "revision", "go_version"}),
	}

	// Set build info
	m.BuildInfo.WithLabelValues(version, revision, goVersion).Set(1)

	return m
}

// Register registers all metrics with the given registry.
func (m *SelfMetrics) Register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		m.CollectionDuration,
		m.LastUpdated,
		m.LastUpdatedGlobal,
		m.DataMaxTimestamp,
		m.DataAge,
		m.CollectionErrors,
		m.APIRequests,
		m.APIBudgetRemaining,
		m.CollectionsShed,
		m.DatasetsUnavail,
		m.DatasetAvailable,
		m.DiscoverySuccess,
		m.CollectorsReg,
		m.ZonesSkippedFree,
		m.BuildInfo,
	}

	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return err
		}
	}

	return nil
}

// RecordCollectionSuccess records a successful collection.
func (m *SelfMetrics) RecordCollectionSuccess(collector string, duration time.Duration, dataMaxTime time.Time) {
	now := time.Now()

	m.CollectionDuration.WithLabelValues(collector).Observe(duration.Seconds())
	m.LastUpdated.WithLabelValues(collector).Set(float64(now.Unix()))
	m.DataMaxTimestamp.WithLabelValues(collector).Set(float64(dataMaxTime.Unix()))
	m.DataAge.WithLabelValues(collector).Set(float64(now.Unix() - dataMaxTime.Unix()))

	m.mu.Lock()
	m.lastUpdatedTimes[collector] = now
	m.dataMaxTimestamps[collector] = dataMaxTime
	m.mu.Unlock()

	m.updateGlobalLastUpdated()
}

// RecordCollectionError records a collection error.
func (m *SelfMetrics) RecordCollectionError(collector, reason string, duration time.Duration) {
	m.CollectionDuration.WithLabelValues(collector).Observe(duration.Seconds())
	m.CollectionErrors.WithLabelValues(collector, reason).Inc()
}

// RecordCollectionShed records a collection that was shed due to budget.
func (m *SelfMetrics) RecordCollectionShed(collector string) {
	m.CollectionsShed.WithLabelValues(collector).Inc()
}

// RecordAPIRequest records an API request.
func (m *SelfMetrics) RecordAPIRequest(api, status string) {
	m.APIRequests.WithLabelValues(api, status).Inc()
}

// SetAPIBudgetRemaining sets the remaining API budget.
func (m *SelfMetrics) SetAPIBudgetRemaining(api string, remaining float64) {
	m.APIBudgetRemaining.WithLabelValues(api).Set(remaining)
}

func (m *SelfMetrics) updateGlobalLastUpdated() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.lastUpdatedTimes) == 0 {
		return
	}

	// Global is the minimum (worst case) across all collectors
	var minTime time.Time
	first := true
	for _, t := range m.lastUpdatedTimes {
		if first || t.Before(minTime) {
			minTime = t
			first = false
		}
	}

	m.LastUpdatedGlobal.Set(float64(minTime.Unix()))
}
