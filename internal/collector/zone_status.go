package collector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/cloudflare"
	"github.com/phaseshiftdata/prometheus_exporters/internal/governor"
	"github.com/phaseshiftdata/prometheus_exporters/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	zoneStatusCollectorName = "zone_status"

	metricZoneStatus = "cloudflare_zone_status"
)

// ZoneStatusInfo holds zone metadata including its operational status. This is
// typically populated during discovery and passed to the collector at
// construction time so no additional API calls are needed.
type ZoneStatusInfo struct {
	ID     string
	Name   string
	Status string
}

// ZoneStatusCollector exposes zone status as a constant-1 info-style gauge.
// It reads from the zone list already fetched during discovery and does not
// make separate API calls.
type ZoneStatusCollector struct {
	client      *cloudflare.Client
	store       *store.Store
	selfMetrics *SelfMetrics
	logger      *zap.Logger

	scrapeDelay     int
	timeWindow      int
	refreshInterval int

	accountIDs []string
	zones      []ZoneInfo

	mu         sync.RWMutex
	zoneStatus []ZoneStatusInfo

	descStatus *prometheus.Desc
}

// NewZoneStatusCollector creates a new ZoneStatusCollector and registers its
// prometheus descriptors with the provided registry. The zoneStatus slice
// provides the pre-fetched zone status information; if nil, the collector
// derives status entries from the zones slice with status "unknown".
func NewZoneStatusCollector(
	client *cloudflare.Client,
	st *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay, timeWindow, refreshInterval int,
	accountIDs []string,
	zones []ZoneInfo,
	zoneStatus []ZoneStatusInfo,
	reg prometheus.Registerer,
) (*ZoneStatusCollector, error) {
	if zoneStatus == nil {
		zoneStatus = make([]ZoneStatusInfo, len(zones))
		for i, z := range zones {
			zoneStatus[i] = ZoneStatusInfo{
				ID:     z.ID,
				Name:   z.Name,
				Status: "unknown",
			}
		}
	}

	c := &ZoneStatusCollector{
		client:          client,
		store:           st,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(zoneStatusCollectorName),
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		accountIDs:      accountIDs,
		zones:           zones,
		zoneStatus:      zoneStatus,
		descStatus: prometheus.NewDesc(
			metricZoneStatus,
			"Zone status info metric (constant value 1). The status label carries the operational state.",
			[]string{"zone_id", "zone_name", "status"},
			nil,
		),
	}

	if err := reg.Register(&zoneStatusPromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering zone_status collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

func (c *ZoneStatusCollector) Name() string                    { return zoneStatusCollectorName }
func (c *ZoneStatusCollector) Priority() governor.PriorityClass { return PriorityBackground }
func (c *ZoneStatusCollector) Interval() time.Duration          { return time.Duration(c.refreshInterval) * time.Second }
func (c *ZoneStatusCollector) RequiredDatasets() []string        { return []string{} }
func (c *ZoneStatusCollector) Scope() Scope                     { return ScopeZone }

// Describe returns a human-readable description.
func (c *ZoneStatusCollector) Describe() string {
	return "Exposes zone operational status as a constant-1 info-style gauge metric."
}

// Collect is a no-op for the zone status collector because zone status is
// provided at construction time (from the discovery phase). It simply records
// a successful collection.
func (c *ZoneStatusCollector) Collect(ctx context.Context) error {
	start := time.Now()
	duration := time.Since(start)
	c.selfMetrics.RecordCollectionSuccess(zoneStatusCollectorName, duration, time.Now())
	return nil
}

// UpdateZoneStatus replaces the zone status data. This can be called when
// discovery refreshes the zone list.
func (c *ZoneStatusCollector) UpdateZoneStatus(zoneStatus []ZoneStatusInfo) {
	c.mu.Lock()
	c.zoneStatus = zoneStatus
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

// zoneStatusPromCollector adapts ZoneStatusCollector to the prometheus.Collector
// interface. This wrapper is necessary because collector.Collector and
// prometheus.Collector both define Describe and Collect methods with
// incompatible signatures.
type zoneStatusPromCollector struct {
	inner *ZoneStatusCollector
}

// Describe implements prometheus.Collector.
func (p *zoneStatusPromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.descStatus
}

// Collect implements prometheus.Collector. It reads the zone status snapshot
// and emits them as constant metrics.
func (p *zoneStatusPromCollector) Collect(ch chan<- prometheus.Metric) {
	p.inner.mu.RLock()
	defer p.inner.mu.RUnlock()
	for _, z := range p.inner.zoneStatus {
		ch <- prometheus.MustNewConstMetric(
			p.inner.descStatus,
			prometheus.GaugeValue,
			1,
			z.ID, z.Name, z.Status,
		)
	}
}
