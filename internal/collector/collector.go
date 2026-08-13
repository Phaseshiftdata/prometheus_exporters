// Package collector defines the collector interface and common types.
package collector

import (
	"context"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/governor"
)

// Priority classes for collection scheduling and budget shedding.
const (
	PriorityCritical   = governor.PriorityCritical   // Access, Gateway policy outcomes
	PriorityStandard   = governor.PriorityStandard   // DNS, tunnels, isolation
	PriorityBackground = governor.PriorityBackground // registrar, certificates, inventory
)

// Scope identifies whether a dataset is account-scoped or zone-scoped.
type Scope string

const (
	ScopeAccount Scope = "account"
	ScopeZone    Scope = "zone"
)

// Collector is the interface that all metric collectors must implement.
type Collector interface {
	// Name returns the unique name of this collector.
	Name() string

	// Priority returns the priority class for budget shedding.
	Priority() governor.PriorityClass

	// Interval returns the desired collection interval.
	Interval() time.Duration

	// RequiredDatasets returns the dataset names this collector needs.
	RequiredDatasets() []string

	// Scope returns whether this collector is account-scoped or zone-scoped.
	Scope() Scope

	// Collect executes a collection run. It should write results into the
	// aggregation store and return any error encountered.
	Collect(ctx context.Context) error

	// Describe returns a human-readable description of what this collector does.
	Describe() string
}

// APISurface re-exports for convenience.
const (
	APIGraphQL = governor.GraphQL
	APIREST    = governor.REST
)

// TimeWindow represents a query window aligned to minute boundaries.
type TimeWindow struct {
	Start time.Time
	End   time.Time
}

// CalculateWindow computes the query window:
// [now - delay - window, now - delay) aligned to minute boundaries.
func CalculateWindow(now time.Time, delaySeconds, windowSeconds int) TimeWindow {
	delay := time.Duration(delaySeconds) * time.Second
	window := time.Duration(windowSeconds) * time.Second

	end := now.Add(-delay).Truncate(time.Minute)
	start := end.Add(-window)

	return TimeWindow{Start: start, End: end}
}
