package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func dnsRecordsRESTResponseData() interface{} {
	return struct {
		Success    bool                   `json:"success"`
		Errors     []interface{}          `json:"errors"`
		Messages   []interface{}          `json:"messages"`
		Result     []map[string]interface{} `json:"result"`
		ResultInfo map[string]interface{} `json:"result_info"`
	}{
		Success: true,
		Result: []map[string]interface{}{
			{
				"id":      "rec1",
				"type":    "A",
				"name":    "example.com",
				"content": "192.0.2.1",
				"proxied": true,
				"ttl":     1,
			},
			{
				"id":      "rec2",
				"type":    "A",
				"name":    "www.example.com",
				"content": "192.0.2.2",
				"proxied": true,
				"ttl":     1,
			},
			{
				"id":      "rec3",
				"type":    "CNAME",
				"name":    "mail.example.com",
				"content": "mail.provider.com",
				"proxied": false,
				"ttl":     3600,
			},
			{
				"id":      "rec4",
				"type":    "MX",
				"name":    "example.com",
				"content": "mail.provider.com",
				"proxied": false,
				"ttl":     3600,
			},
		},
		ResultInfo: map[string]interface{}{
			"page":        1,
			"per_page":    100,
			"total_pages": 1,
			"count":       4,
			"total_count": 4,
		},
	}
}

func serveDNSRecords(w http.ResponseWriter, _ *http.Request) {
	b, _ := json.Marshal(dnsRecordsRESTResponseData())
	w.Write(b)
}

func TestDNSRecordsCollector_Collect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(serveDNSRecords))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	c, err := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(c.gauges) != 4 {
		t.Fatalf("expected 4 record gauges, got %d", len(c.gauges))
	}

	if len(c.totals) != 3 {
		t.Fatalf("expected 3 totals (A, CNAME, MX), got %d", len(c.totals))
	}

	// Verify A record count
	for _, tg := range c.totals {
		if tg.rtype == "A" && tg.count != 2 {
			t.Fatalf("expected 2 A records, got %.0f", tg.count)
		}
	}
}

func TestDNSRecordsCollector_PrometheusEmission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(serveDNSRecords))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	c.Collect(context.Background())

	families, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	foundInfo := false
	foundTotal := false
	for _, f := range families {
		switch f.GetName() {
		case "cloudflare_dns_record_info":
			foundInfo = true
			if len(f.GetMetric()) != 4 {
				t.Fatalf("expected 4 dns_record_info metrics, got %d", len(f.GetMetric()))
			}
		case "cloudflare_dns_records_total":
			foundTotal = true
			if len(f.GetMetric()) != 3 {
				t.Fatalf("expected 3 dns_records_total metrics, got %d", len(f.GetMetric()))
			}
		}
	}
	if !foundInfo {
		t.Error("dns_record_info metric not found")
	}
	if !foundTotal {
		t.Error("dns_records_total metric not found")
	}
}

func TestDNSRecordsCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(serveDNSRecords))
	defer server.Close()
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if c.Name() != "dns_records" {
		t.Fatalf("expected dns_records, got %q", c.Name())
	}
	if c.Scope() != ScopeZone {
		t.Fatal("expected zone scope")
	}
	if len(c.RequiredDatasets()) != 0 {
		t.Fatal("expected no required datasets")
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
	if c.Priority() != PriorityBackground {
		t.Fatal("expected background priority")
	}
	if c.Interval() != 60*1000000000 { // 60 seconds in nanoseconds
		t.Fatalf("expected 60s interval, got %v", c.Interval())
	}
}

