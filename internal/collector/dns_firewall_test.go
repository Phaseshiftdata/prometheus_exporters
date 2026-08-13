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

func dnsFirewallResponseData() interface{} {
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	return map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"dnsFirewallAnalyticsAdaptiveGroups": []map[string]interface{}{
						{
							"count": 500,
							"dimensions": map[string]string{
								"clusterID":      "cluster1",
								"responseCode":   "NOERROR",
								"cacheStatus":    "hit",
								"datetimeMinute": ts,
							},
						},
						{
							"count": 100,
							"dimensions": map[string]string{
								"clusterID":      "cluster1",
								"responseCode":   "NXDOMAIN",
								"cacheStatus":    "miss",
								"datetimeMinute": ts,
							},
						},
					},
				},
			},
		},
	}
}

func TestDNSFirewallCollector_Collect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(dnsFirewallResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	c, err := NewDNSFirewallCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	all := ts.store.GetAll("cloudflare_dns_firewall_queries_total")
	if len(all) != 2 {
		t.Fatalf("expected 2 dimension keys, got %d", len(all))
	}

	dims := store.MakeDimensionKey("account_id", "acc1", "cluster_id", "cluster1", "response_code", "NOERROR", "cache_status", "hit")
	if v := ts.store.Get("cloudflare_dns_firewall_queries_total", dims); v != 500 {
		t.Fatalf("expected 500, got %f", v)
	}
}

func TestDNSFirewallCollector_Deduplication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(dnsFirewallResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDNSFirewallCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)

	c.Collect(context.Background())
	c.Collect(context.Background())

	dims := store.MakeDimensionKey("account_id", "acc1", "cluster_id", "cluster1", "response_code", "NOERROR", "cache_status", "hit")
	if v := ts.store.Get("cloudflare_dns_firewall_queries_total", dims); v != 500 {
		t.Fatalf("expected 500, got %f", v)
	}
}

func TestDNSFirewallCollector_PrometheusEmission(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(dnsFirewallResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	NewDNSFirewallCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)

	dims := store.MakeDimensionKey("account_id", "acc1", "cluster_id", "c1", "response_code", "NOERROR", "cache_status", "hit")
	ts.store.Add("cloudflare_dns_firewall_queries_total", dims, time.Now(), 500)

	families, _ := ts.registry.Gather()
	found := false
	for _, f := range families {
		if f.GetName() == "cloudflare_dns_firewall_queries_total" {
			found = true
		}
	}
	if !found {
		t.Error("DNS firewall metric not found")
	}
}

func TestDNSFirewallCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(dnsFirewallResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	c, _ := NewDNSFirewallCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)

	if c.Name() != "dns_firewall" {
		t.Fatalf("expected dns_firewall, got %q", c.Name())
	}
	if c.Scope() != ScopeAccount {
		t.Fatal("expected account scope")
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestDNSFirewallCollector_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()
	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDNSFirewallCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestDNSFirewallCollector_PromBadDimKey(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(dnsFirewallResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	NewDNSFirewallCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)
	ts.store.Add("cloudflare_dns_firewall_queries_total", "bad", time.Now(), 1)
	ts.registry.Gather()
}

func TestDNSFirewallCollector_GraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"data": map[string]interface{}{}, "errors": []map[string]string{{"message": "err"}}}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()
	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDNSFirewallCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)
	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
