package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/store"
)

func gatewayNetworkResponseData() interface{} {
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	return map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"gatewayL4SessionsAdaptiveGroups": []map[string]interface{}{
						{
							"count": 50,
							"dimensions": map[string]string{
								"datetimeMinute": ts,
								"action":         "allow",
								"protocol":       "tcp",
							},
						},
					},
				},
			},
		},
	}
}

func gatewayNetworkBytesResponseData() interface{} {
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	return map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"gatewayL4DownstreamSessionsAdaptiveGroups": []map[string]interface{}{
						{
							"sum": map[string]int64{"bytesReceived": 1024},
							"dimensions": map[string]string{
								"datetimeMinute": ts,
							},
						},
					},
					"gatewayL4UpstreamSessionsAdaptiveGroups": []map[string]interface{}{
						{
							"sum": map[string]int64{"bytesSent": 2048},
							"dimensions": map[string]string{
								"datetimeMinute": ts,
							},
						},
					},
				},
			},
		},
	}
}

func TestGatewayNetworkCollector_Collect(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Write(makeGraphQLResponse(gatewayNetworkResponseData()))
		} else {
			w.Write(makeGraphQLResponse(gatewayNetworkBytesResponseData()))
		}
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, err := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	sessions := ts.store.GetAll("cloudflare_gateway_network_sessions_total")
	if len(sessions) == 0 {
		t.Fatal("expected session data in store")
	}

	dims := store.MakeDimensionKey("account_id", "acc1", "action", "allow", "protocol", "tcp")
	if v := ts.store.Get("cloudflare_gateway_network_sessions_total", dims); v != 50 {
		t.Fatalf("expected 50 sessions, got %f", v)
	}

	bytes := ts.store.GetAll("cloudflare_gateway_network_bytes_total")
	if len(bytes) == 0 {
		t.Fatal("expected bytes data in store")
	}
}

func TestGatewayNetworkCollector_Deduplication(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount%2 == 1 {
			w.Write(makeGraphQLResponse(gatewayNetworkResponseData()))
		} else {
			w.Write(makeGraphQLResponse(gatewayNetworkBytesResponseData()))
		}
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	c.Collect(context.Background())
	c.Collect(context.Background())

	dims := store.MakeDimensionKey("account_id", "acc1", "action", "allow", "protocol", "tcp")
	if v := ts.store.Get("cloudflare_gateway_network_sessions_total", dims); v != 50 {
		t.Fatalf("expected 50 (no double count), got %f", v)
	}
}

func TestGatewayNetworkCollector_PrometheusEmission(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(gatewayNetworkResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)

	NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	dims := store.MakeDimensionKey("account_id", "acc1", "action", "allow", "protocol", "tcp")
	ts.store.Add("cloudflare_gateway_network_sessions_total", dims, time.Now(), 50)
	bytesDims := store.MakeDimensionKey("account_id", "acc1", "direction", "downstream")
	ts.store.Add("cloudflare_gateway_network_bytes_total", bytesDims, time.Now(), 1024)

	families, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	foundSessions := false
	foundBytes := false
	for _, f := range families {
		switch f.GetName() {
		case "cloudflare_gateway_network_sessions_total":
			foundSessions = true
		case "cloudflare_gateway_network_bytes_total":
			foundBytes = true
		}
	}
	if !foundSessions {
		t.Error("sessions metric not found")
	}
	if !foundBytes {
		t.Error("bytes metric not found")
	}
}

func TestGatewayNetworkCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(gatewayNetworkResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if c.Name() != "gateway_network" {
		t.Fatalf("expected gateway_network, got %q", c.Name())
	}
	if c.Scope() != ScopeAccount {
		t.Fatal("expected account scope")
	}
	if len(c.RequiredDatasets()) != 3 {
		t.Fatalf("expected 3 required datasets, got %d", len(c.RequiredDatasets()))
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestGatewayNetworkCollector_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestGatewayNetworkCollector_BadTimestampSessions(t *testing.T) {
	data := map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"gatewayL4SessionsAdaptiveGroups": []map[string]interface{}{
						{"count": 10, "dimensions": map[string]string{"datetimeMinute": "bad", "action": "allow", "protocol": "tcp"}},
					},
				},
			},
		},
	}
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Write(makeGraphQLResponse(data))
		} else {
			w.Write(makeGraphQLResponse(gatewayNetworkBytesResponseData()))
		}
	}))
	defer server.Close()
	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	c.Collect(context.Background())
}

