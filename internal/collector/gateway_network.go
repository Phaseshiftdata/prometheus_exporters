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
	gatewayNetworkCollectorName = "gateway_network"
	gatewayNetworkMetricSessions = "cloudflare_gateway_network_sessions_total"
	gatewayNetworkMetricBytes    = "cloudflare_gateway_network_bytes_total"

	gatewayNetworkDatasetSessions   = "gatewayL4SessionsAdaptiveGroups"
	gatewayNetworkDatasetDownstream = "gatewayL4DownstreamSessionsAdaptiveGroups"
	gatewayNetworkDatasetUpstream   = "gatewayL4UpstreamSessionsAdaptiveGroups"

	gatewayNetworkSessionLabelCount = 3 // account_id, action, protocol
	gatewayNetworkBytesLabelCount   = 2 // account_id, direction
)

// gatewayNetworkSessionsResponse is the typed response for gateway L4 session queries.
type gatewayNetworkSessionsResponse struct {
	Viewer struct {
		Accounts []struct {
			Sessions []struct {
				Count      int `json:"count"`
				Dimensions struct {
					DatetimeMinute string `json:"datetimeMinute"`
					Action         string `json:"action"`
					Protocol       string `json:"protocol"`
				} `json:"dimensions"`
			} `json:"gatewayL4SessionsAdaptiveGroups"`
		} `json:"accounts"`
	} `json:"viewer"`
}

// gatewayNetworkBytesResponse is the typed response for gateway L4 byte count queries.
type gatewayNetworkBytesResponse struct {
	Viewer struct {
		Accounts []struct {
			Downstream []struct {
				Sum struct {
					BytesReceived int64 `json:"bytesReceived"`
				} `json:"sum"`
				Dimensions struct {
					DatetimeMinute string `json:"datetimeMinute"`
				} `json:"dimensions"`
			} `json:"gatewayL4DownstreamSessionsAdaptiveGroups"`
			Upstream []struct {
				Sum struct {
					BytesSent int64 `json:"bytesSent"`
				} `json:"sum"`
				Dimensions struct {
					DatetimeMinute string `json:"datetimeMinute"`
				} `json:"dimensions"`
			} `json:"gatewayL4UpstreamSessionsAdaptiveGroups"`
		} `json:"accounts"`
	} `json:"viewer"`
}

// GatewayNetworkCollector collects Cloudflare Gateway network session and
// byte transfer metrics.
type GatewayNetworkCollector struct {
	client          *cloudflare.Client
	store           *store.Store
	selfMetrics     *SelfMetrics
	logger          *zap.Logger
	accountIDs      []string
	scrapeDelay     int
	timeWindow      int
	refreshInterval int

	sessionsDesc *prometheus.Desc
	bytesDesc    *prometheus.Desc
}

