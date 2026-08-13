// Package scheduler runs collectors on independent tickers with quota governance.
package scheduler

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/phaseshiftdata/prometheus_exporters/internal/collector"
	"github.com/phaseshiftdata/prometheus_exporters/internal/governor"
)

// Scheduler runs collectors on independent tickers.
type Scheduler struct {
	mu         sync.RWMutex
	collectors map[string]collector.Collector
	governor   *governor.Governor
	logger     *zap.Logger
	cancels    map[string]context.CancelFunc
	wg         sync.WaitGroup

	// Callbacks for self-instrumentation
	OnCollectionStart    func(name string)
	OnCollectionComplete func(name string, duration time.Duration, err error)
	OnCollectionShed     func(name string)
}

// NewScheduler creates a new Scheduler.
func NewScheduler(gov *governor.Governor, logger *zap.Logger) *Scheduler {
	return &Scheduler{
		collectors: make(map[string]collector.Collector),
		governor:   gov,
		logger:     logger,
		cancels:    make(map[string]context.CancelFunc),
	}
}

// Register adds a collector to the scheduler.
func (s *Scheduler) Register(c collector.Collector) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.collectors[c.Name()] = c
	s.logger.Info("registered collector",
		zap.String("collector", c.Name()),
		zap.String("scope", string(c.Scope())),
		zap.Duration("interval", c.Interval()),
		zap.Int("priority", int(c.Priority())),
	)
}

// Deregister removes a collector from the scheduler and stops its ticker.
func (s *Scheduler) Deregister(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.cancels[name]; ok {
		cancel()
		delete(s.cancels, name)
	}
	delete(s.collectors, name)
	s.logger.Info("deregistered collector", zap.String("collector", name))
}

// Start begins running all registered collectors on their intervals.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for name, c := range s.collectors {
		collectorCtx, cancel := context.WithCancel(ctx)
		s.cancels[name] = cancel
		s.wg.Add(1)
		go s.runCollector(collectorCtx, c)
	}
}

// StartCollector starts a single collector (used for dynamic registration).
func (s *Scheduler) StartCollector(ctx context.Context, name string) {
	s.mu.RLock()
	c, ok := s.collectors[name]
	s.mu.RUnlock()
	if !ok {
		return
	}

	collectorCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancels[name] = cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go s.runCollector(collectorCtx, c)
}

// Stop cancels all running collectors and waits for them to finish.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	for name, cancel := range s.cancels {
		cancel()
		delete(s.cancels, name)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// RegisteredCollectors returns the names of all registered collectors.
func (s *Scheduler) RegisteredCollectors() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.collectors))
	for name := range s.collectors {
		names = append(names, name)
	}
	return names
}

// CollectorCount returns the number of registered collectors.
func (s *Scheduler) CollectorCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.collectors)
}

func (s *Scheduler) runCollector(ctx context.Context, c collector.Collector) {
	defer s.wg.Done()

	name := c.Name()
	interval := c.Interval()
	priority := c.Priority()

	// Determine API surface cost
	surface := governor.GraphQL
	cost := 1
	if c.Scope() == collector.ScopeZone {
		surface = governor.GraphQL
	}

	// Check if this is a REST-only collector
	for _, ds := range c.RequiredDatasets() {
		if isRESTDataset(ds) {
			surface = governor.REST
		}
	}

	// Run immediately on first tick
	s.executeCollection(ctx, c, name, surface, cost, priority)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.executeCollection(ctx, c, name, surface, cost, priority)
		}
	}
}

func (s *Scheduler) executeCollection(ctx context.Context, c collector.Collector, name string, surface governor.APISurface, cost int, priority governor.PriorityClass) {
	// Check budget
	if !s.governor.Allow(surface, cost, priority) {
		s.logger.Warn("collection shed due to budget exhaustion",
			zap.String("collector", name),
			zap.String("surface", string(surface)),
		)
		if s.OnCollectionShed != nil {
			s.OnCollectionShed(name)
		}
		return
	}

	if s.OnCollectionStart != nil {
		s.OnCollectionStart(name)
	}

	start := time.Now()
	err := c.Collect(ctx)
	duration := time.Since(start)

	if err != nil {
		s.logger.Error("collection failed",
			zap.String("collector", name),
			zap.Duration("duration", duration),
			zap.Error(err),
		)
	} else {
		s.logger.Debug("collection succeeded",
			zap.String("collector", name),
			zap.Duration("duration", duration),
		)
	}

	// Record API usage
	s.governor.Record(surface, cost)

	if s.OnCollectionComplete != nil {
		s.OnCollectionComplete(name, duration, err)
	}
}

func isRESTDataset(name string) bool {
	switch name {
	case "domains", "certificates", "zones", "tunnels_inventory":
		return true
	}
	return false
}
