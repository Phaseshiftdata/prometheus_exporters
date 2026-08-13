package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/phaseshiftdata/prometheus_exporters/internal/collector"
	"github.com/phaseshiftdata/prometheus_exporters/internal/governor"
)

// mockCollector implements collector.Collector for testing.
type mockCollector struct {
	name             string
	priority         governor.PriorityClass
	interval         time.Duration
	requiredDatasets []string
	scope            collector.Scope
	collectErr       error
	collectCount     atomic.Int64
}

func (m *mockCollector) Name() string                       { return m.name }
func (m *mockCollector) Priority() governor.PriorityClass   { return m.priority }
func (m *mockCollector) Interval() time.Duration            { return m.interval }
func (m *mockCollector) RequiredDatasets() []string          { return m.requiredDatasets }
func (m *mockCollector) Scope() collector.Scope             { return m.scope }
func (m *mockCollector) Describe() string                   { return "mock collector" }
func (m *mockCollector) Collect(ctx context.Context) error {
	m.collectCount.Add(1)
	return m.collectErr
}

func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	gov := governor.NewGovernor(1000, 1000)
	return NewScheduler(gov, logger)
}

func TestRegister(t *testing.T) {
	s := newTestScheduler(t)
	mc := &mockCollector{name: "test", interval: time.Second, scope: collector.ScopeAccount}

	s.Register(mc)

	if s.CollectorCount() != 1 {
		t.Fatalf("expected 1 collector, got %d", s.CollectorCount())
	}

	names := s.RegisteredCollectors()
	if len(names) != 1 || names[0] != "test" {
		t.Fatalf("expected [test], got %v", names)
	}
}

func TestDeregister(t *testing.T) {
	s := newTestScheduler(t)
	mc := &mockCollector{name: "test", interval: time.Second, scope: collector.ScopeAccount}

	s.Register(mc)
	s.Deregister("test")

	if s.CollectorCount() != 0 {
		t.Fatalf("expected 0 collectors, got %d", s.CollectorCount())
	}
}

func TestDeregister_WhileRunning(t *testing.T) {
	s := newTestScheduler(t)
	mc := &mockCollector{
		name:     "running",
		interval: 50 * time.Millisecond,
		scope:    collector.ScopeAccount,
	}
	s.Register(mc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	// Deregister while running
	s.Deregister("running")

	if s.CollectorCount() != 0 {
		t.Fatalf("expected 0 after deregister, got %d", s.CollectorCount())
	}

	s.Stop()
}

func TestDeregister_Nonexistent(t *testing.T) {
	s := newTestScheduler(t)
	// Should not panic
	s.Deregister("nonexistent")
}

func TestCollectorCount(t *testing.T) {
	s := newTestScheduler(t)
	if s.CollectorCount() != 0 {
		t.Fatalf("expected 0, got %d", s.CollectorCount())
	}

	s.Register(&mockCollector{name: "a", interval: time.Second, scope: collector.ScopeAccount})
	s.Register(&mockCollector{name: "b", interval: time.Second, scope: collector.ScopeZone})

	if s.CollectorCount() != 2 {
		t.Fatalf("expected 2, got %d", s.CollectorCount())
	}
}

func TestStartStop(t *testing.T) {
	s := newTestScheduler(t)
	mc := &mockCollector{
		name:     "fast",
		interval: 50 * time.Millisecond,
		scope:    collector.ScopeAccount,
	}

	s.Register(mc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)

	// Wait for at least one collection
	time.Sleep(100 * time.Millisecond)

	s.Stop()

	count := mc.collectCount.Load()
	if count < 1 {
		t.Fatalf("expected at least 1 collection, got %d", count)
	}
}

func TestStartCollector(t *testing.T) {
	s := newTestScheduler(t)
	mc := &mockCollector{
		name:     "dynamic",
		interval: 50 * time.Millisecond,
		scope:    collector.ScopeAccount,
	}

	s.Register(mc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.StartCollector(ctx, "dynamic")
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if mc.collectCount.Load() < 1 {
		t.Fatal("expected at least 1 collection")
	}
}

func TestStartCollector_Nonexistent(t *testing.T) {
	s := newTestScheduler(t)
	ctx := context.Background()
	// Should not panic
	s.StartCollector(ctx, "nonexistent")
}

func TestScheduler_BudgetShedding(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	// Very small budget: 0 remaining
	gov := governor.NewGovernor(1, 1)
	now := time.Now()
	gov.Record(governor.GraphQL, 1)
	_ = now

	s := NewScheduler(gov, logger)

	var shedCount atomic.Int64
	s.OnCollectionShed = func(name string) {
		shedCount.Add(1)
	}

	mc := &mockCollector{
		name:     "sheddable",
		interval: 50 * time.Millisecond,
		scope:    collector.ScopeAccount,
	}
	s.Register(mc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if shedCount.Load() < 1 {
		t.Fatal("expected at least 1 shed event")
	}
}

func TestScheduler_Callbacks(t *testing.T) {
	s := newTestScheduler(t)

	var startCount, completeCount atomic.Int64
	s.OnCollectionStart = func(name string) {
		startCount.Add(1)
	}
	s.OnCollectionComplete = func(name string, duration time.Duration, err error) {
		completeCount.Add(1)
	}

	mc := &mockCollector{
		name:     "cb",
		interval: 50 * time.Millisecond,
		scope:    collector.ScopeAccount,
	}
	s.Register(mc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if startCount.Load() < 1 || completeCount.Load() < 1 {
		t.Fatalf("expected callbacks to fire, starts=%d, completes=%d",
			startCount.Load(), completeCount.Load())
	}
}

func TestScheduler_RESTCollector(t *testing.T) {
	s := newTestScheduler(t)
	mc := &mockCollector{
		name:             "rest_collector",
		interval:         50 * time.Millisecond,
		scope:            collector.ScopeAccount,
		requiredDatasets: []string{"domains"},
	}
	s.Register(mc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if mc.collectCount.Load() < 1 {
		t.Fatal("expected at least 1 collection for REST collector")
	}
}

func TestScheduler_CollectorReturnsError(t *testing.T) {
	s := newTestScheduler(t)

	var completeCount atomic.Int64
	var lastErr error
	s.OnCollectionComplete = func(name string, duration time.Duration, err error) {
		completeCount.Add(1)
		lastErr = err
	}

	mc := &mockCollector{
		name:       "failing",
		interval:   50 * time.Millisecond,
		scope:      collector.ScopeAccount,
		collectErr: context.DeadlineExceeded,
	}
	s.Register(mc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if completeCount.Load() < 1 {
		t.Fatal("expected callback to fire")
	}
	if lastErr == nil {
		t.Fatal("expected error to be passed to callback")
	}
}

func TestScheduler_ZoneScopedCollector(t *testing.T) {
	s := newTestScheduler(t)
	mc := &mockCollector{
		name:     "zone_collector",
		interval: 50 * time.Millisecond,
		scope:    collector.ScopeZone,
	}
	s.Register(mc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if mc.collectCount.Load() < 1 {
		t.Fatal("expected at least 1 collection for zone-scoped collector")
	}
}

func TestIsRESTDataset(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"domains", true},
		{"certificates", true},
		{"zones", true},
		{"tunnels_inventory", true},
		{"dnsAnalyticsAdaptiveGroups", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isRESTDataset(tt.name); got != tt.want {
			t.Errorf("isRESTDataset(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
