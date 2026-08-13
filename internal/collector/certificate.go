package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/asymmetric-effort/prometheus-exporters/internal/cloudflare"
	"github.com/asymmetric-effort/prometheus-exporters/internal/governor"
	"github.com/asymmetric-effort/prometheus-exporters/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	certificateCollectorName = "certificate"

	metricCertExpirationTimestamp = "cloudflare_certificate_expiration_timestamp_seconds"
)

// certificatePack represents a certificate pack returned by the Cloudflare
// SSL/TLS REST API.
type certificatePack struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Certificates []certificateEntry  `json:"certificates"`
}

// certificateEntry represents a single certificate within a pack.
type certificateEntry struct {
	Issuer    string    `json:"issuer"`
	ExpiresOn time.Time `json:"expires_on"`
}

// certGauge holds a snapshot of a single certificate's expiration metric.
type certGauge struct {
	zoneID   string
	zoneName string
	issuer   string
	certType string
	expiry   float64
}

// CertificateCollector collects certificate lifecycle metrics from the
// Cloudflare SSL/TLS REST API.
type CertificateCollector struct {
	client      *cloudflare.Client
	store       *store.Store
	selfMetrics *SelfMetrics
	logger      *zap.Logger

	scrapeDelay     int
	timeWindow      int
	refreshInterval int

	accountIDs []string
	zones      []ZoneInfo

	descExpiration *prometheus.Desc

	// gauges holds the latest scraped values.
	gauges []certGauge
}

// NewCertificateCollector creates a new CertificateCollector and registers its
// prometheus descriptors with the provided registry.
func NewCertificateCollector(
	client *cloudflare.Client,
	st *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay, timeWindow, refreshInterval int,
	accountIDs []string,
	zones []ZoneInfo,
	reg prometheus.Registerer,
) (*CertificateCollector, error) {
	c := &CertificateCollector{
		client:          client,
		store:           st,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(certificateCollectorName),
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		accountIDs:      accountIDs,
		zones:           zones,
		descExpiration: prometheus.NewDesc(
			metricCertExpirationTimestamp,
			"Absolute certificate expiry Unix timestamp. Exposing absolute timestamps rather than days_remaining is deliberate.",
			[]string{"zone_id", "zone_name", "issuer", "type"},
			nil,
		),
	}

	if err := reg.Register(&certificatePromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering certificate collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

func (c *CertificateCollector) Name() string                    { return certificateCollectorName }
func (c *CertificateCollector) Priority() governor.PriorityClass { return PriorityBackground }
func (c *CertificateCollector) Interval() time.Duration          { return time.Duration(c.refreshInterval) * time.Second }
func (c *CertificateCollector) RequiredDatasets() []string        { return []string{"ssl_certificate_packs"} }
func (c *CertificateCollector) Scope() Scope                     { return ScopeZone }

// Describe returns a human-readable description.
func (c *CertificateCollector) Describe() string {
	return "Collects certificate lifecycle metrics (expiration timestamps) from the SSL/TLS REST API."
}

// Collect fetches certificate packs for every configured zone and records
// expiration timestamps.
func (c *CertificateCollector) Collect(ctx context.Context) error {
	start := time.Now()

	var newGauges []certGauge
	var lastErr error

	for _, z := range c.zones {
		packs, err := c.fetchCertificatePacks(ctx, z.ID)
		if err != nil {
			c.logger.Error("failed to fetch certificate packs",
				zap.String("zone_id", z.ID),
				zap.String("zone_name", z.Name),
				zap.Error(err),
			)
			lastErr = err
			continue
		}

		for _, pack := range packs {
			for _, cert := range pack.Certificates {
				newGauges = append(newGauges, certGauge{
					zoneID:   z.ID,
					zoneName: z.Name,
					issuer:   cert.Issuer,
					certType: pack.Type,
					expiry:   float64(cert.ExpiresOn.Unix()),
				})
			}
		}
	}

	// Atomically replace gauge snapshot.
	c.gauges = newGauges

	duration := time.Since(start)
	if lastErr != nil {
		c.selfMetrics.RecordCollectionError(certificateCollectorName, "rest_api", duration)
		return lastErr
	}
	c.selfMetrics.RecordCollectionSuccess(certificateCollectorName, duration, time.Now())
	return nil
}

func (c *CertificateCollector) fetchCertificatePacks(ctx context.Context, zoneID string) ([]certificatePack, error) {
	path := fmt.Sprintf("/zones/%s/ssl/certificate_packs", zoneID)

	raw, _, err := c.client.RESTGet(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("fetching certificate packs for zone %s: %w", zoneID, err)
	}

	restResp, err := cloudflare.ParseRESTResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate packs response for zone %s: %w", zoneID, err)
	}

	var packs []certificatePack
	if err := json.Unmarshal(restResp.Result, &packs); err != nil {
		return nil, fmt.Errorf("unmarshaling certificate packs for zone %s: %w", zoneID, err)
	}

	return packs, nil
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

// certificatePromCollector adapts CertificateCollector to the prometheus.Collector
// interface. This wrapper is necessary because collector.Collector and
// prometheus.Collector both define Describe and Collect methods with
// incompatible signatures.
type certificatePromCollector struct {
	inner *CertificateCollector
}

// Describe implements prometheus.Collector.
func (p *certificatePromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.descExpiration
}

// Collect implements prometheus.Collector. It reads the latest gauge snapshot
// and emits them as constant metrics.
func (p *certificatePromCollector) Collect(ch chan<- prometheus.Metric) {
	for _, g := range p.inner.gauges {
		ch <- prometheus.MustNewConstMetric(
			p.inner.descExpiration,
			prometheus.GaugeValue,
			g.expiry,
			g.zoneID, g.zoneName, g.issuer, g.certType,
		)
	}
}
