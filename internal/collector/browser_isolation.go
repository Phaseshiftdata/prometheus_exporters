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
	browserIsolationCollectorName = "browser_isolation"
	browserIsolationMetricTotal   = "cloudflare_browser_isolation_sessions_total"
	browserIsolationDataset       = "browserIsolationSessionsAdaptiveGroups"
	browserIsolationLabelCount    = 2 // account_id, outcome
)

// browserIsolationGraphQLResponse is the typed response for browser isolation queries.
type browserIsolationGraphQLResponse struct {
	Viewer struct {
		Accounts []struct {
			Dataset []struct {
				Count      int `json:"count"`
				Dimensions struct {
					DatetimeMinute string `json:"datetimeMinute"`
					Outcome        string `json:"outcome"`
				} `json:"dimensions"`
			} `json:"browserIsolationSessionsAdaptiveGroups"`
		} `json:"accounts"`
	} `json:"viewer"`
}

// BrowserIsolationCollector collects Cloudflare Browser Isolation session metrics.
type BrowserIsolationCollector struct {
	client          *cloudflare.Client
	store           *store.Store
	selfMetrics     *SelfMetrics
	logger          *zap.Logger
	accountIDs      []string
	scrapeDelay     int
	timeWindow      int
	refreshInterval int

	sessionsDesc *prometheus.Desc
}

// NewBrowserIsolationCollector creates a new BrowserIsolationCollector and
// registers its prometheus descriptors with the provided registry.
func NewBrowserIsolationCollector(
	client *cloudflare.Client,
	s *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay int,
	timeWindow int,
	refreshInterval int,
	accountIDs []string,
	reg prometheus.Registerer,
) (*BrowserIsolationCollector, error) {
	c := &BrowserIsolationCollector{
		client:          client,
		store:           s,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(browserIsolationCollectorName),
		accountIDs:      accountIDs,
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		sessionsDesc: prometheus.NewDesc(
			browserIsolationMetricTotal,
			"Total Browser Isolation sessions by outcome.",
			[]string{"account_id", "outcome"},
			nil,
		),
	}

	if err := reg.Register(&browserIsolationPromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering browser isolation collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

// Name returns the collector name.
func (c *BrowserIsolationCollector) Name() string { return browserIsolationCollectorName }

// Priority returns the priority class.
func (c *BrowserIsolationCollector) Priority() governor.PriorityClass {
	return governor.PriorityStandard
}

// Interval returns the desired collection interval.
func (c *BrowserIsolationCollector) Interval() time.Duration {
	return time.Duration(c.refreshInterval) * time.Second
}

// RequiredDatasets returns the dataset names this collector needs.
func (c *BrowserIsolationCollector) RequiredDatasets() []string {
	return []string{browserIsolationDataset}
}

// Scope returns the scope of this collector.
func (c *BrowserIsolationCollector) Scope() Scope { return ScopeAccount }

// Describe returns a human-readable description of the collector.
func (c *BrowserIsolationCollector) Describe() string {
	return "Collects Cloudflare Browser Isolation session metrics by outcome."
}

// Collect executes a collection run against the Cloudflare GraphQL API.
func (c *BrowserIsolationCollector) Collect(ctx context.Context) error {
	start := time.Now()
	window := CalculateWindow(time.Now(), c.scrapeDelay, c.timeWindow)

	var lastErr error
	var maxDataTime time.Time

	for _, accountID := range c.accountIDs {
		dataTime, err := c.collectAccount(ctx, accountID, window)
		if err != nil {
			c.logger.Error("failed to collect browser isolation metrics",
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
		c.selfMetrics.RecordCollectionError(browserIsolationCollectorName, "query_failed", duration)
		return lastErr
	}

	if maxDataTime.IsZero() {
		maxDataTime = window.End
	}
	c.selfMetrics.RecordCollectionSuccess(browserIsolationCollectorName, duration, maxDataTime)
	return nil
}

func (c *BrowserIsolationCollector) collectAccount(ctx context.Context, accountID string, window TimeWindow) (time.Time, error) {
	query := `query BrowserIsolationSessions($accountTag: string!, $start: Time!, $end: Time!) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      browserIsolationSessionsAdaptiveGroups(
        filter: {datetimeMinute_geq: $start, datetimeMinute_lt: $end}
        limit: 10000
      ) {
        count
        dimensions {
          datetimeMinute
          outcome
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
		return time.Time{}, fmt.Errorf("querying browser isolation sessions: %w", err)
	}

	if len(resp.Errors) > 0 {
		return time.Time{}, fmt.Errorf("graphql errors: %s", resp.Errors[0].Message)
	}

	var data browserIsolationGraphQLResponse
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return time.Time{}, fmt.Errorf("unmarshaling browser isolation response: %w", err)
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
				"outcome", row.Dimensions.Outcome,
			)

			c.store.Add(browserIsolationMetricTotal, dims, t, float64(row.Count))
		}
	}

	return maxTime, nil
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

type browserIsolationPromCollector struct {
	inner *BrowserIsolationCollector
}

// Describe implements prometheus.Collector.
func (p *browserIsolationPromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.sessionsDesc
}

// Collect implements prometheus.Collector.
func (p *browserIsolationPromCollector) Collect(ch chan<- prometheus.Metric) {
	all := p.inner.store.GetAll(browserIsolationMetricTotal)
	for dims, value := range all {
		labels := parseDimensionKey(dims, browserIsolationLabelCount)
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
}