func TestGatewayNetworkCollector_BadTimestampBytes(t *testing.T) {
	data := map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"gatewayL4DownstreamSessionsAdaptiveGroups": []map[string]interface{}{
						{"sum": map[string]int64{"bytesReceived": 1024}, "dimensions": map[string]string{"datetimeMinute": "bad"}},
					},
					"gatewayL4UpstreamSessionsAdaptiveGroups": []map[string]interface{}{
						{"sum": map[string]int64{"bytesSent": 2048}, "dimensions": map[string]string{"datetimeMinute": "bad"}},
					},
				},
			},
		},
	}
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Write(makeGraphQLResponse(gatewayNetworkResponseData()))
		} else {
			w.Write(makeGraphQLResponse(data))
		}
	}))
	defer server.Close()
	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	c.Collect(context.Background())
}

func TestGatewayNetworkCollector_PromBadDimKey(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(gatewayNetworkResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	ts.store.Add("cloudflare_gateway_network_sessions_total", "bad", time.Now(), 1)
	ts.store.Add("cloudflare_gateway_network_bytes_total", "bad", time.Now(), 1)
	ts.registry.Gather()
}

func TestGatewayNetworkCollector_EmptyData(t *testing.T) {
	// Return empty data sets for both sessions and bytes queries.
	// This covers the maxDataTime.IsZero() branch.
	emptyData := map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"gatewayL4SessionsAdaptiveGroups":           []interface{}{},
					"gatewayL4DownstreamSessionsAdaptiveGroups": []interface{}{},
					"gatewayL4UpstreamSessionsAdaptiveGroups":   []interface{}{},
				},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(emptyData))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("expected no error for empty data, got: %v", err)
	}
}

func TestGatewayNetworkCollector_MultipleUpstreamTimestamps(t *testing.T) {
	// The upstream data has a NEWER timestamp than downstream, so the
	// t.After(maxTime) true branch is exercised in the upstream loop.
	// A second upstream row with an older timestamp ensures the false
	// branch is also exercised.
	tsOlder := time.Now().Add(-10 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	tsMiddle := time.Now().Add(-7 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	tsNewest := time.Now().Add(-5 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)

	bytesData := map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"gatewayL4DownstreamSessionsAdaptiveGroups": []map[string]interface{}{
						{
							"sum":        map[string]int64{"bytesReceived": 512},
							"dimensions": map[string]string{"datetimeMinute": tsOlder},
						},
					},
					"gatewayL4UpstreamSessionsAdaptiveGroups": []map[string]interface{}{
						{
							"sum":        map[string]int64{"bytesSent": 1024},
							"dimensions": map[string]string{"datetimeMinute": tsNewest},
						},
						{
							"sum":        map[string]int64{"bytesSent": 2048},
							"dimensions": map[string]string{"datetimeMinute": tsMiddle},
						},
					},
				},
			},
		},
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Write(makeGraphQLResponse(gatewayNetworkResponseData()))
		} else {
			w.Write(makeGraphQLResponse(bytesData))
		}
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestGatewayNetworkCollector_SessionsGraphQLError(t *testing.T) {
	// Test that GraphQL errors in the sessions query are propagated
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLErrorResponse("not entitled"))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error for GraphQL error response")
	}
}

func TestGatewayNetworkCollector_BytesGraphQLError(t *testing.T) {
	// First call (sessions) succeeds, second call (bytes) returns GraphQL error
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Write(makeGraphQLResponse(gatewayNetworkResponseData()))
		} else {
			w.Write(makeGraphQLErrorResponse("rate limited"))
		}
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error for bytes GraphQL error")
	}
}

func TestGatewayNetworkCollector_BytesError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Write(makeGraphQLResponse(gatewayNetworkResponseData()))
		} else {
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error when bytes query fails")
	}
}