// NewGatewayNetworkCollector creates a new GatewayNetworkCollector and registers
// its prometheus descriptors with the provided registry.
func NewGatewayNetworkCollector(
	client *cloudflare.Client,
	s *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay int,
	timeWindow int,
	refreshInterval int,
	accountIDs []string,
	reg prometheus.Registerer,
) (*GatewayNetworkCollector, error) {
	c := &GatewayNetworkCollector{
		client:          client,
		store:           s,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(gatewayNetworkCollectorName),
		accountIDs:      accountIDs,
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		sessionsDesc: prometheus.NewDesc(
			gatewayNetworkMetricSessions,
			"Total Gateway network sessions. Sampled estimate; see cloudflare_exporter_data_age_seconds for freshness. Values are operational signals, not billing records.",
			[]string{"account_id", "action", "protocol"},
			nil,
		),
		bytesDesc: prometheus.NewDesc(
			gatewayNetworkMetricBytes,
			"Total Gateway network bytes transferred. Sampled estimate; see cloudflare_exporter_data_age_seconds for freshness. Values are operational signals, not billing records.",
			[]string{"account_id", "direction"},
			nil,
		),
	}

	if err := reg.Register(&gatewayNetworkPromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering gateway network collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

// Name returns the collector name.
func (c *GatewayNetworkCollector) Name() string { return gatewayNetworkCollectorName }

// Priority returns the priority class.
func (c *GatewayNetworkCollector) Priority() governor.PriorityClass {
	return governor.PriorityCritical
}

// Interval returns the desired collection interval.
func (c *GatewayNetworkCollector) Interval() time.Duration {
	return time.Duration(c.refreshInterval) * time.Second
}

// RequiredDatasets returns the dataset names this collector needs.
func (c *GatewayNetworkCollector) RequiredDatasets() []string {
	return []string{
		gatewayNetworkDatasetSessions,
		gatewayNetworkDatasetDownstream,
		gatewayNetworkDatasetUpstream,
	}
}

// Scope returns the scope of this collector.
func (c *GatewayNetworkCollector) Scope() Scope { return ScopeAccount }

// Describe returns a human-readable description of the collector.
func (c *GatewayNetworkCollector) Describe() string {
	return "Collects Cloudflare Gateway L4 network session counts and byte transfer totals."
}

// Collect executes a collection run against the Cloudflare GraphQL API.
func (c *GatewayNetworkCollector) Collect(ctx context.Context) error {
	start := time.Now()
	window := CalculateWindow(time.Now(), c.scrapeDelay, c.timeWindow)

	var lastErr error
	var maxDataTime time.Time

	for _, accountID := range c.accountIDs {
		dataTime, err := c.collectAccount(ctx, accountID, window)
		if err != nil {
			c.logger.Error("failed to collect gateway network metrics",
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
		c.selfMetrics.RecordCollectionError(gatewayNetworkCollectorName, "query_failed", duration)
		return lastErr
	}

	if maxDataTime.IsZero() {
		maxDataTime = window.End
	}
	c.selfMetrics.RecordCollectionSuccess(gatewayNetworkCollectorName, duration, maxDataTime)
	return nil
}

func (c *GatewayNetworkCollector) collectAccount(ctx context.Context, accountID string, window TimeWindow) (time.Time, error) {
	sessionTime, err := c.collectSessions(ctx, accountID, window)
	if err != nil {
		return time.Time{}, err
	}

	bytesTime, err := c.collectBytes(ctx, accountID, window)
	if err != nil {
		return sessionTime, err
	}

	if bytesTime.After(sessionTime) {
		return bytesTime, nil
	}
	return sessionTime, nil
}

func (c *GatewayNetworkCollector) collectSessions(ctx context.Context, accountID string, window TimeWindow) (time.Time, error) {
	query := `query GatewayNetworkSessions($accountTag: string!, $start: Time!, $end: Time!) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      gatewayL4SessionsAdaptiveGroups(
        filter: {datetimeMinute_geq: $start, datetimeMinute_lt: $end}
        limit: 10000
      ) {
        count
        dimensions {
          datetimeMinute
          action
          protocol
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
		return time.Time{}, fmt.Errorf("querying gateway network sessions: %w", err)
	}

	if len(resp.Errors) > 0 {
		return time.Time{}, fmt.Errorf("graphql errors: %s", resp.Errors[0].Message)
	}

	var data gatewayNetworkSessionsResponse
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return time.Time{}, fmt.Errorf("unmarshaling gateway network sessions: %w", err)
	}

	var maxTime time.Time
	for _, account := range data.Viewer.Accounts {
		for _, row := range account.Sessions {
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
				"action", row.Dimensions.Action,
				"protocol", row.Dimensions.Protocol,
			)

			c.store.Add(gatewayNetworkMetricSessions, dims, t, float64(row.Count))
		}
	}

	return maxTime, nil
}

func (c *GatewayNetworkCollector) collectBytes(ctx context.Context, accountID string, window TimeWindow) (time.Time, error) {
	query := `query GatewayNetworkBytes($accountTag: string!, $start: Time!, $end: Time!) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      gatewayL4DownstreamSessionsAdaptiveGroups(
        filter: {datetimeMinute_geq: $start, datetimeMinute_lt: $end}
        limit: 10000
      ) {
        sum {
          bytesReceived
        }
        dimensions {
          datetimeMinute
        }
      }
      gatewayL4UpstreamSessionsAdaptiveGroups(
        filter: {datetimeMinute_geq: $start, datetimeMinute_lt: $end}
        limit: 10000
      ) {
        sum {
          bytesSent
        }
        dimensions {
          datetimeMinute
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
		return time.Time{}, fmt.Errorf("querying gateway network bytes: %w", err)
	}

	if len(resp.Errors) > 0 {
		return time.Time{}, fmt.Errorf("graphql errors: %s", resp.Errors[0].Message)
	}

	var data gatewayNetworkBytesResponse
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return time.Time{}, fmt.Errorf("unmarshaling gateway network bytes: %w", err)
	}

	var maxTime time.Time
	for _, account := range data.Viewer.Accounts {
		for _, row := range account.Downstream {
			t, err := time.Parse(time.RFC3339, row.Dimensions.DatetimeMinute)
			if err != nil {
				c.logger.Warn("skipping downstream row with unparseable timestamp",
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
				"direction", "downstream",
			)

			c.store.Add(gatewayNetworkMetricBytes, dims, t, float64(row.Sum.BytesReceived))
		}

		for _, row := range account.Upstream {
			t, err := time.Parse(time.RFC3339, row.Dimensions.DatetimeMinute)
			if err != nil {
				c.logger.Warn("skipping upstream row with unparseable timestamp",
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
				"direction", "upstream",
			)

			c.store.Add(gatewayNetworkMetricBytes, dims, t, float64(row.Sum.BytesSent))
		}
	}

	return maxTime, nil
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

type gatewayNetworkPromCollector struct {
	inner *GatewayNetworkCollector
}

// Describe implements prometheus.Collector.
func (p *gatewayNetworkPromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.sessionsDesc
	ch <- p.inner.bytesDesc
}

// Collect implements prometheus.Collector.
func (p *gatewayNetworkPromCollector) Collect(ch chan<- prometheus.Metric) {
	// Emit session counters.
	sessions := p.inner.store.GetAll(gatewayNetworkMetricSessions)
	for dims, value := range sessions {
		labels := parseDimensionKey(dims, gatewayNetworkSessionLabelCount)
		if labels == nil {
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			p.inner.sessionsDesc,
			prometheus.CounterValue,
			value,
			dimValues(labels)...,
		)
	}

	// Emit byte counters.
	bytes := p.inner.store.GetAll(gatewayNetworkMetricBytes)
	for dims, value := range bytes {
		labels := parseDimensionKey(dims, gatewayNetworkBytesLabelCount)
		if labels == nil {
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			p.inner.bytesDesc,
			prometheus.CounterValue,
			value,
			dimValues(labels)...,
		)
	}
}
