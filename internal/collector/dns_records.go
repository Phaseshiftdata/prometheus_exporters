package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/cloudflare"
	"github.com/phaseshiftdata/prometheus_exporters/internal/governor"
	"github.com/phaseshiftdata/prometheus_exporters/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	dnsRecordsCollectorName = "dns_records"

	metricDNSRecordInfo   = "cloudflare_dns_record_info"
	metricDNSRecordsTotal = "cloudflare_dns_records_total"

	// dnsRecordsPerPage is the number of records to request per page.
	dnsRecordsPerPage = 100

	// dnsRecordsMaxPages caps pagination to prevent runaway requests.
	dnsRecordsMaxPages = 100

	// maxContentLabelBytes caps the content label to prevent excessively
	// long TXT record values from inflating label bytes.
	maxContentLabelBytes = 256
)

// dnsRecord represents a single DNS record returned by the Cloudflare
// REST API (/zones/{id}/dns_records).
type dnsRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

// dnsRecordGauge holds a snapshot of a single DNS record info metric.
type dnsRecordGauge struct {
	id       string
	zoneID   string
	zoneName string
	rtype    string
	name     string
	content  string
	proxied  string
	ttl      string
}

// dnsRecordsTotalGauge holds a count of DNS records per zone and type.
type dnsRecordsTotalGauge struct {
	zoneName string
	rtype    string
	count    float64
}

// DNSRecordsCollector enumerates DNS records per zone via the Cloudflare
// REST API and exposes them as info-style gauge metrics for change tracking.
type DNSRecordsCollector struct {
	client      *cloudflare.Client
	store       *store.Store
	selfMetrics *SelfMetrics
	logger      *zap.Logger

	scrapeDelay     int
	timeWindow      int
	refreshInterval int

	accountIDs []string
	zones      []ZoneInfo

	descRecordInfo   *prometheus.Desc
	descRecordsTotal *prometheus.Desc

	mu     sync.RWMutex
	gauges []dnsRecordGauge
	totals []dnsRecordsTotalGauge
}

