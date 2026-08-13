package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/asymmetric-effort/prometheus-exporters/internal/store"
)

func tunnelGraphQLResponseData() interface{} {
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	return map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"cloudflareTunnelsAnalyticsAdaptiveGroups": []map[string]interface{}{
						{
							"count": 200,
							"dimensions": map[string]string{
								"datetimeMinute": ts,
								"tunnelID":       "t1",
								"tunnelName":     "prod-tunnel",
							},
						},
					},
				},
			},
		},
	}
}

func tunnelRESTResponseData() interface{} {
	return []map[string]string{
		{"id": "t1", "name": "prod-tunnel", "status": "healthy"},
		{"id": "t2", "name": "staging-tunnel", "status": "down"},
	}
}

func TestTunnelCollector_Collect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write(makeGraphQLResponse(tunnelGraphQLResponseData()))
		} else {
			w.Write(makeRESTResponse(tunnelRESTResponseData()))
		}
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, err := NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	dims := store.MakeDimensionKey("account_id", "acc1", "tunnel_id", "t1", "tunnel_name", "prod-tunnel")
	if v := ts.store.Get("cloudflare_tunnel_requests_total", dims); v != 200 {
		t.Fatalf("expected 200, got %f", v)
	}
}

func TestTunnelCollector_Deduplication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write(makeGraphQLResponse(tunnelGraphQLResponseData()))
		} else {
			w.Write(makeRESTResponse(tunnelRESTResponseData()))
		}
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	c.Collect(context.Background())
	c.Collect(context.Background())

	dims := store.MakeDimensionKey("account_id", "acc1", "tunnel_id", "t1", "tunnel_name", "prod-tunnel")
	if v := ts.store.Get("cloudflare_tunnel_requests_total", dims); v != 200 {
		t.Fatalf("expected 200, got %f", v)
	}
}

func TestTunnelCollector_RESTInventory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write(makeGraphQLResponse(tunnelGraphQLResponseData()))
		} else {
			w.Write(makeRESTResponse(tunnelRESTResponseData()))
		}
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	c.Collect(context.Background())

	c.mu.RLock()
	inv := c.inventory["acc1"]
	c.mu.RUnlock()

	if len(inv) != 2 {
		t.Fatalf("expected 2 tunnels in inventory, got %d", len(inv))
	}
}

func TestTunnelCollector_PrometheusEmission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write(makeGraphQLResponse(tunnelGraphQLResponseData()))
		} else {
			w.Write(makeRESTResponse(tunnelRESTResponseData()))
		}
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	c.Collect(context.Background())

	families, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	foundRequests := false
	foundInfo := false
	for _, f := range families {
		switch f.GetName() {
		case "cloudflare_tunnel_requests_total":
			foundRequests = true
		case "cloudflare_tunnel_info":
			foundInfo = true
			if len(f.GetMetric()) != 2 {
				t.Fatalf("expected 2 tunnel info metrics, got %d", len(f.GetMetric()))
			}
		}
	}
	if !foundRequests {
		t.Error("tunnel requests metric not found")
	}
	if !foundInfo {
		t.Error("tunnel info metric not found")
	}
}

func TestTunnelCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(tunnelGraphQLResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	c, _ := NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if c.Name() != "tunnel" {
		t.Fatalf("expected tunnel, got %q", c.Name())
	}
	if c.Scope() != ScopeAccount {
		t.Fatal("expected account scope")
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestTunnelCollector_RESTFailure_NonFatal(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write(makeGraphQLResponse(tunnelGraphQLResponseData()))
		} else {
			callCount++
			w.WriteHeader(500)
			w.Write([]byte(`error`))
		}
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	// REST failure is non-fatal
	err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("expected nil error (REST failure is non-fatal), got: %v", err)
	}
}

func TestTunnelCollector_GraphQLWithErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			resp := map[string]interface{}{
				"data":   map[string]interface{}{},
				"errors": []map[string]string{{"message": "some error"}},
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			w.Write(makeRESTResponse(tunnelRESTResponseData()))
		}
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error for GraphQL errors")
	}
}

func TestTunnelCollector_BadTimestamp(t *testing.T) {
	data := map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"cloudflareTunnelsAnalyticsAdaptiveGroups": []map[string]interface{}{
						{"count": 10, "dimensions": map[string]string{"datetimeMinute": "bad", "tunnelID": "t1", "tunnelName": "test"}},
					},
				},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write(makeGraphQLResponse(data))
		} else {
			w.Write(makeRESTResponse(tunnelRESTResponseData()))
		}
	}))
	defer server.Close()
	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	c.Collect(context.Background())
}

func TestTunnelCollector_PromBadDimKey(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write(makeGraphQLResponse(tunnelGraphQLResponseData()))
		} else {
			w.Write(makeRESTResponse(tunnelRESTResponseData()))
		}
	}))
	defer server.Close()
	client := createTestClient(server)
	NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	ts.store.Add("cloudflare_tunnel_requests_total", "bad", time.Now(), 1)
	ts.registry.Gather()
}
