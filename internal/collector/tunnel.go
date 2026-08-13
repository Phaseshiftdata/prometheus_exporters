package collector

import (
	"context"
	"encoding/json"
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
	tunnelCollectorName       = "tunnel"
	tunnelMetricRequests      = "cloudflare_tunnel_requests_total"
	tunnelMetricInfo          = "cloudflare_tunnel_info"
	tunnelDataset             = "cloudflareTunnelsAnalyticsAdaptiveGroups"
	tunnelRequestsLabelCount  = 3 // account_id, tunnel_id, tunnel_name
	tunnelInfoLabelCount      = 4 // account_id, tunnel_id, tunnel_name, status
)

// tunnelGraphQLResponse is the typed response for tunnel analytics queries.
type tunnelGraphQLResponse struct {
	Viewer struct {
		Accounts []struct {
			Dataset []struct {
				Count      int `json:"count"`
				Dimensions struct {
					DatetimeMinute string `json:"datetimeMinute"`
					TunnelID       string `json:"tunnelID"`
					TunnelName     string `json:"tunnelName"`
				} `json:"dimensions"`
			} `json:"cloudflareTunnelsAnalyticsAdaptiveGroups"`
		} `json:"accounts"`
	} `json:"viewer"`
}

// tunnelRESTResponse represents a single tunnel from the REST API listing.
type tunnelRESTResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// tunnelInventoryEntry caches the tunnel inventory.
type tunnelInventoryEntry struct {
	tunnelID   string
	tunnelName string
	status     string
}

// TunnelCollector collects Cloudflare Tunnel request metrics and inventory info.
type TunnelCollector struct {
	client          *cloudflare.Client
	store           *store.Store
	selfMetrics     *SelfMetrics
	logger          *zap.Logger
	accountIDs      []string
	scrapeDelay     int
	timeWindow      int
	refreshInterval int

	requestsDesc *prometheus.Desc
	infoDesc     *prometheus.Desc

	mu        sync.RWMutex
	inventory map[string][]tunnelInventoryEntry // keyed by account_id
}

