package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func certificateRESTResponseData() interface{} {
	return []map[string]interface{}{
		{
			"id":   "pack1",
			"type": "universal",
			"certificates": []map[string]interface{}{
				{
					"issuer":     "DigiCert",
					"expires_on": "2025-06-15T00:00:00Z",
				},
			},
		},
		{
			"id":   "pack2",
			"type": "advanced",
			"certificates": []map[string]interface{}{
				{
					"issuer":     "Lets Encrypt",
					"expires_on": "2024-12-01T00:00:00Z",
				},
				{
					"issuer":     "Google Trust Services",
					"expires_on": "2025-03-01T00:00:00Z",
				},
			},
		},
	}
}

func TestCertificateCollector_Collect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeRESTResponse(certificateRESTResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	c, err := NewCertificateCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(c.gauges) != 3 {
		t.Fatalf("expected 3 certificates, got %d", len(c.gauges))
	}
}

func TestCertificateCollector_PrometheusEmission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeRESTResponse(certificateRESTResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewCertificateCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	c.Collect(context.Background())

	families, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "cloudflare_certificate_expiration_timestamp_seconds" {
			found = true
			if len(f.GetMetric()) != 3 {
				t.Fatalf("expected 3 cert metrics, got %d", len(f.GetMetric()))
			}
		}
	}
	if !found {
		t.Error("certificate metric not found")
	}
}

func TestCertificateCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeRESTResponse(certificateRESTResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewCertificateCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if c.Name() != "certificate" {
		t.Fatalf("expected certificate, got %q", c.Name())
	}
	if c.Scope() != ScopeZone {
		t.Fatal("expected zone scope")
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestCertificateCollector_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewCertificateCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertificateCollector_UnmarshalError(t *testing.T) {
	// Return a REST response where "result" is a JSON string instead of an
	// array, triggering the json.Unmarshal error path in fetchCertificatePacks.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"errors":[],"messages":[],"result":"not-an-array"}`))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewCertificateCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestCertificateCollector_EmptyPacks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeRESTResponse([]interface{}{}))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewCertificateCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(c.gauges) != 0 {
		t.Fatalf("expected 0 gauges, got %d", len(c.gauges))
	}
}
