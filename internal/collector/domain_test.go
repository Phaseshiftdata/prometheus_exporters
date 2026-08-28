package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func domainRESTResponseData() interface{} {
	return []map[string]interface{}{
		{
			"name":       "example.com",
			"expires_at": "2025-12-31T00:00:00Z",
			"auto_renew": true,
			"locked":     true,
		},
		{
			"name":       "example.org",
			"expires_at": "2024-06-15T00:00:00Z",
			"auto_renew": false,
			"locked":     false,
		},
	}
}

func TestDomainCollector_Collect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeRESTResponse(domainRESTResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	c, err := NewDomainCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(c.gauges) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(c.gauges))
	}
}

func TestDomainCollector_PrometheusEmission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeRESTResponse(domainRESTResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDomainCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)
	c.Collect(context.Background())

	families, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	foundExpiration := false
	foundAutoRenew := false
	foundLocked := false
	for _, f := range families {
		switch f.GetName() {
		case "cloudflare_domain_expiration_timestamp_seconds":
			foundExpiration = true
		case "cloudflare_domain_auto_renew":
			foundAutoRenew = true
		case "cloudflare_domain_locked":
			foundLocked = true
		}
	}
	if !foundExpiration {
		t.Error("expiration metric not found")
	}
	if !foundAutoRenew {
		t.Error("auto_renew metric not found")
	}
	if !foundLocked {
		t.Error("locked metric not found")
	}
}

func TestDomainCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeRESTResponse(domainRESTResponseData()))
	}))
	defer server.Close()
	client := createTestClient(server)
	c, _ := NewDomainCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)

	if c.Name() != "domain" {
		t.Fatalf("expected domain, got %q", c.Name())
	}
	if c.Scope() != ScopeAccount {
		t.Fatal("expected account scope")
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
	datasets := c.RequiredDatasets()
	if len(datasets) != 1 || datasets[0] != "domains" {
		t.Fatalf("expected [domains], got %v", datasets)
	}
}

func TestDomainCollector_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDomainCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainCollector_UnmarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"errors":[],"messages":[],"result":"not-an-array"}`))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDomainCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)

	if err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestDomainCollector_AutoRenewValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeRESTResponse(domainRESTResponseData()))
	}))
	defer server.Close()

	ts := newTestSetup(t)
	client := createTestClient(server)
	c, _ := NewDomainCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, ts.registry)
	c.Collect(context.Background())

	// Check the gauge values
	for _, g := range c.gauges {
		if g.domain == "example.com" {
			if g.autoRenew != 1 {
				t.Error("expected autoRenew=1 for example.com")
			}
			if g.locked != 1 {
				t.Error("expected locked=1 for example.com")
			}
			if g.expiration <= 0 {
				t.Error("expected positive expiration timestamp")
			}
		}
		if g.domain == "example.org" {
			if g.autoRenew != 0 {
				t.Error("expected autoRenew=0 for example.org")
			}
			if g.locked != 0 {
				t.Error("expected locked=0 for example.org")
			}
		}
	}
}
