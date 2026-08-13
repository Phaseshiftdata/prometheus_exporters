package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/asymmetric-effort/prometheus-exporters/internal/store"
)

func gatewayDNSResponseData() interface{} {
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	return map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"gatewayResolverByCategoryAdaptiveGroups": []map[string]interface{}{
						{
							"count": 100,
							"dimensions": map[string]string{
								"datetimeMinute":   ts,
								"resolverDecision": "allow",
								"category":         "social",
								"locationID":       "loc1",
							},
						},
						{
							"count": 25,
							"dimensions": map[string]string{
								"datetimeMinute":   ts,
								"resolverDecision": "block",
								"category":         "malware",
								"locationID":       "loc1",
							},
						},
					},
				},
			},
		},
	}
}

func TestGatewayDNSCollector_Collect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(gatewayDNSResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)

	c, err := NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	all := ts.store.GetAll("cloudflare_gateway_dns_queries_total")
	if len(all) != 2 {
		t.Fatalf("expected 2 dimension keys, got %d", len(all))
	}

	dims := store.MakeDimensionKey("account_id", "acc1", "resolver_decision", "allow", "category", "social", "location_id", "loc1")
	if v := ts.store.Get("cloudflare_gateway_dns_queries_total", dims); v != 100 {
		t.Fatalf("expected 100, got %f", v)
	}
}

func TestGatewayDNSCollector_Deduplication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(gatewayDNSResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	c.Collect(context.Background())
	c.Collect(context.Background())

	dims := store.MakeDimensionKey("account_id", "acc1", "resolver_decision", "allow", "category", "social", "location_id", "loc1")
	if v := ts.store.Get("cloudflare_gateway_dns_queries_total", dims); v != 100 {
		t.Fatalf("expected 100 (no double count), got %f", v)
	}
}

func TestGatewayDNSCollector_PrometheusEmission(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(gatewayDNSResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)

	NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	dims := store.MakeDimensionKey("account_id", "acc1", "resolver_decision", "allow", "category", "social", "location_id", "loc1")
	ts.store.Add("cloudflare_gateway_dns_queries_total", dims, time.Now(), 50)

	families, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "cloudflare_gateway_dns_queries_total" {
			found = true
		}
	}
	if !found {
		t.Error("gateway DNS metric not found")
	}
}

func TestGatewayDNSCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(gatewayDNSResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	c, _ := NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if c.Name() != "gateway_dns" {
		t.Fatalf("expected gateway_dns, got %q", c.Name())
	}
	if c.Scope() != ScopeAccount {
		t.Fatal("expected account scope")
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestGatewayDNSCollector_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestGatewayDNSCollector_BadTimestamp(t *testing.T) {
	data := map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"gatewayResolverByCategoryAdaptiveGroups": []map[string]interface{}{
						{
							"count": 10,
							"dimensions": map[string]string{
								"datetimeMinute":   "bad-date",
								"resolverDecision": "allow",
								"category":         "social",
								"locationID":       "loc1",
							},
						},
					},
				},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(data))
	}))
	defer server.Close()
	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	c.Collect(context.Background())
	if len(ts.store.GetAll("cloudflare_gateway_dns_queries_total")) != 0 {
		t.Fatal("expected 0 entries with bad timestamp")
	}
}

func TestGatewayDNSCollector_GraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLErrorResponse("not entitled"))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error for GraphQL error response")
	}
}

func TestGatewayDNSCollector_PromBadDimKey(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(gatewayDNSResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	ts.store.Add("cloudflare_gateway_dns_queries_total", "bad\x00key", time.Now(), 1)
	_, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather should not fail: %v", err)
	}
}
