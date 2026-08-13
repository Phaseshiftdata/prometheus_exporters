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
	dnsCollectorName = "dns"
	dnsDataset       = "dnsAnalyticsAdaptiveGroups"

	metricDNSQueriesTotal        = "cloudflare_dns_queries_total"
	metricDNSQueryDurationSeconds = "cloudflare_dns_query_duration_seconds"
)

// dnsGraphQLResponse represents the expected shape of the GraphQL response for
// DNS analytics.
type dnsGraphQLResponse struct {
	Viewer struct {
		Zones []struct {
			DNSAnalyticsAdaptiveGroups []struct {
				Count      float64 `json:"count"`
				Dimensions struct {
					QueryType      string `json:"queryType"`
					ResponseCode   string `json:"responseCode"`
					DatetimeMinute string `json:"datetimeMinute"`
				} `json:"dimensions"`
				Avg struct {
					QueryTimeMicroseconds float64 `json:"queryTimeMicroseconds"`
				} `json:"avg"`
			} `json:"dnsAnalyticsAdaptiveGroups"`
		} `json:"zones"`
	} `json:"viewer"`
}

// ZoneInfo holds a zone's ID and human-readable name.
type ZoneInfo struct {
	ID   string
	Name string
}

// DNSCollector collects authoritative DNS analytics from the Cloudflare
// GraphQL Analytics API.
type DNSCollector struct {
	client      *cloudflare.Client
	store       *store.Store
	selfMetrics *SelfMetrics
	logger      *zap.Logger

	scrapeDelay     int
	timeWindow      int
	refreshInterval int

	accountIDs []string
	zones      []ZoneInfo

	descQueries  *prometheus.Desc
	descDuration *prometheus.Desc
}