// NewTunnelCollector creates a new TunnelCollector and registers its prometheus
// descriptors with the provided registry.
func NewTunnelCollector(
	client *cloudflare.Client,
	s *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay int,
	timeWindow int,
	refreshInterval int,
	accountIDs []string,
	reg prometheus.Registerer,
) (*TunnelCollector, error) {
	c := &TunnelCollector{
		client:          client,
		store:           s,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(tunnelCollectorName),
		accountIDs:      accountIDs,
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		inventory:       make(map[string][]tunnelInventoryEntry),
		requestsDesc: prometheus.NewDesc(
			tunnelMetricRequests,
			"Account-side tunnel request count. Join against cloudflared_* for connector-side truth.",
			[]string{"account_id", "tunnel_id", "tunnel_name"},
			nil,
		),
		infoDesc: prometheus.NewDesc(
			tunnelMetricInfo,
			"Tunnel inventory information. Constant value 1; use labels for metadata.",
			[]string{"account_id", "tunnel_id", "tunnel_name", "status"},
			nil,
		),
	}

	if err := reg.Register(&tunnelPromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering tunnel collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

// Name returns the collector name.
func (c *TunnelCollector) Name() string { return tunnelCollectorName }

// Priority returns the priority class.
func (c *TunnelCollector) Priority() governor.PriorityClass { return governor.PriorityStandard }

// Interval returns the desired collection interval.
func (c *TunnelCollector) Interval() time.Duration {
	return time.Duration(c.refreshInterval) * time.Second
}

// RequiredDatasets returns the dataset names this collector needs.
func (c *TunnelCollector) RequiredDatasets() []string {
	return []string{tunnelDataset}
}

// Scope returns the scope of this collector.
func (c *TunnelCollector) Scope() Scope { return ScopeAccount }

// Describe returns a human-readable description of the collector.
func (c *TunnelCollector) Describe() string {
	return "Collects Cloudflare Tunnel request counts via GraphQL and tunnel inventory via the REST API."
}

// Collect executes a collection run: first queries GraphQL for request counts,
// then refreshes the tunnel inventory via the REST API.
func (c *TunnelCollector) Collect(ctx context.Context) error {
	start := time.Now()
	window := CalculateWindow(time.Now(), c.scrapeDelay, c.timeWindow)

	var lastErr error
	var maxDataTime time.Time

	for _, accountID := range c.accountIDs {
		dataTime, err := c.collectAccount(ctx, accountID, window)
		if err != nil {
			c.logger.Error("failed to collect tunnel metrics",
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
		c.selfMetrics.RecordCollectionError(tunnelCollectorName, "query_failed", duration)
		return lastErr
	}

	if maxDataTime.IsZero() {
		maxDataTime = window.End
	}
	c.selfMetrics.RecordCollectionSuccess(tunnelCollectorName, duration, maxDataTime)
	return nil
}

func (c *TunnelCollector) collectAccount(ctx context.Context, accountID string, window TimeWindow) (time.Time, error) {
	maxTime, err := c.collectRequests(ctx, accountID, window)
	if err != nil {
		return time.Time{}, err
	}

	if err := c.refreshInventory(ctx, accountID); err != nil {
		c.logger.Warn("failed to refresh tunnel inventory; using cached data",
			zap.String("account_id", accountID),
			zap.Error(err),
		)
		// Non-fatal: we can still serve request metrics from the store.
	}

	return maxTime, nil
}

func (c *TunnelCollector) collectRequests(ctx context.Context, accountID string, window TimeWindow) (time.Time, error) {
	query := `query TunnelRequests($accountTag: string!, $start: Time!, $end: Time!) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      cloudflareTunnelsAnalyticsAdaptiveGroups(
        filter: {datetimeMinute_geq: $start, datetimeMinute_lt: $end}
        limit: 10000
      ) {
        count
        dimensions {
          datetimeMinute
          tunnelID
          tunnelName
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
		return time.Time{}, fmt.Errorf("querying tunnel requests: %w", err)
	}

	if len(resp.Errors) > 0 {
		return time.Time{}, fmt.Errorf("graphql errors: %s", resp.Errors[0].Message)
	}

	var data tunnelGraphQLResponse
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return time.Time{}, fmt.Errorf("unmarshaling tunnel response: %w", err)
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
				"tunnel_id", row.Dimensions.TunnelID,
				"tunnel_name", row.Dimensions.TunnelName,
			)

			c.store.Add(tunnelMetricRequests, dims, t, float64(row.Count))
		}
	}

	return maxTime, nil
}

func (c *TunnelCollector) refreshInventory(ctx context.Context, accountID string) error {
	path := fmt.Sprintf("/accounts/%s/cfd_tunnel", accountID)

	body, _, err := c.client.RESTGet(ctx, path)
	if err != nil {
		return fmt.Errorf("fetching tunnel inventory: %w", err)
	}

	restResp, err := cloudflare.ParseRESTResponse(body)
	if err != nil {
		return fmt.Errorf("parsing tunnel inventory response: %w", err)
	}

	var tunnels []tunnelRESTResponse
	if err := json.Unmarshal(restResp.Result, &tunnels); err != nil {
		return fmt.Errorf("unmarshaling tunnel list: %w", err)
	}

	entries := make([]tunnelInventoryEntry, 0, len(tunnels))
	for _, t := range tunnels {
		entries = append(entries, tunnelInventoryEntry{
			tunnelID:   t.ID,
			tunnelName: t.Name,
			status:     t.Status,
		})
	}

	c.mu.Lock()
	c.inventory[accountID] = entries
	c.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

type tunnelPromCollector struct {
	inner *TunnelCollector
}

// Describe implements prometheus.Collector.
func (p *tunnelPromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.requestsDesc
	ch <- p.inner.infoDesc
}

// Collect implements prometheus.Collector.
func (p *tunnelPromCollector) Collect(ch chan<- prometheus.Metric) {
	// Emit request counters from the store.
	requests := p.inner.store.GetAll(tunnelMetricRequests)
	for dims, value := range requests {
		labels := parseDimensionKey(dims, tunnelRequestsLabelCount)
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

	// Emit tunnel info gauge from the cached inventory.
	p.inner.mu.RLock()
	defer p.inner.mu.RUnlock()

	for accountID, entries := range p.inner.inventory {
		for _, entry := range entries {
			ch <- prometheus.MustNewConstMetric(
				p.inner.infoDesc,
				prometheus.GaugeValue,
				1,
				accountID,
				entry.tunnelID,
				entry.tunnelName,
				entry.status,
			)
		}
	}
}