func TestDNSRecordsCollector_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestDNSRecordsCollector_UnmarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"errors":[],"messages":[],"result":"not-an-array"}`))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestDNSRecordsCollector_EmptyRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Success    bool          `json:"success"`
			Errors     []interface{} `json:"errors"`
			Messages   []interface{} `json:"messages"`
			Result     []interface{} `json:"result"`
			ResultInfo interface{}   `json:"result_info"`
		}{
			Success: true,
			Result:  []interface{}{},
			ResultInfo: map[string]interface{}{
				"page":        1,
				"per_page":    100,
				"total_pages": 1,
				"count":       0,
				"total_count": 0,
			},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(c.gauges) != 0 {
		t.Fatalf("expected 0 gauges, got %d", len(c.gauges))
	}
	if len(c.totals) != 0 {
		t.Fatalf("expected 0 totals, got %d", len(c.totals))
	}
}

func TestDNSRecordsCollector_ContentTruncation(t *testing.T) {
	longContent := strings.Repeat("x", 512) // exceeds maxContentLabelBytes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Success    bool          `json:"success"`
			Errors     []interface{} `json:"errors"`
			Messages   []interface{} `json:"messages"`
			Result     []interface{} `json:"result"`
			ResultInfo interface{}   `json:"result_info"`
		}{
			Success: true,
			Result: []interface{}{
				map[string]interface{}{
					"id":      "rec-long",
					"type":    "TXT",
					"name":    "example.com",
					"content": longContent,
					"proxied": false,
					"ttl":     3600,
				},
			},
			ResultInfo: map[string]interface{}{
				"page":        1,
				"per_page":    100,
				"total_pages": 1,
				"count":       1,
				"total_count": 1,
			},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	c.Collect(context.Background())

	if len(c.gauges) != 1 {
		t.Fatalf("expected 1 gauge, got %d", len(c.gauges))
	}
	if len(c.gauges[0].content) != maxContentLabelBytes {
		t.Fatalf("expected content truncated to %d bytes, got %d", maxContentLabelBytes, len(c.gauges[0].content))
	}
}

func TestDNSRecordsCollector_Pagination(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		page := requestCount
		records := []map[string]interface{}{
			{
				"id":      fmt.Sprintf("rec-p%d", page),
				"type":    "A",
				"name":    "example.com",
				"content": fmt.Sprintf("192.0.2.%d", page),
				"proxied": false,
				"ttl":     300,
			},
		}
		totalPages := 3
		resp := struct {
			Success    bool                     `json:"success"`
			Errors     []interface{}            `json:"errors"`
			Messages   []interface{}            `json:"messages"`
			Result     []map[string]interface{} `json:"result"`
			ResultInfo map[string]interface{}   `json:"result_info"`
		}{
			Success: true,
			Result:  records,
			ResultInfo: map[string]interface{}{
				"page":        page,
				"per_page":    1,
				"total_pages": totalPages,
				"count":       1,
				"total_count": 3,
			},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(c.gauges) != 3 {
		t.Fatalf("expected 3 records across 3 pages, got %d", len(c.gauges))
	}
	if requestCount != 3 {
		t.Fatalf("expected 3 API requests (pagination), got %d", requestCount)
	}
}

func TestDNSRecordsCollector_MultipleZones(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Determine which zone from the URL
		path := r.URL.Path
		var zoneName string
		if strings.Contains(path, "z1") {
			zoneName = "example.com"
		} else {
			zoneName = "example.org"
		}
		resp := struct {
			Success    bool                     `json:"success"`
			Errors     []interface{}            `json:"errors"`
			Messages   []interface{}            `json:"messages"`
			Result     []map[string]interface{} `json:"result"`
			ResultInfo map[string]interface{}   `json:"result_info"`
		}{
			Success: true,
			Result: []map[string]interface{}{
				{
					"id":      "rec-" + zoneName,
					"type":    "A",
					"name":    zoneName,
					"content": "192.0.2.1",
					"proxied": false,
					"ttl":     300,
				},
			},
			ResultInfo: map[string]interface{}{
				"page":        1,
				"per_page":    100,
				"total_pages": 1,
				"count":       1,
				"total_count": 1,
			},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{
		{ID: "z1", Name: "example.com"},
		{ID: "z2", Name: "example.org"},
	}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(c.gauges) != 2 {
		t.Fatalf("expected 2 records (one per zone), got %d", len(c.gauges))
	}
	if len(c.totals) != 2 {
		t.Fatalf("expected 2 totals (one per zone), got %d", len(c.totals))
	}
}

func TestDNSRecordsCollector_APIErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Success  bool          `json:"success"`
			Errors   []interface{} `json:"errors"`
			Messages []interface{} `json:"messages"`
			Result   interface{}   `json:"result"`
		}{
			Success: false,
			Errors: []interface{}{
				map[string]interface{}{"code": 10000, "message": "Authentication error"},
			},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error from failed API response")
	}
}

func TestDNSRecordsCollector_ProxiedLabels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Success    bool                     `json:"success"`
			Errors     []interface{}            `json:"errors"`
			Messages   []interface{}            `json:"messages"`
			Result     []map[string]interface{} `json:"result"`
			ResultInfo map[string]interface{}   `json:"result_info"`
		}{
			Success: true,
			Result: []map[string]interface{}{
				{
					"id":      "rec-proxied",
					"type":    "A",
					"name":    "example.com",
					"content": "192.0.2.1",
					"proxied": true,
					"ttl":     1,
				},
				{
					"id":      "rec-unproxied",
					"type":    "A",
					"name":    "direct.example.com",
					"content": "192.0.2.2",
					"proxied": false,
					"ttl":     3600,
				},
			},
			ResultInfo: map[string]interface{}{
				"page": 1, "per_page": 100, "total_pages": 1,
				"count": 2, "total_count": 2,
			},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	c.Collect(context.Background())

	for _, g := range c.gauges {
		if g.id == "rec-proxied" && g.proxied != "true" {
			t.Errorf("expected proxied=true for rec-proxied, got %q", g.proxied)
		}
		if g.id == "rec-unproxied" && g.proxied != "false" {
			t.Errorf("expected proxied=false for rec-unproxied, got %q", g.proxied)
		}
		if g.id == "rec-unproxied" && g.ttl != "3600" {
			t.Errorf("expected ttl=3600 for rec-unproxied, got %q", g.ttl)
		}
	}
}

func TestDNSRecordsCollector_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not-valid-json`))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error from invalid JSON")
	}
}

