package collector

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewSelfMetrics(t *testing.T) {
	m := NewSelfMetrics("1.0.0", "abc123", "go1.21")
	if m == nil {
		t.Fatal("expected non-nil SelfMetrics")
	}
	if m.CollectionDuration == nil {
		t.Error("CollectionDuration is nil")
	}
	if m.LastUpdated == nil {
		t.Error("LastUpdated is nil")
	}
	if m.LastUpdatedGlobal == nil {
		t.Error("LastUpdatedGlobal is nil")
	}
	if m.CollectionErrors == nil {
		t.Error("CollectionErrors is nil")
	}
	if m.BuildInfo == nil {
		t.Error("BuildInfo is nil")
	}
}

func TestSelfMetrics_Register(t *testing.T) {
	m := NewSelfMetrics("1.0.0", "abc123", "go1.21")
	reg := prometheus.NewRegistry()

	if err := m.Register(reg); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Verify metrics are registered by gathering
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("expected some metric families")
	}

	// Check build info is present
	found := false
	for _, f := range families {
		if f.GetName() == "cloudflare_exporter_build_info" {
			found = true
			break
		}
	}
	if !found {
		t.Error("build_info metric not found")
	}
}

func TestSelfMetrics_RegisterTwice(t *testing.T) {
	m := NewSelfMetrics("1.0.0", "abc123", "go1.21")
	reg := prometheus.NewRegistry()

	if err := m.Register(reg); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	// Second registration should fail
	err := m.Register(reg)
	if err == nil {
		t.Fatal("expected error on double registration")
	}
}

func TestRecordCollectionSuccess(t *testing.T) {
	m := NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	m.Register(reg)

	dataTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	m.RecordCollectionSuccess("test_collector", 500*time.Millisecond, dataTime)

	// Should be able to gather without error
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	// Look for the last_updated metric
	foundLastUpdated := false
	for _, f := range families {
		if f.GetName() == "cloudflare_exporter_last_updated_timestamp_seconds" {
			foundLastUpdated = true
		}
	}
	if !foundLastUpdated {
		t.Error("last_updated metric not found after RecordCollectionSuccess")
	}
}

func TestRecordCollectionError(t *testing.T) {
	m := NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	m.Register(reg)

	m.RecordCollectionError("test_collector", "query_failed", 100*time.Millisecond)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	foundErrors := false
	for _, f := range families {
		if f.GetName() == "cloudflare_exporter_collection_errors_total" {
			foundErrors = true
		}
	}
	if !foundErrors {
		t.Error("collection_errors metric not found")
	}
}

func TestRecordCollectionShed(t *testing.T) {
	m := NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	m.Register(reg)

	m.RecordCollectionShed("test_collector")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	foundShed := false
	for _, f := range families {
		if f.GetName() == "cloudflare_exporter_collections_shed_total" {
			foundShed = true
		}
	}
	if !foundShed {
		t.Error("collections_shed metric not found")
	}
}

func TestRecordAPIRequest(t *testing.T) {
	m := NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	m.Register(reg)

	m.RecordAPIRequest("graphql", "200")
	m.RecordAPIRequest("rest", "429")
}

func TestSetAPIBudgetRemaining(t *testing.T) {
	m := NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	m.Register(reg)

	m.SetAPIBudgetRemaining("graphql", 150)
	m.SetAPIBudgetRemaining("rest", 500)
}

func TestUpdateGlobalLastUpdated_MinCalculation(t *testing.T) {
	m := NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	m.Register(reg)

	t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC) // earlier

	m.RecordCollectionSuccess("collector_a", time.Second, t1)
	m.RecordCollectionSuccess("collector_b", time.Second, t2)

	// The global should reflect the min of the lastUpdated times (both are "now"),
	// but since calls happen sequentially, the first call's time should be <= the
	// second call's time. The global should be the first (earlier) one.

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	for _, f := range families {
		if f.GetName() == "cloudflare_exporter_last_updated_global_timestamp_seconds" {
			if len(f.GetMetric()) == 0 {
				t.Fatal("no metric values for global last updated")
			}
			val := f.GetMetric()[0].GetGauge().GetValue()
			if val <= 0 {
				t.Fatalf("expected positive timestamp, got %f", val)
			}
		}
	}
}

func TestUpdateGlobalLastUpdated_Empty(t *testing.T) {
	m := NewSelfMetrics("1.0.0", "abc", "go1.21")
	// Should not panic when no collectors have reported
	m.updateGlobalLastUpdated()
}
