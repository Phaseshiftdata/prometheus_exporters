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
	accessCollectorName  = "access"
	accessMetricRequests = "cloudflare_access_login_requests_total"
	accessDataset        = "accessLoginRequestsAdaptiveGroups"
	accessLabelCount     = 4 // account_id, app_id, identity_provider, result
)

// accessGraphQLResponse is the typed response for the access login requests query.
type accessGraphQLResponse struct {
	Viewer struct {
		Accounts []struct {
			Dataset []struct {
				Count      int `json:"count"`
				Dimensions struct {
					DatetimeMinute   string `json:"datetimeMinute"`
					AppID            string `json:"appID"`
					IdentityProvider string `json:"identityProvider"`
					Result           string `json:"result"`
				} `json:"dimensions"`
			} `json:"accessLoginRequestsAdaptiveGroups"`
		} `json:"accounts"`
	} `json:"viewer"`
}

// AccessCollector collects Cloudflare Access login request metrics.
type AccessCollector struct {
	client          *cloudflare.Client
	store           *store.Store
	selfMetrics     *SelfMetrics
	logger          *zap.Logger
	accountIDs      []string
	scrapeDelay     int
	timeWindow      int
	refreshInterval int

	requestsDesc *prometheus.Desc
}

// NewAccessCollector creates a new AccessCollector and registers its prometheus
// descriptors with the provided registry.
func NewAccessCollector(
	client *cloudflare.Client,
	s *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay int,
	timeWindow int,
	refreshInterval int,
	accountIDs []string,
	reg prometheus.Registerer,
) (*AccessCollector, error) {
	c := &AccessCollector{
		client:          client,
		store:           s,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(accessCollectorName),
		accountIDs:      accountIDs,
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		requestsDesc: prometheus.NewDesc(
			accessMetricRequests,
			"Total Access login requests. result is the authentication outcome, not a user identity. Values are operational signals, not billing records.",
			[]string{"account_id", "app_id", "identity_provider", "result"},
			nil,
		),
	}

	if err := reg.Register(&accessPromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering access collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

// Name returns the collector name.
func (c *AccessCollector) Name() string { return accessCollectorName }

// Priority returns the priority class.
func (c *AccessCollector) Priority() governor.PriorityClass { return governor.PriorityCritical }

// Interval returns the desired collection interval.
func (c *AccessCollector) Interval() time.Duration {
	return time.Duration(c.refreshInterval) * time.Second
}

// RequiredDatasets returns the dataset names this collector needs.
func (c *AccessCollector) RequiredDatasets() []string {
	return []string{accessDataset}
}

// Scope returns the scope of this collector.
func (c *AccessCollector) Scope() Scope { return ScopeAccount }

// Describe returns a human-readable description of the collector.
func (c *AccessCollector) Describe() string {
	return "Collects Cloudflare Access login request metrics per application, identity provider, and result."
}

// Collect executes a collection run against the Cloudflare GraphQL API.
func (c *AccessCollector) Collect(ctx context.Context) error {
	start := time.Now()
	window := CalculateWindow(time.Now(), c.scrapeDelay, c.timeWindow)

	var lastErr error
	var maxDataTime time.Time

	for _, accountID := range c.accountIDs {
		dataTime, err := c.collectAccount(ctx, accountID, window)
		if err != nil {
			c.logger.Error("failed to collect access metrics",
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
		c.selfMetrics.RecordCollectionError(accessCollectorName, "query_failed", duration)
		return lastErr
	}

	if maxDataTime.IsZero() {
		maxDataTime = window.End
	}
	c.selfMetrics.RecordCollectionSuccess(accessCollectorName, duration, maxDataTime)
	return nil
}

func (c *AccessCollector) collectAccount(ctx context.Context, accountID string, window TimeWindow) (time.Time, error) {
	query := `query AccessLoginRequests($accountTag: string!, $start: Time!, $end: Time!) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      accessLoginRequestsAdaptiveGroups(
        filter: {datetimeMinute_geq: $start, datetimeMinute_lt: $end}
        limit: 10000
      ) {
        count
        dimensions {
          datetimeMinute
          appID
          identityProvider
          result
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
		return time.Time{}, fmt.Errorf("querying access login requests: %w", err)
	}

	if len(resp.Errors) > 0 {
		return time.Time{}, fmt.Errorf("graphql errors: %s", resp.Errors[0].Message)
	}

	var data accessGraphQLResponse
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return time.Time{}, fmt.Errorf("unmarshaling access response: %w", err)
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
				"app_id", row.Dimensions.AppID,
				"identity_provider", row.Dimensions.IdentityProvider,
				"result", row.Dimensions.Result,
			)

			c.store.Add(accessMetricRequests, dims, t, float64(row.Count))
		}
	}

	return maxTime, nil
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

// accessPromCollector adapts AccessCollector to the prometheus.Collector
// interface. This wrapper is necessary because collector.Collector and
// prometheus.Collector both define Describe and Collect methods with
// incompatible signatures.
type accessPromCollector struct {
	inner *AccessCollector
}

// Describe implements prometheus.Collector.
func (p *accessPromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.requestsDesc
}

// Collect implements prometheus.Collector. It reads accumulated values from the
// store and emits them as constant metrics.
func (p *accessPromCollector) Collect(ch chan<- prometheus.Metric) {
	all := p.inner.store.GetAll(accessMetricRequests)
	for dims, value := range all {
		labels := parseDimensionKey(dims, accessLabelCount)
		if labels == nil {
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			p.inner.requestsDesc,
			prometheus.CounterValue,
			value,
			dimValues(labels)...,
		)
	}
}