// NewDNSRecordsCollector creates a new DNSRecordsCollector and registers its
// prometheus descriptors with the provided registry.
func NewDNSRecordsCollector(
	client *cloudflare.Client,
	st *store.Store,
	selfMetrics *SelfMetrics,
	logger *zap.Logger,
	scrapeDelay, timeWindow, refreshInterval int,
	accountIDs []string,
	zones []ZoneInfo,
	reg prometheus.Registerer,
) (*DNSRecordsCollector, error) {
	c := &DNSRecordsCollector{
		client:          client,
		store:           st,
		selfMetrics:     selfMetrics,
		logger:          logger.Named(dnsRecordsCollectorName),
		scrapeDelay:     scrapeDelay,
		timeWindow:      timeWindow,
		refreshInterval: refreshInterval,
		accountIDs:      accountIDs,
		zones:           zones,
		descRecordInfo: prometheus.NewDesc(
			metricDNSRecordInfo,
			"DNS record info metric (constant value 1). Record data is carried in labels for change tracking.",
			[]string{"id", "zone_name", "zone_id", "type", "name", "content", "proxied", "ttl"},
			nil,
		),
		descRecordsTotal: prometheus.NewDesc(
			metricDNSRecordsTotal,
			"Total number of DNS records per zone and record type.",
			[]string{"zone_name", "type"},
			nil,
		),
	}

	if err := reg.Register(&dnsRecordsPromCollector{c}); err != nil {
		return nil, fmt.Errorf("registering dns_records collector: %w", err)
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// collector.Collector interface
// ---------------------------------------------------------------------------

func (c *DNSRecordsCollector) Name() string                     { return dnsRecordsCollectorName }
func (c *DNSRecordsCollector) Priority() governor.PriorityClass { return PriorityBackground }
func (c *DNSRecordsCollector) Interval() time.Duration {
	return time.Duration(c.refreshInterval) * time.Second
}
func (c *DNSRecordsCollector) RequiredDatasets() []string { return []string{} }
func (c *DNSRecordsCollector) Scope() Scope               { return ScopeZone }

// Describe returns a human-readable description.
func (c *DNSRecordsCollector) Describe() string {
	return "Enumerates DNS records per zone via the REST API, exposing record values as labels for change tracking."
}

// Collect fetches DNS records for every configured zone and records
// info-style gauge metrics.
func (c *DNSRecordsCollector) Collect(ctx context.Context) error {
	start := time.Now()

	var newGauges []dnsRecordGauge
	totalsByKey := make(map[string]*dnsRecordsTotalGauge)
	var lastErr error

	for _, z := range c.zones {
		records, err := c.fetchAllDNSRecords(ctx, z.ID)
		if err != nil {
			c.logger.Error("failed to fetch DNS records",
				zap.String("zone_id", z.ID),
				zap.String("zone_name", z.Name),
				zap.Error(err),
			)
			lastErr = err
			continue
		}

		for _, r := range records {
			content := r.Content
			if len(content) > maxContentLabelBytes {
				content = content[:maxContentLabelBytes]
			}

			newGauges = append(newGauges, dnsRecordGauge{
				id:       r.ID,
				zoneID:   z.ID,
				zoneName: z.Name,
				rtype:    r.Type,
				name:     r.Name,
				content:  content,
				proxied:  strconv.FormatBool(r.Proxied),
				ttl:      strconv.Itoa(r.TTL),
			})

			key := z.Name + "\x00" + r.Type
			if tg, ok := totalsByKey[key]; ok {
				tg.count++
			} else {
				totalsByKey[key] = &dnsRecordsTotalGauge{
					zoneName: z.Name,
					rtype:    r.Type,
					count:    1,
				}
			}
		}
	}

	newTotals := make([]dnsRecordsTotalGauge, 0, len(totalsByKey))
	for _, tg := range totalsByKey {
		newTotals = append(newTotals, *tg)
	}

	// Atomically replace gauge snapshot.
	c.mu.Lock()
	c.gauges = newGauges
	c.totals = newTotals
	c.mu.Unlock()

	duration := time.Since(start)
	if lastErr != nil {
		c.selfMetrics.RecordCollectionError(dnsRecordsCollectorName, "rest_api", duration)
		return lastErr
	}
	c.selfMetrics.RecordCollectionSuccess(dnsRecordsCollectorName, duration, time.Now())
	return nil
}

// dnsRecordsResponse holds the paginated response from the DNS records API.
type dnsRecordsResponse struct {
	Success    bool            `json:"success"`
	Errors     []cloudflare.RESTError `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *resultInfo     `json:"result_info"`
}

// resultInfo holds pagination metadata from the Cloudflare API.
type resultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalPages int `json:"total_pages"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
}

// fetchAllDNSRecords retrieves all DNS records for a zone, paginating as
// needed up to dnsRecordsMaxPages.
func (c *DNSRecordsCollector) fetchAllDNSRecords(ctx context.Context, zoneID string) ([]dnsRecord, error) {
	var allRecords []dnsRecord

	for page := 1; page <= dnsRecordsMaxPages; page++ {
		path := fmt.Sprintf("/zones/%s/dns_records?per_page=%d&page=%d",
			url.PathEscape(zoneID), dnsRecordsPerPage, page)

		raw, _, err := c.client.RESTGet(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("fetching DNS records for zone %s page %d: %w", zoneID, page, err)
		}

		var resp dnsRecordsResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("parsing DNS records response for zone %s page %d: %w", zoneID, page, err)
		}

		if !resp.Success {
			msgs := make([]string, len(resp.Errors))
			for i, e := range resp.Errors {
				msgs[i] = e.Message
			}
			return nil, fmt.Errorf("DNS records API error for zone %s: %v", zoneID, msgs)
		}

		var records []dnsRecord
		if err := json.Unmarshal(resp.Result, &records); err != nil {
			return nil, fmt.Errorf("unmarshaling DNS records for zone %s page %d: %w", zoneID, page, err)
		}

		allRecords = append(allRecords, records...)

		// Check if there are more pages.
		if resp.ResultInfo == nil || page >= resp.ResultInfo.TotalPages {
			break
		}
	}

	return allRecords, nil
}

// ProbeDNSRecordsAccess issues a minimal probe request to determine whether
// the API token has Zone -> DNS: Read permission for the given zone. Returns
// true if the probe succeeds (HTTP 200), false otherwise.
func ProbeDNSRecordsAccess(ctx context.Context, client *cloudflare.Client, zoneID string) bool {
	path := fmt.Sprintf("/zones/%s/dns_records?per_page=1", url.PathEscape(zoneID))
	_, _, err := client.RESTGet(ctx, path)
	return err == nil
}

// ---------------------------------------------------------------------------
// prometheus.Collector adapter
// ---------------------------------------------------------------------------

// dnsRecordsPromCollector adapts DNSRecordsCollector to the prometheus.Collector
// interface. This wrapper is necessary because collector.Collector and
// prometheus.Collector both define Describe and Collect methods with
// incompatible signatures.
type dnsRecordsPromCollector struct {
	inner *DNSRecordsCollector
}

// Describe implements prometheus.Collector.
func (p *dnsRecordsPromCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.inner.descRecordInfo
	ch <- p.inner.descRecordsTotal
}

// Collect implements prometheus.Collector. It reads the latest gauge snapshot
// and emits them as constant metrics.
func (p *dnsRecordsPromCollector) Collect(ch chan<- prometheus.Metric) {
	p.inner.mu.RLock()
	defer p.inner.mu.RUnlock()

	for _, g := range p.inner.gauges {
		ch <- prometheus.MustNewConstMetric(
			p.inner.descRecordInfo,
			prometheus.GaugeValue,
			1,
			g.id, g.zoneName, g.zoneID, g.rtype, g.name, g.content, g.proxied, g.ttl,
		)
	}

	for _, t := range p.inner.totals {
		ch <- prometheus.MustNewConstMetric(
			p.inner.descRecordsTotal,
			prometheus.GaugeValue,
			t.count,
			t.zoneName, t.rtype,
		)
	}
}
