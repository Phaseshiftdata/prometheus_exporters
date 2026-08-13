package main

import (
	"testing"
	"time"

	"github.com/asymmetric-effort/prometheus-exporters/internal/cloudflare"
	"github.com/asymmetric-effort/prometheus-exporters/internal/collector"
	"github.com/asymmetric-effort/prometheus-exporters/internal/config"
	"github.com/asymmetric-effort/prometheus-exporters/internal/discovery"
	"github.com/asymmetric-effort/prometheus-exporters/internal/governor"
	"github.com/asymmetric-effort/prometheus-exporters/internal/scheduler"
	"github.com/asymmetric-effort/prometheus-exporters/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

func TestRegisterCollectors_AllEnabled(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := cloudflare.NewClient("test-token", 5*time.Second)
	st := store.NewStore(10 * time.Minute)
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	gov := governor.NewGovernor(160, 600)
	sched := scheduler.NewScheduler(gov, logger)

	matrix := discovery.NewCapabilityMatrix()
	matrix.Accounts = []discovery.AccountInfo{
		{ID: "acc1", Name: "Test Account"},
	}
	matrix.Zones = []discovery.ZoneInfo{
		{ID: "z1", Name: "example.com", Plan: "pro", Status: "active"},
	}

	// Mark all datasets as available
	for _, ds := range discovery.KnownDatasets {
		matrix.SetDataset(ds.Name, discovery.DatasetCapability{
			Dataset: ds.Name,
			Scope:   ds.Scope,
			State:   discovery.StateAvailable,
		})
	}

	cfg := &config.Config{
		ScrapeDelay:     300 * time.Second,
		TimeWindow:      60 * time.Second,
		RefreshInterval: 60 * time.Second,
	}

	registerCollectors(cfg, client, st, sm, reg, sched, matrix, logger)

	// Verify some collectors were registered
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("expected registered collector metrics")
	}
}

func TestRegisterCollectors_SubsetEnabled(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := cloudflare.NewClient("test-token", 5*time.Second)
	st := store.NewStore(10 * time.Minute)
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	gov := governor.NewGovernor(160, 600)
	sched := scheduler.NewScheduler(gov, logger)

	matrix := discovery.NewCapabilityMatrix()
	matrix.Accounts = []discovery.AccountInfo{{ID: "acc1", Name: "Test"}}
	matrix.Zones = []discovery.ZoneInfo{{ID: "z1", Name: "example.com", Plan: "pro", Status: "active"}}

	for _, ds := range discovery.KnownDatasets {
		matrix.SetDataset(ds.Name, discovery.DatasetCapability{
			Dataset: ds.Name,
			Scope:   ds.Scope,
			State:   discovery.StateAvailable,
		})
	}

	cfg := &config.Config{
		ScrapeDelay:       300 * time.Second,
		TimeWindow:        60 * time.Second,
		RefreshInterval:   60 * time.Second,
		CollectorsEnabled: []string{"access", "dns"},
	}

	registerCollectors(cfg, client, st, sm, reg, sched, matrix, logger)
}

func TestRegisterCollectors_NoAccounts(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := cloudflare.NewClient("test-token", 5*time.Second)
	st := store.NewStore(10 * time.Minute)
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	gov := governor.NewGovernor(160, 600)
	sched := scheduler.NewScheduler(gov, logger)

	matrix := discovery.NewCapabilityMatrix()

	cfg := &config.Config{
		ScrapeDelay:     300 * time.Second,
		TimeWindow:      60 * time.Second,
		RefreshInterval: 60 * time.Second,
	}

	// Should not panic with no accounts
	registerCollectors(cfg, client, st, sm, reg, sched, matrix, logger)
}

func TestRegisterCollectors_NoZones(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := cloudflare.NewClient("test-token", 5*time.Second)
	st := store.NewStore(10 * time.Minute)
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	gov := governor.NewGovernor(160, 600)
	sched := scheduler.NewScheduler(gov, logger)

	matrix := discovery.NewCapabilityMatrix()
	matrix.Accounts = []discovery.AccountInfo{{ID: "acc1", Name: "Test"}}
	// No zones - zone-scoped collectors should be skipped

	for _, ds := range discovery.KnownDatasets {
		matrix.SetDataset(ds.Name, discovery.DatasetCapability{
			Dataset: ds.Name,
			Scope:   ds.Scope,
			State:   discovery.StateAvailable,
		})
	}

	cfg := &config.Config{
		ScrapeDelay:     300 * time.Second,
		TimeWindow:      60 * time.Second,
		RefreshInterval: 60 * time.Second,
	}

	registerCollectors(cfg, client, st, sm, reg, sched, matrix, logger)
}

func TestRegisterCollectors_DatasetsNotAvailable(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := cloudflare.NewClient("test-token", 5*time.Second)
	st := store.NewStore(10 * time.Minute)
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	gov := governor.NewGovernor(160, 600)
	sched := scheduler.NewScheduler(gov, logger)

	matrix := discovery.NewCapabilityMatrix()
	matrix.Accounts = []discovery.AccountInfo{{ID: "acc1", Name: "Test"}}
	matrix.Zones = []discovery.ZoneInfo{{ID: "z1", Name: "example.com", Plan: "pro", Status: "active"}}

	// Mark all datasets as not entitled
	for _, ds := range discovery.KnownDatasets {
		matrix.SetDataset(ds.Name, discovery.DatasetCapability{
			Dataset: ds.Name,
			Scope:   ds.Scope,
			State:   discovery.StateNotEntitled,
		})
	}

	cfg := &config.Config{
		ScrapeDelay:     300 * time.Second,
		TimeWindow:      60 * time.Second,
		RefreshInterval: 60 * time.Second,
	}

	// Domain and certificate don't require dataset availability
	registerCollectors(cfg, client, st, sm, reg, sched, matrix, logger)
}

func TestRegisterCollectors_DuplicateRegistration(t *testing.T) {
	// Registering collectors twice with the same registry should trigger the
	// error branches (duplicate collector registration).
	logger, _ := zap.NewDevelopment()
	client := cloudflare.NewClient("test-token", 5*time.Second)
	st := store.NewStore(10 * time.Minute)
	sm := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	reg := prometheus.NewRegistry()
	gov := governor.NewGovernor(160, 600)
	sched := scheduler.NewScheduler(gov, logger)

	matrix := discovery.NewCapabilityMatrix()
	matrix.Accounts = []discovery.AccountInfo{
		{ID: "acc1", Name: "Test Account"},
	}
	matrix.Zones = []discovery.ZoneInfo{
		{ID: "z1", Name: "example.com", Plan: "pro", Status: "active"},
	}

	for _, ds := range discovery.KnownDatasets {
		matrix.SetDataset(ds.Name, discovery.DatasetCapability{
			Dataset: ds.Name,
			Scope:   ds.Scope,
			State:   discovery.StateAvailable,
		})
	}

	cfg := &config.Config{
		ScrapeDelay:     300 * time.Second,
		TimeWindow:      60 * time.Second,
		RefreshInterval: 60 * time.Second,
	}

	// First registration succeeds
	registerCollectors(cfg, client, st, sm, reg, sched, matrix, logger)

	// Second registration with the same registry should hit error branches
	// because collectors are already registered.
	sm2 := collector.NewSelfMetrics("1.0.0", "abc", "go1.21")
	sched2 := scheduler.NewScheduler(gov, logger)
	registerCollectors(cfg, client, st, sm2, reg, sched2, matrix, logger)
}