// NewDNSCollector creates a new DNSCollector and registers its prometheus
// descriptors with the provided registry.
func NewDNSCollector(
	client *cloudflare.Client,
	st *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay, timeWindow, refreshInterval int,
	accountIDs []string,
	zones []ZoneInfo,
	reg prometheus.Registerer,
) (*DNSCollector, error) {
	c := &DNSCollector{
		client:          client,
		store:           st,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(dnsCollectorName),
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		accountIDs:      accountIDs,
		zones:           zones,
		descQueries: prometheus.NewDesc(
			metricDNSQueriesTotal,
			"Total number of authoritative DNS queries.",
			[]string{"zone_id", "zone_name", "query_type", "response_code"},
			nil,
		),
		descDuration: prometheus.NewDesc(
			metricDNSQueryDurationSeconds,
			"Average DNS query duration in seconds.",
			[]string{"zone_id", "zone_name"},
			nil,
		),
	}

	if err := reg.Register(&dnsPromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering dns collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

func (c *DNSCollector) Name() string                    { return dnsCollectorName }
func (c *DNSCollector) Priority() governor.PriorityClass { return PriorityStandard }
func (c *DNSCollector) Interval() time.Duration          { return time.Duration(c.refreshInterval) * time.Second }
func (c *DNSCollector) RequiredDatasets() []string        { return []string{dnsDataset} }
func (c *DNSCollector) Scope() Scope                     { return ScopeZone }

// Describe returns a human-readable description of what this collector does.
// This satisfies the collector.Collector interface (returns string).
func (c *DNSCollector) Describe() string {
	return "Collects authoritative DNS analytics from dnsAnalyticsAdaptiveGroups."
}

// Collect queries the Cloudflare GraphQL API for DNS analytics and folds the
// results into the aggregation store.
func (c *DNSCollector) Collect(ctx context.Context) error {
	start := time.Now()
	window := CalculateWindow(time.Now(), c.scrapeDelay, c.timeWindow)

	var lastErr error
	for _, z := range c.zones {
		if err := c.collectZone(ctx, z, window); err != nil {
			c.logger.Error("failed to collect DNS analytics",
				zap.String("zone_id", z.ID),
				zap.String("zone_name", z.Name),
				zap.Error(err),
			)
			lastErr = err
		}
	}

	duration := time.Since(start)
	if lastErr != nil {
		c.selfMetrics.RecordCollectionError(dnsCollectorName, "graphql_query", duration)
		return lastErr
	}
	c.selfMetrics.RecordCollectionSuccess(dnsCollectorName, duration, window.End)
	return nil
}

func (c *DNSCollector) collectZone(ctx context.Context, z ZoneInfo, window TimeWindow) error {
	query := `query ($zoneID: String!, $start: Time!, $end: Time!) {
  viewer {
    zones(filter: {zoneTag: $zoneID}) {
      dnsAnalyticsAdaptiveGroups(
        filter: {datetime_geq: $start, datetime_lt: $end}
        limit: 10000
      ) {
        count
        dimensions {
          queryType
          responseCode
          datetimeMinute
        }
        avg {
          queryTimeMicroseconds
        }
      }
    }
  }
}`

	variables := map[string]any{
		"zoneID": z.ID,
		"start":  window.Start.Format(time.RFC3339),
		"end":    window.End.Format(time.RFC3339),
	}

	resp, _, err := c.client.QueryGraphQL(ctx, query, variables)
	if err != nil {
		return fmt.Errorf("querying DNS analytics for zone %s: %w", z.ID, err)
	}

	if len(resp.Errors) > 0 {
		msgs := make([]string, len(resp.Errors))
		for i, e := range resp.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("graphql errors for zone %s: %s", z.ID, strings.Join(msgs, "; "))
	}

	var data dnsGraphQLResponse
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return fmt.Errorf("unmarshaling DNS analytics for zone %s: %w", z.ID, err)
	}

	for _, zone := range data.Viewer.Zones {
		for _, group := range zone.DNSAnalyticsAdaptiveGroups {
			timeBucket, _ := time.Parse(time.RFC3339, group.Dimensions.DatetimeMinute)

			dims := store.MakeDimensionKey(
				"zone_id", z.ID,
				"zone_name", z.Name,
				"query_type", group.Dimensions.QueryType,
				"response_code", group.Dimensions.ResponseCode,
			)
			c.store.Add(metricDNSQueriesTotal, dims, timeBucket, group.Count)

			// Store average query duration. Use the latest value seen.
			durationDims := store.MakeDimensionKey(
				"zone_id", z.ID,
				"zone_name", z.Name,
			)
			durationSeconds := group.Avg.QueryTimeMicroseconds / 1e6
			c.store.Add(metricDNSQueryDurationSeconds, durationDims, timeBucket, durationSeconds)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

// dnsPromCollector adapts DNSCollector to the prometheus.Collector interface.
// This wrapper is necessary because collector.Collector and prometheus.Collector
// both define Describe and Collect methods with incompatible signatures.
type dnsPromCollector struct {
	inner *DNSCollector
}

// Describe implements prometheus.Collector.
func (p *dnsPromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.descQueries
	ch <- p.inner.descDuration
}

// Collect implements prometheus.Collector. It reads accumulated values from the
// store and emits them as constant metrics.
func (p *dnsPromCollector) Collect(ch chan<- prometheus.Metric) {
	for dims, value := range p.inner.store.GetAll(metricDNSQueriesTotal) {
		labels := splitDimensionKey(dims)
		if len(labels) < 8 {
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			p.inner.descQueries,
			prometheus.CounterValue,
			value,
			labels[1], labels[3], labels[5], labels[7], // zone_id, zone_name, query_type, response_code
		)
	}

	for dims, value := range p.inner.store.GetAll(metricDNSQueryDurationSeconds) {
		labels := splitDimensionKey(dims)
		if len(labels) < 4 {
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			p.inner.descDuration,
			prometheus.GaugeValue,
			value,
			labels[1], labels[3], // zone_id, zone_name
		)
	}
}

// splitDimensionKey splits a null-byte separated DimensionKey into its
// constituent parts.
func splitDimensionKey(dk store.DimensionKey) []string {
	return strings.Split(string(dk), "\x00")
}
