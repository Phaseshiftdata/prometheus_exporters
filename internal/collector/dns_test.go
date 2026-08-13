package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/store"
)

func dnsResponseData() interface{} {
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	return map[string]interface{}{
		"viewer": map[string]interface{}{
			"zones": []map[string]interface{}{
				{
					"dnsAnalyticsAdaptiveGroups": []map[string]interface{}{
						{
							"count": 1000,
							"dimensions": map[string]string{
								"queryType":      "A",
								"responseCode":   "NOERROR",
								"datetimeMinute": ts,
							},
							"avg": map[string]float64{
								"queryTimeMicroseconds": 500.0,
							},
						},
						{
							"count": 50,
							"dimensions": map[string]string{
								"queryType":      "AAAA",
								"responseCode":   "NXDOMAIN",
								"datetimeMinute": ts,
							},
							"avg": map[string]float64{
								"queryTimeMicroseconds": 200.0,
							},
						},
					},
				},
			},
		},
	}
}

func TestDNSCollector_Collect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(dnsResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	c, err := NewDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	all := ts.store.GetAll("cloudflare_dns_queries_total")
	if len(all) != 2 {
		t.Fatalf("expected 2 dimension keys, got %d", len(all))
	}

	dims := store.MakeDimensionKey("zone_id", "z1", "zone_name", "example.com", "query_type", "A", "response_code", "NOERROR")
	if v := ts.store.Get("cloudflare_dns_queries_total", dims); v != 1000 {
		t.Fatalf("expected 1000, got %f", v)
	}

	// Check duration metric
	durationAll := ts.store.GetAll("cloudflare_dns_query_duration_seconds")
	if len(durationAll) == 0 {
		t.Fatal("expected duration data in store")
	}
}

func TestDNSCollector_Deduplication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(dnsResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	c.Collect(context.Background())
	c.Collect(context.Background())

	dims := store.MakeDimensionKey("zone_id", "z1", "zone_name", "example.com", "query_type", "A", "response_code", "NOERROR")
	if v := ts.store.Get("cloudflare_dns_queries_total", dims); v != 1000 {
		t.Fatalf("expected 1000 (no double count), got %f", v)
	}
}

func TestDNSCollector_PrometheusEmission(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(dnsResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	NewDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	dims := store.MakeDimensionKey("zone_id", "z1", "zone_name", "example.com", "query_type", "A", "response_code", "NOERROR")
	ts.store.Add("cloudflare_dns_queries_total", dims, time.Now(), 1000)
	durDims := store.MakeDimensionKey("zone_id", "z1", "zone_name", "example.com")
	ts.store.Add("cloudflare_dns_query_duration_seconds", durDims, time.Now(), 0.0005)

	families, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	foundQueries := false
	foundDuration := false
	for _, f := range families {
		switch f.GetName() {
		case "cloudflare_dns_queries_total":
			foundQueries = true
		case "cloudflare_dns_query_duration_seconds":
			foundDuration = true
		}
	}
	if !foundQueries {
		t.Error("DNS queries metric not found")
	}
	if !foundDuration {
		t.Error("DNS duration metric not found")
	}
}

func TestDNSCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(dnsResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if c.Name() != "dns" {
		t.Fatalf("expected dns, got %q", c.Name())
	}
	if c.Scope() != ScopeZone {
		t.Fatal("expected zone scope")
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestDNSCollector_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestDNSCollector_PromBadDimKey(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(dnsResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	NewDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	ts.store.Add("cloudflare_dns_queries_total", "bad", time.Now(), 1)
	ts.store.Add("cloudflare_dns_query_duration_seconds", "bad", time.Now(), 1)
	ts.registry.Gather()
}

func TestDNSCollector_GraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"data": map[string]interface{}{}, "errors": []map[string]string{{"message": "err"}}}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()
	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
