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
	gatewayDNSCollectorName = "gateway_dns"
	gatewayDNSMetricTotal   = "cloudflare_gateway_dns_queries_total"
	gatewayDNSDataset       = "gatewayResolverByCategoryAdaptiveGroups"
	gatewayDNSLabelCount    = 4 // account_id, resolver_decision, category, location_id
)

// gatewayDNSGraphQLResponse is the typed response for the gateway DNS query.
type gatewayDNSGraphQLResponse struct {
	Viewer struct {
		Accounts []struct {
			Dataset []struct {
				Count      int `json:"count"`
				Dimensions struct {
					DatetimeMinute   string `json:"datetimeMinute"`
					ResolverDecision string `json:"resolverDecision"`
					Category         string `json:"category"`
					LocationID       string `json:"locationID"`
				} `json:"dimensions"`
			} `json:"gatewayResolverByCategoryAdaptiveGroups"`
		} `json:"accounts"`
	} `json:"viewer"`
}

// GatewayDNSCollector collects Cloudflare Gateway DNS query metrics.
type GatewayDNSCollector struct {
	client          *cloudflare.Client
	store           *store.Store
	selfMetrics     *SelfMetrics
	logger          *zap.Logger
	accountIDs      []string
	scrapeDelay     int
	timeWindow      int
	refreshInterval int

	queriesDesc *prometheus.Desc
}

// NewGatewayDNSCollector creates a new GatewayDNSCollector and registers its
// prometheus descriptors with the provided registry.
func NewGatewayDNSCollector(
	client *cloudflare.Client,
	s *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay int,
	timeWindow int,
	refreshInterval int,
	accountIDs []string,
	reg prometheus.Registerer,
) (*GatewayDNSCollector, error) {
	c := &GatewayDNSCollector{
		client:          client,
		store:           s,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(gatewayDNSCollectorName),
		accountIDs:      accountIDs,
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		queriesDesc: prometheus.NewDesc(
			gatewayDNSMetricTotal,
			"Total Gateway DNS queries. Sampled estimate; see cloudflare_exporter_data_age_seconds for freshness. Values are operational signals, not billing records.",
			[]string{"account_id", "resolver_decision", "category", "location_id"},
			nil,
		),
	}

	if err := reg.Register(&gatewayDNSPromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering gateway DNS collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

// Name returns the collector name.
func (c *GatewayDNSCollector) Name() string { return gatewayDNSCollectorName }

// Priority returns the priority class.
func (c *GatewayDNSCollector) Priority() governor.PriorityClass { return governor.PriorityCritical }

// Interval returns the desired collection interval.
func (c *GatewayDNSCollector) Interval() time.Duration {
	return time.Duration(c.refreshInterval) * time.Second
}

// RequiredDatasets returns the dataset names this collector needs.
func (c *GatewayDNSCollector) RequiredDatasets() []string {
	return []string{gatewayDNSDataset}
}

// Scope returns the scope of this collector.
func (c *GatewayDNSCollector) Scope() Scope { return ScopeAccount }

// Describe returns a human-readable description of the collector.
func (c *GatewayDNSCollector) Describe() string {
	return "Collects Cloudflare Gateway DNS query metrics by resolver decision, category, and location."
}

// Collect executes a collection run against the Cloudflare GraphQL API.
func (c *GatewayDNSCollector) Collect(ctx context.Context) error {
	start := time.Now()
	window := CalculateWindow(time.Now(), c.scrapeDelay, c.timeWindow)

	var lastErr error
	var maxDataTime time.Time

	for _, accountID := range c.accountIDs {
		dataTime, err := c.collectAccount(ctx, accountID, window)
		if err != nil {
			c.logger.Error("failed to collect gateway DNS metrics",
				zap.String("account_id", accountID),
				zap.Error(err),
			)
			lastErr = err
			continue
		}
		if dataTime.After(maxDataTime) {
			maxDataTime = dataTime
		}
	}

	duration := time.Since(start)
	if lastErr != nil {
		c.selfMetrics.RecordCollectionError(gatewayDNSCollectorName, "query_failed", duration)
		return lastErr
	}

	if maxDataTime.IsZero() {
		maxDataTime = window.End
	}
	c.selfMetrics.RecordCollectionSuccess(gatewayDNSCollectorName, duration, maxDataTime)
	return nil
}

func (c *GatewayDNSCollector) collectAccount(ctx context.Context, accountID string, window TimeWindow) (time.Time, error) {
	query := `query GatewayDNSQueries($accountTag: string!, $start: Time!, $end: Time!) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      gatewayResolverByCategoryAdaptiveGroups(
        filter: {datetimeMinute_geq: $start, datetimeMinute_lt: $end}
        limit: 10000
      ) {
        count
        dimensions {
          datetimeMinute
          resolverDecision
          category
          locationID
        }
      }
    }
  }
}`

	variables := map[string]any{
		"accountTag": accountID,
		"start":      window.Start.Format(time.RFC3339),
		"end":        window.End.Format(time.RFC3339),
	}

	resp, _, err := c.client.QueryGraphQL(ctx, query, variables)
	if err != nil {
		return time.Time{}, fmt.Errorf("querying gateway DNS: %w", err)
	}

	if len(resp.Errors) > 0 {
		return time.Time{}, fmt.Errorf("graphql errors: %s", resp.Errors[0].Message)
	}

	var data gatewayDNSGraphQLResponse
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return time.Time{}, fmt.Errorf("unmarshaling gateway DNS response: %w", err)
	}

	var maxTime time.Time
	for _, account := range data.Viewer.Accounts {
		for _, row := range account.Dataset {
			t, err := time.Parse(time.RFC3339, row.Dimensions.DatetimeMinute)
			if err != nil {
				c.logger.Warn("skipping row with unparseable timestamp",
					zap.String("timestamp", row.Dimensions.DatetimeMinute),
					zap.Error(err),
				)
				continue
			}
			if t.After(maxTime) {
				maxTime = t
			}

			dims := store.MakeDimensionKey(
				"account_id", accountID,
				"resolver_decision", row.Dimensions.ResolverDecision,
				"category", row.Dimensions.Category,
				"location_id", row.Dimensions.LocationID,
			)

			c.store.Add(gatewayDNSMetricTotal, dims, t, float64(row.Count))
		}
	}

	return maxTime, nil
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

type gatewayDNSPromCollector struct {
	inner *GatewayDNSCollector
}

// Describe implements prometheus.Collector.
func (p *gatewayDNSPromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.queriesDesc
}

// Collect implements prometheus.Collector.
func (p *gatewayDNSPromCollector) Collect(ch chan<- prometheus.Metric) {
	all := p.inner.store.GetAll(gatewayDNSMetricTotal)
	for dims, value := range all {
		labels := parseDimensionKey(dims, gatewayDNSLabelCount)
		if labels == nil {
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			p.inner.queriesDesc,
			prometheus.CounterValue,
			value,
			dimValues(labels)...,
		)
	}
}
