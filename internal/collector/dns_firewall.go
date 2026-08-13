package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/cloudflare"
	"github.com/phaseshiftdata/prometheus_exporters/internal/governor"
	"github.com/phaseshiftdata/prometheus_exporters/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	dnsFirewallCollectorName = "dns_firewall"
	dnsFirewallDataset       = "dnsFirewallAnalyticsAdaptiveGroups"

	metricDNSFirewallQueriesTotal = "cloudflare_dns_firewall_queries_total"
)

// dnsFirewallGraphQLResponse represents the expected shape of the GraphQL
// response for DNS Firewall analytics.
type dnsFirewallGraphQLResponse struct {
	Viewer struct {
		Accounts []struct {
			DNSFirewallAnalyticsAdaptiveGroups []struct {
				Count      float64 `json:"count"`
				Dimensions struct {
					ClusterID      string `json:"clusterID"`
					ResponseCode   string `json:"responseCode"`
					CacheStatus    string `json:"cacheStatus"`
					DatetimeMinute string `json:"datetimeMinute"`
				} `json:"dimensions"`
			} `json:"dnsFirewallAnalyticsAdaptiveGroups"`
		} `json:"accounts"`
	} `json:"viewer"`
}

// DNSFirewallCollector collects DNS Firewall analytics from the Cloudflare
// GraphQL Analytics API.
type DNSFirewallCollector struct {
	client      *cloudflare.Client
	store       *store.Store
	selfMetrics *SelfMetrics
	logger      *zap.Logger

	scrapeDelay     int
	timeWindow      int
	refreshInterval int

	accountIDs []string
	zones      []ZoneInfo

	descQueries *prometheus.Desc
}

// NewDNSFirewallCollector creates a new DNSFirewallCollector and registers its
// prometheus descriptors with the provided registry.
func NewDNSFirewallCollector(
	client *cloudflare.Client,
	st *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay, timeWindow, refreshInterval int,
	accountIDs []string,
	zones []ZoneInfo,
	reg prometheus.Registerer,
) (*DNSFirewallCollector, error) {
	c := &DNSFirewallCollector{
		client:          client,
		store:           st,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(dnsFirewallCollectorName),
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		accountIDs:      accountIDs,
		zones:           zones,
		descQueries: prometheus.NewDesc(
			metricDNSFirewallQueriesTotal,
			"Total number of DNS Firewall queries.",
			[]string{"account_id", "cluster_id", "response_code", "cache_status"},
			nil,
		),
	}

	if err := reg.Register(&dnsFirewallPromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering dns_firewall collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

func (c *DNSFirewallCollector) Name() string                    { return dnsFirewallCollectorName }
func (c *DNSFirewallCollector) Priority() governor.PriorityClass { return PriorityStandard }
func (c *DNSFirewallCollector) Interval() time.Duration          { return time.Duration(c.refreshInterval) * time.Second }
func (c *DNSFirewallCollector) RequiredDatasets() []string        { return []string{dnsFirewallDataset} }
func (c *DNSFirewallCollector) Scope() Scope                     { return ScopeAccount }

// Describe returns a human-readable description.
func (c *DNSFirewallCollector) Describe() string {
	return "Collects DNS Firewall analytics from dnsFirewallAnalyticsAdaptiveGroups."
}

// Collect queries the Cloudflare GraphQL API for DNS Firewall analytics and
// folds the results into the aggregation store.
func (c *DNSFirewallCollector) Collect(ctx context.Context) error {
	start := time.Now()
	window := CalculateWindow(time.Now(), c.scrapeDelay, c.timeWindow)

	var lastErr error
	for _, accountID := range c.accountIDs {
		if err := c.collectAccount(ctx, accountID, window); err != nil {
			c.logger.Error("failed to collect DNS Firewall analytics",
				zap.String("account_id", accountID),
				zap.Error(err),
			)
			lastErr = err
		}
	}

	duration := time.Since(start)
	if lastErr != nil {
		c.selfMetrics.RecordCollectionError(dnsFirewallCollectorName, "graphql_query", duration)
		return lastErr
	}
	c.selfMetrics.RecordCollectionSuccess(dnsFirewallCollectorName, duration, window.End)
	return nil
}

func (c *DNSFirewallCollector) collectAccount(ctx context.Context, accountID string, window TimeWindow) error {
	query := `query ($accountID: String!, $start: Time!, $end: Time!) {
  viewer {
    accounts(filter: {accountTag: $accountID}) {
      dnsFirewallAnalyticsAdaptiveGroups(
        filter: {datetime_geq: $start, datetime_lt: $end}
        limit: 10000
      ) {
        count
        dimensions {
          clusterID
          responseCode
          cacheStatus
          datetimeMinute
        }
      }
    }
  }
}`

	variables := map[string]any{
		"accountID": accountID,
		"start":     window.Start.Format(time.RFC3339),
		"end":       window.End.Format(time.RFC3339),
	}

	resp, _, err := c.client.QueryGraphQL(ctx, query, variables)
	if err != nil {
		return fmt.Errorf("querying DNS Firewall analytics for account %s: %w", accountID, err)
	}

	if len(resp.Errors) > 0 {
		msgs := make([]string, len(resp.Errors))
		for i, e := range resp.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("graphql errors for account %s: %s", accountID, strings.Join(msgs, "; "))
	}

	var data dnsFirewallGraphQLResponse
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return fmt.Errorf("unmarshaling DNS Firewall analytics for account %s: %w", accountID, err)
	}

	for _, account := range data.Viewer.Accounts {
		for _, group := range account.DNSFirewallAnalyticsAdaptiveGroups {
			timeBucket, _ := time.Parse(time.RFC3339, group.Dimensions.DatetimeMinute)

			dims := store.MakeDimensionKey(
				"account_id", accountID,
				"cluster_id", group.Dimensions.ClusterID,
				"response_code", group.Dimensions.ResponseCode,
				"cache_status", group.Dimensions.CacheStatus,
			)
			c.store.Add(metricDNSFirewallQueriesTotal, dims, timeBucket, group.Count)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

// dnsFirewallPromCollector adapts DNSFirewallCollector to the prometheus.Collector
// interface. This wrapper is necessary because collector.Collector and
// prometheus.Collector both define Describe and Collect methods with
// incompatible signatures.
type dnsFirewallPromCollector struct {
	inner *DNSFirewallCollector
}

// Describe implements prometheus.Collector.
func (p *dnsFirewallPromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.descQueries
}

// Collect implements prometheus.Collector. It reads accumulated values from the
// store and emits them as constant metrics.
func (p *dnsFirewallPromCollector) Collect(ch chan<- prometheus.Metric) {
	for dims, value := range p.inner.store.GetAll(metricDNSFirewallQueriesTotal) {
		labels := splitDimensionKey(dims)
		if len(labels) < 8 {
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			p.inner.descQueries,
			prometheus.CounterValue,
			value,
			labels[1], labels[3], labels[5], labels[7], // account_id, cluster_id, response_code, cache_status
		)
	}
}
