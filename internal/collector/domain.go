package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/cloudflare"
	"github.com/phaseshiftdata/prometheus_exporters/internal/governor"
	"github.com/phaseshiftdata/prometheus_exporters/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	domainCollectorName = "domain"

	metricDomainExpirationTimestamp = "cloudflare_domain_expiration_timestamp_seconds"
	metricDomainAutoRenew           = "cloudflare_domain_auto_renew"
	metricDomainLocked              = "cloudflare_domain_locked"
)

// registrarDomain represents a single domain returned by the Cloudflare
// Registrar REST API.
type registrarDomain struct {
	DomainName    string    `json:"name"`
	ExpiresAt     time.Time `json:"expires_at"`
	AutoRenew     bool      `json:"auto_renew"`
	Locked        bool      `json:"locked"`
}

// DomainCollector collects domain lifecycle metrics from the Cloudflare
// Registrar REST API.
type DomainCollector struct {
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
	descAutoRenew  *prometheus.Desc
	descLocked     *prometheus.Desc

	// gauges holds the latest scraped values keyed by account_id + domain.
	gauges map[string]domainGauges
}

type domainGauges struct {
	accountID  string
	domain     string
	expiration float64
	autoRenew  float64
	locked     float64
}

// NewDomainCollector creates a new DomainCollector and registers its prometheus
// descriptors with the provided registry.
func NewDomainCollector(
	client *cloudflare.Client,
	st *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay, timeWindow, refreshInterval int,
	accountIDs []string,
	zones []ZoneInfo,
	reg prometheus.Registerer,
) (*DomainCollector, error) {
	c := &DomainCollector{
		client:          client,
		store:           st,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(domainCollectorName),
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		accountIDs:      accountIDs,
		zones:           zones,
		gauges:          make(map[string]domainGauges),
		descExpiration: prometheus.NewDesc(
			metricDomainExpirationTimestamp,
			"Absolute domain expiry Unix timestamp. Alert on (metric - time()) < threshold.",
			[]string{"account_id", "domain"},
			nil,
		),
		descAutoRenew: prometheus.NewDesc(
			metricDomainAutoRenew,
			"Whether auto-renew is enabled for the domain (1 = enabled, 0 = disabled).",
			[]string{"account_id", "domain"},
			nil,
		),
		descLocked: prometheus.NewDesc(
			metricDomainLocked,
			"Registrar transfer lock.",
			[]string{"account_id", "domain"},
			nil,
		),
	}

	if err := reg.Register(&domainPromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering domain collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

func (c *DomainCollector) Name() string                    { return domainCollectorName }
func (c *DomainCollector) Priority() governor.PriorityClass { return PriorityBackground }
func (c *DomainCollector) Interval() time.Duration          { return time.Duration(c.refreshInterval) * time.Second }
func (c *DomainCollector) RequiredDatasets() []string        { return []string{"domains"} }
func (c *DomainCollector) Scope() Scope                     { return ScopeAccount }

// Describe returns a human-readable description.
func (c *DomainCollector) Describe() string {
	return "Collects domain lifecycle metrics (expiration, auto-renew, lock status) from the Registrar REST API."
}

// Collect fetches domain information from the Cloudflare Registrar REST API.
func (c *DomainCollector) Collect(ctx context.Context) error {
	start := time.Now()

	newGauges := make(map[string]domainGauges)
	var lastErr error

	for _, accountID := range c.accountIDs {
		domains, err := c.fetchDomains(ctx, accountID)
		if err != nil {
			c.logger.Error("failed to fetch domains",
				zap.String("account_id", accountID),
				zap.Error(err),
			)
			lastErr = err
			continue
		}

		for _, d := range domains {
			key := accountID + "\x00" + d.DomainName
			g := domainGauges{
				accountID:  accountID,
				domain:     d.DomainName,
				expiration: float64(d.ExpiresAt.Unix()),
			}
			if d.AutoRenew {
				g.autoRenew = 1
			}
			if d.Locked {
				g.locked = 1
			}
			newGauges[key] = g
		}
	}

	// Atomically replace gauge snapshot.
	c.gauges = newGauges

	duration := time.Since(start)
	if lastErr != nil {
		c.selfMetrics.RecordCollectionError(domainCollectorName, "rest_api", duration)
		return lastErr
	}
	c.selfMetrics.RecordCollectionSuccess(domainCollectorName, duration, time.Now())
	return nil
}

func (c *DomainCollector) fetchDomains(ctx context.Context, accountID string) ([]registrarDomain, error) {
	path := fmt.Sprintf("/accounts/%s/registrar/domains", accountID)

	raw, _, err := c.client.RESTGet(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("fetching domains for account %s: %w", accountID, err)
	}

	restResp, err := cloudflare.ParseRESTResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing domains response for account %s: %w", accountID, err)
	}

	var domains []registrarDomain
	if err := json.Unmarshal(restResp.Result, &domains); err != nil {
		return nil, fmt.Errorf("unmarshaling domains for account %s: %w", accountID, err)
	}

	return domains, nil
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

// domainPromCollector adapts DomainCollector to the prometheus.Collector
// interface. This wrapper is necessary because collector.Collector and
// prometheus.Collector both define Describe and Collect methods with
// incompatible signatures.
type domainPromCollector struct {
	inner *DomainCollector
}

// Describe implements prometheus.Collector.
func (p *domainPromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.descExpiration
	ch <- p.inner.descAutoRenew
	ch <- p.inner.descLocked
}

// Collect implements prometheus.Collector. It reads the latest gauge snapshot
// and emits them as constant metrics.
func (p *domainPromCollector) Collect(ch chan<- prometheus.Metric) {
	for _, g := range p.inner.gauges {
		ch <- prometheus.MustNewConstMetric(
			p.inner.descExpiration,
			prometheus.GaugeValue,
			g.expiration,
			g.accountID, g.domain,
		)
		ch <- prometheus.MustNewConstMetric(
			p.inner.descAutoRenew,
			prometheus.GaugeValue,
			g.autoRenew,
			g.accountID, g.domain,
		)
		ch <- prometheus.MustNewConstMetric(
			p.inner.descLocked,
			prometheus.GaugeValue,
			g.locked,
			g.accountID, g.domain,
		)
	}
}
