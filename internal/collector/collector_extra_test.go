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

// TestDNSFirewallCollector_Collect_MultipleAccounts covers multiple-account
// iteration in dns_firewall collectAccount.
func TestDNSFirewallCollector_Collect_MultipleAccounts(t *testing.T) {
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(map[string]interface{}{
			"viewer": map[string]interface{}{
				"accounts": []map[string]interface{}{
					{
						"dnsFirewallAnalyticsAdaptiveGroups": []map[string]interface{}{
							{
								"count": 100,
								"dimensions": map[string]string{
									"clusterID":      "c1",
									"responseCode":   "NOERROR",
									"cacheStatus":    "HIT",
									"datetimeMinute": ts,
								},
							},
						},
					},
				},
			},
		}))
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDNSFirewallCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1", "acc2"}, nil, setup.registry)

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Both accounts should have data
	dims1 := store.MakeDimensionKey("account_id", "acc1", "cluster_id", "c1", "response_code", "NOERROR", "cache_status", "HIT")
	dims2 := store.MakeDimensionKey("account_id", "acc2", "cluster_id", "c1", "response_code", "NOERROR", "cache_status", "HIT")
	if setup.store.Get("cloudflare_dns_firewall_queries_total", dims1) != 100 {
		t.Fatal("missing data for acc1")
	}
	if setup.store.Get("cloudflare_dns_firewall_queries_total", dims2) != 100 {
		t.Fatal("missing data for acc2")
	}
}

// TestDNSFirewallCollector_Collect_GraphQLResponseErrors covers the case
// where GraphQL returns errors in the response body (not HTTP error).
func TestDNSFirewallCollector_Collect_GraphQLResponseErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data":   map[string]interface{}{},
			"errors": []map[string]string{{"message": "first"}, {"message": "second"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDNSFirewallCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, nil, setup.registry)

	err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestCertificateCollector_Collect_RESTParseError covers the case where
// REST response is valid HTTP but invalid JSON.
func TestCertificateCollector_Collect_RESTParseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewCertificateCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, zones, setup.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestDomainCollector_Collect_RESTParseError covers invalid JSON from REST.
func TestDomainCollector_Collect_RESTParseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDomainCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, nil, setup.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestTunnelCollector_Collect_RESTParseError covers invalid JSON from
// tunnel REST inventory.
func TestTunnelCollector_Collect_RESTParseError(t *testing.T) {
	callCount := 0
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodPost {
			w.Write(makeGraphQLResponse(map[string]interface{}{
				"viewer": map[string]interface{}{
					"accounts": []map[string]interface{}{
						{
							"cloudflareTunnelsAnalyticsAdaptiveGroups": []map[string]interface{}{
								{"count": 1, "dimensions": map[string]string{"datetimeMinute": ts, "tunnelID": "t1", "tunnelName": "n1"}},
							},
						},
					},
				},
			}))
		} else {
			w.Write([]byte("not json"))
		}
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewTunnelCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, setup.registry)

	// Inventory parse failure is non-fatal
	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("inventory parse failure should be non-fatal: %v", err)
	}
}

// TestAccessCollector_Collect_InvalidResponseJSON covers the case where the
// GraphQL response data cannot be unmarshaled into the expected struct.
func TestAccessCollector_Collect_InvalidResponseJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Valid GraphQL response but with invalid data structure
		resp := map[string]interface{}{
			"data":   "not an object",
			"errors": []interface{}{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewAccessCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, setup.registry)

	err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid response JSON")
	}
}

// TestGatewayDNSCollector_Collect_InvalidResponseJSON covers unmarshal error.
func TestGatewayDNSCollector_Collect_InvalidResponseJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"data": "bad", "errors": []interface{}{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayDNSCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, setup.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

// TestBrowserIsolationCollector_Collect_InvalidResponseJSON covers unmarshal error.
func TestBrowserIsolationCollector_Collect_InvalidResponseJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"data": "bad", "errors": []interface{}{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewBrowserIsolationCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, setup.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

// TestGatewayNetworkCollector_Collect_InvalidSessionsJSON covers unmarshal error.
func TestGatewayNetworkCollector_Collect_InvalidSessionsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"data": "bad", "errors": []interface{}{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, setup.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

// TestTunnelCollector_Collect_InvalidGraphQLJSON covers unmarshal error.
func TestTunnelCollector_Collect_InvalidGraphQLJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"data": "bad", "errors": []interface{}{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewTunnelCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, setup.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

// TestDNSCollector_Collect_InvalidResponseJSON covers unmarshal error.
func TestDNSCollector_Collect_InvalidResponseJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"data": "bad", "errors": []interface{}{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewDNSCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, nil, zones, setup.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

// TestDNSFirewallCollector_Collect_InvalidResponseJSON covers unmarshal error.
func TestDNSFirewallCollector_Collect_InvalidResponseJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"data": "bad", "errors": []interface{}{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDNSFirewallCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, nil, setup.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

// TestGatewayNetworkCollector_Collect_InvalidBytesJSON covers bytes query unmarshal error.
func TestGatewayNetworkCollector_Collect_InvalidBytesJSON(t *testing.T) {
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// Valid sessions response
			w.Write(makeGraphQLResponse(map[string]interface{}{
				"viewer": map[string]interface{}{
					"accounts": []map[string]interface{}{
						{
							"gatewayL4SessionsAdaptiveGroups": []map[string]interface{}{
								{"count": 1, "dimensions": map[string]string{"datetimeMinute": ts, "action": "allow", "protocol": "tcp"}},
							},
						},
					},
				},
			}))
		} else {
			// Invalid bytes response
			resp := map[string]interface{}{"data": "bad", "errors": []interface{}{}}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	setup := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, setup.store, setup.selfMetrics, setup.logger, 300, 60, 60, []string{"acc1"}, setup.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error for invalid bytes JSON")
	}
}
