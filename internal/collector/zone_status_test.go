package collector

import (
	"context"
	"testing"
	"time"

	"github.com/asymmetric-effort/prometheus-exporters/internal/cloudflare"
)

func TestZoneStatusCollector_Collect(t *testing.T) {
	ts := newTestSetup(t)
	client := cloudflare.NewClient("token", 5*time.Second)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	zoneStatus := []ZoneStatusInfo{
		{ID: "z1", Name: "example.com", Status: "active"},
	}

	c, err := NewZoneStatusCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, zoneStatus, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if err := c.Collect(context.Background()); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
}

func TestZoneStatusCollector_PrometheusEmission(t *testing.T) {
	ts := newTestSetup(t)
	client := cloudflare.NewClient("token", 5*time.Second)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}, {ID: "z2", Name: "example.org"}}
	zoneStatus := []ZoneStatusInfo{
		{ID: "z1", Name: "example.com", Status: "active"},
		{ID: "z2", Name: "example.org", Status: "pending"},
	}

	NewZoneStatusCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, zoneStatus, ts.registry)

	families, err := ts.registry.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "cloudflare_zone_status" {
			found = true
			if len(f.GetMetric()) != 2 {
				t.Fatalf("expected 2 zone status metrics, got %d", len(f.GetMetric()))
			}
		}
	}
	if !found {
		t.Error("zone_status metric not found")
	}
}

func TestZoneStatusCollector_NilZoneStatus(t *testing.T) {
	ts := newTestSetup(t)
	client := cloudflare.NewClient("token", 5*time.Second)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	c, err := NewZoneStatusCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, nil, ts.registry)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	// When zoneStatus is nil, zones should be used with "unknown" status
	if len(c.zoneStatus) != 1 {
		t.Fatalf("expected 1 zone status entry, got %d", len(c.zoneStatus))
	}
	if c.zoneStatus[0].Status != "unknown" {
		t.Fatalf("expected unknown status, got %q", c.zoneStatus[0].Status)
	}
}

func TestZoneStatusCollector_UpdateZoneStatus(t *testing.T) {
	ts := newTestSetup(t)
	client := cloudflare.NewClient("token", 5*time.Second)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	c, _ := NewZoneStatusCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, nil, ts.registry)

	newStatus := []ZoneStatusInfo{
		{ID: "z1", Name: "example.com", Status: "active"},
		{ID: "z2", Name: "example.org", Status: "pending"},
	}
	c.UpdateZoneStatus(newStatus)

	if len(c.zoneStatus) != 2 {
		t.Fatalf("expected 2 zone status entries, got %d", len(c.zoneStatus))
	}
}

func TestZoneStatusCollector_Interface(t *testing.T) {
	ts := newTestSetup(t)
	client := cloudflare.NewClient("token", 5*time.Second)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}
	c, _ := NewZoneStatusCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, nil, ts.registry)

	if c.Name() != "zone_status" {
		t.Fatalf("expected zone_status, got %q", c.Name())
	}
	if c.Scope() != ScopeZone {
		t.Fatal("expected zone scope")
	}
	if len(c.RequiredDatasets()) != 0 {
		t.Fatal("expected no required datasets")
	}
	if c.Describe() == "" {
		t.Fatal("expected non-empty description")
	}
}