func TestDNSRecordsCollector_PartialZoneFailure(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if strings.Contains(r.URL.Path, "z-fail") {
			w.WriteHeader(500)
			return
		}
		resp := struct {
			Success    bool                     `json:"success"`
			Errors     []interface{}            `json:"errors"`
			Messages   []interface{}            `json:"messages"`
			Result     []map[string]interface{} `json:"result"`
			ResultInfo map[string]interface{}   `json:"result_info"`
		}{
			Success: true,
			Result: []map[string]interface{}{
				{
					"id":      "rec1",
					"type":    "A",
					"name":    "ok.example.com",
					"content": "192.0.2.1",
					"proxied": false,
					"ttl":     300,
				},
			},
			ResultInfo: map[string]interface{}{
				"page": 1, "per_page": 100, "total_pages": 1,
				"count": 1, "total_count": 1,
			},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{
		{ID: "z-ok", Name: "ok.example.com"},
		{ID: "z-fail", Name: "fail.example.com"},
	}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	// Collect returns error because one zone failed, but the successful zone's
	// records should still be in the gauge snapshot.
	err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error from partial failure")
	}

	if len(c.gauges) != 1 {
		t.Fatalf("expected 1 gauge from successful zone, got %d", len(c.gauges))
	}
}

func TestDNSRecordsCollector_NilResultInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Success  bool                     `json:"success"`
			Errors   []interface{}            `json:"errors"`
			Messages []interface{}            `json:"messages"`
			Result   []map[string]interface{} `json:"result"`
		}{
			Success: true,
			Result: []map[string]interface{}{
				{
					"id":      "rec1",
					"type":    "A",
					"name":    "example.com",
					"content": "192.0.2.1",
					"proxied": false,
					"ttl":     300,
				},
			},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("expected no error with nil result_info, got: %v", err)
	}
	if len(c.gauges) != 1 {
		t.Fatalf("expected 1 gauge, got %d", len(c.gauges))
	}
}

func TestProbeDNSRecordsAccess_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Success  bool          `json:"success"`
			Errors   []interface{} `json:"errors"`
			Messages []interface{} `json:"messages"`
			Result   []interface{} `json:"result"`
		}{
			Success: true,
			Result:  []interface{}{},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()

	client := createTestClient(server)
	if !ProbeDNSRecordsAccess(context.Background(), client, "z1") {
		t.Fatal("expected probe to succeed")
	}
}

func TestProbeDNSRecordsAccess_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"forbidden"}]}`))
	}))
	defer server.Close()

	client := createTestClient(server)
	if ProbeDNSRecordsAccess(context.Background(), client, "z1") {
		t.Fatal("expected probe to fail on 403")
	}
}

func TestDNSRecordsCollector_RegistrationError(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(serveDNSRecords))
	defer server.Close()
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	// First registration succeeds
	_, err := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	// Second registration with same registry should fail
	_, err = NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	if err == nil {
		t.Fatal("expected error on duplicate registration")
	}
}

func TestDNSRecordsCollector_NoZones(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(serveDNSRecords))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDNSRecordsCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("expected no error with nil zones, got: %v", err)
	}
	if len(c.gauges) != 0 {
		t.Fatalf("expected 0 gauges with nil zones, got %d", len(c.gauges))
	}
}
