// Package collector defines the interface that all metric collectors implement.
package collector

import "github.com/prometheus/client_golang/prometheus"

// Collector extends prometheus.Collector with a Name method for logging
// and registration diagnostics.
type Collector interface {
	prometheus.Collector
	// Name returns a short, unique identifier for this collector (e.g. "arp", "ipsec").
	Name() string
}
