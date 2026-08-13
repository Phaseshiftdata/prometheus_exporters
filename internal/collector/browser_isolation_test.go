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

func browserIsolationResponseData() interface{} {
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	return map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"browserIsolationSessionsAdaptiveGroups": []map[string]interface{}{
						{
							"count": 30,
							"dimensions": map[string]string{
								"datetimeMinute": ts,
								"outcome":        "isolated",
							},
						},
						{
							"count": 10,
							"dimensions": map[string]string{
								"datetimeMinute": ts,
								"outcome":        "passed",
							},
						},
					},
				},
			},
		},
	}
}

func TestBrowserIsolationCollector_Collect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(browserIsolationResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, err := NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	all := ts.store.GetAll("cloudflare_browser_isolation_sessions_total")
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}

	dims := store.MakeDimensionKey("account_id", "acc1", "outcome", "isolated")
	if v := ts.store.Get("cloudflare_browser_isolation_sessions_total", dims); v != 30 {
		t.Fatalf("expected 30, got %f", v)
	}
}

func TestBrowserIsolationCollector_Deduplication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(browserIsolationResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	c.Collect(context.Background())
	c.Collect(context.Background())

	dims := store.MakeDimensionKey("account_id", "acc1", "outcome", "isolated")
	if v := ts.store.Get("cloudflare_browser_isolation_sessions_total", dims); v != 30 {
		t.Fatalf("expected 30 (no double count), got %f", v)
	}
}

func TestBrowserIsolationCollector_PrometheusEmission(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(browserIsolationResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)

	NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	dims := store.MakeDimensionKey("account_id", "acc1", "outcome", "isolated")
	ts.store.Add("cloudflare_browser_isolation_sessions_total", dims, time.Now(), 30)

	families, _ := ts.registry.Gather()
	found := false
	for _, f := range families {
		if f.GetName() == "cloudflare_browser_isolation_sessions_total" {
			found = true
		}
	}
	if !found {
		t.Error("browser isolation metric not found")
	}
}

func TestBrowserIsolationCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(browserIsolationResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	c, _ := NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if c.Name() != "browser_isolation" {
		t.Fatalf("expected browser_isolation, got %q", c.Name())
	}
	if c.Scope() != ScopeAccount {
		t.Fatal("expected account scope")
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestBrowserIsolationCollector_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()
	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestBrowserIsolationCollector_BadTimestamp(t *testing.T) {
	data := map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"browserIsolationSessionsAdaptiveGroups": []map[string]interface{}{
						{"count": 10, "dimensions": map[string]string{"datetimeMinute": "bad", "outcome": "isolated"}},
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
	c, _ := NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	c.Collect(context.Background())
	if len(ts.store.GetAll("cloudflare_browser_isolation_sessions_total")) != 0 {
		t.Fatal("expected 0 entries")
	}
}

func TestBrowserIsolationCollector_PromBadDimKey(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(browserIsolationResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	ts.store.Add("cloudflare_browser_isolation_sessions_total", "bad", time.Now(), 1)
	ts.registry.Gather()
}

func TestBrowserIsolationCollector_GraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"data": map[string]interface{}{}, "errors": []map[string]string{{"message": "err"}}}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()
	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
