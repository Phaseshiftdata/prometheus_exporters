package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/store"
	"github.com/prometheus/client_golang/prometheus"
)

func accessResponseData() interface{} {
	ts := time.Now().Add(-6 * time.Minute).Truncate(time.Minute).Format(time.RFC3339)
	return map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"accessLoginRequestsAdaptiveGroups": []map[string]interface{}{
						{
							"count": 10,
							"dimensions": map[string]string{
								"datetimeMinute":   ts,
								"appID":            "app1",
								"identityProvider": "google",
								"result":           "success",
							},
						},
						{
							"count": 5,
							"dimensions": map[string]string{
								"datetimeMinute":   ts,
								"appID":            "app1",
								"identityProvider": "google",
								"result":           "fail",
							},
						},
					},
				},
			},
		},
	}
}

func TestAccessCollector_Collect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(accessResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)

	c, err := NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	all := ts.store.GetAll("cloudflare_access_login_requests_total")
	if len(all) != 2 {
		t.Fatalf("expected 2 dimension keys, got %d", len(all))
	}

	dims := store.MakeDimensionKey("account_id", "acc1", "app_id", "app1", "identity_provider", "google", "result", "success")
	if v := ts.store.Get("cloudflare_access_login_requests_total", dims); v != 10 {
		t.Fatalf("expected 10, got %f", v)
	}
}

func TestAccessCollector_Deduplication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(accessResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)

	c, _ := NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	c.Collect(context.Background())
	c.Collect(context.Background())

	dims := store.MakeDimensionKey("account_id", "acc1", "app_id", "app1", "identity_provider", "google", "result", "success")
	v := ts.store.Get("cloudflare_access_login_requests_total", dims)
	if v != 10 {
		t.Fatalf("expected 10 (no double count), got %f", v)
	}
}

func TestAccessCollector_PrometheusEmission(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(accessResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)

	NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	now := time.Now()
	dims := store.MakeDimensionKey("account_id", "acc1", "app_id", "app1", "identity_provider", "google", "result", "success")
	ts.store.Add("cloudflare_access_login_requests_total", dims, now, 42)

	families, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "cloudflare_access_login_requests_total" {
			found = true
		}
	}
	if !found {
		t.Error("access metric not found in gathered output")
	}
}

func TestAccessCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(accessResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)

	c, _ := NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if c.Name() != "access" {
		t.Fatalf("expected access, got %q", c.Name())
	}
	if c.Scope() != ScopeAccount {
		t.Fatal("expected account scope")
	}
	if c.Interval() != 60*time.Second {
		t.Fatal("expected 60s interval")
	}
	if len(c.RequiredDatasets()) != 1 {
		t.Fatal("expected 1 required dataset")
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestAccessCollector_GraphQLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`error`))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAccessCollector_EmptyResponse(t *testing.T) {
	data := map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{"accessLoginRequestsAdaptiveGroups": []map[string]interface{}{}},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(data))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("expected no error for empty response, got: %v", err)
	}
}

func TestAccessCollector_BadTimestamp(t *testing.T) {
	data := map[string]interface{}{
		"viewer": map[string]interface{}{
			"accounts": []map[string]interface{}{
				{
					"accessLoginRequestsAdaptiveGroups": []map[string]interface{}{
						{
							"count": 10,
							"dimensions": map[string]string{
								"datetimeMinute":   "not-a-date",
								"appID":            "app1",
								"identityProvider": "google",
								"result":           "success",
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
	c, _ := NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	// Should not fail, just skip the bad row
	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	all := ts.store.GetAll("cloudflare_access_login_requests_total")
	if len(all) != 0 {
		t.Fatalf("expected 0 entries (bad timestamp skipped), got %d", len(all))
	}
}

func TestAccessCollector_GraphQLResponseErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data":   map[string]interface{}{},
			"errors": []map[string]string{{"message": "some error"}},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error for GraphQL errors")
	}
}

func TestAccessCollector_PromCollect_BadDimKey(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(accessResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	// Add a malformed dimension key (wrong number of pairs)
	ts.store.Add("cloudflare_access_login_requests_total", "bad\x00key", time.Now(), 1)

	// Should not panic during Gather - malformed keys are skipped
	_, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather should not fail: %v", err)
	}
}

func TestAccessCollector_RegisterNewRegistry(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(accessResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)

	_, err := NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	reg2 := prometheus.NewRegistry()
	_, err = NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, reg2)
	if err != nil {
		t.Fatalf("second registration with new registry failed: %v", err)
	}
}
