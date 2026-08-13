// Package store implements an aggregation store for converting Cloudflare
// GraphQL window-based aggregates into monotonically increasing Prometheus
// counters.
//
// GraphQL returns aggregates over a time window. Because Prometheus counters
// must increase monotonically, the store accumulates values across successive
// fetches. To prevent double counting when windows overlap (due to retries,
// clock skew, or delayed runs), every row carries a datetimeMinute dimension
// that forms part of the deduplication key. A row whose (dimension-tuple,
// datetimeMinute) pair has already been applied is silently discarded.
//
// Applied-bucket sets are pruned after a configurable duration (typically twice
// the propagation delay) to bound memory usage. Counters reset to zero on
// process restart, which is correct Prometheus behavior.
package store

import (
	"strings"
	"sync"
	"time"
)

// DimensionKey is a string-serialized tuple of label values used to uniquely
// identify a combination of Prometheus label values for a given metric.
type DimensionKey string

// dedupKey uniquely identifies a single time-bucketed observation so that
// overlapping windows do not cause double counting.
type dedupKey struct {
	metric     string
	dims       DimensionKey
	timeBucket time.Time
}

// Store holds accumulated counter values with deduplication. All exported
// methods are safe for concurrent use.
type Store struct {
	mu sync.RWMutex

	// counters maps metric -> DimensionKey -> accumulated value.
	counters map[string]map[DimensionKey]float64

	// applied tracks which (metric, dims, timeBucket) tuples have already
	// been folded into the counters so that duplicate deliveries are
	// discarded.
	applied map[dedupKey]time.Time // value = wall-clock time when the entry was recorded

	// pruneAfter controls how long applied-set entries are retained before
	// they become eligible for pruning.
	pruneAfter time.Duration
}

// NewStore creates a new aggregation store. pruneAfter controls how long
// deduplication records are kept; a sensible default is twice the expected
// propagation delay.
func NewStore(pruneAfter time.Duration) *Store {
	return &Store{
		counters:   make(map[string]map[DimensionKey]float64),
		applied:    make(map[dedupKey]time.Time),
		pruneAfter: pruneAfter,
	}
}

// Add adds a value for the given metric/dimension/time bucket. It returns true
// if the value was new (not a duplicate). If the same (metric, dims,
// timeBucket) triple has been seen before, the call is a no-op and returns
// false.
func (s *Store) Add(metric string, dims DimensionKey, timeBucket time.Time, value float64) bool {
	key := dedupKey{
		metric:     metric,
		dims:       dims,
		timeBucket: timeBucket,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.applied[key]; exists {
		return false
	}

	s.applied[key] = time.Now()

	dimMap, ok := s.counters[metric]
	if !ok {
		dimMap = make(map[DimensionKey]float64)
		s.counters[metric] = dimMap
	}
	dimMap[dims] += value

	return true
}

// Get returns the accumulated counter value for a metric/dimension pair. If
// the pair has never been observed, zero is returned.
func (s *Store) Get(metric string, dims DimensionKey) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if dimMap, ok := s.counters[metric]; ok {
		return dimMap[dims]
	}
	return 0
}

// GetAll returns all (DimensionKey, value) pairs for a metric. The returned
// map is a shallow copy and safe to mutate. If the metric has never been
// observed, an empty (non-nil) map is returned.
func (s *Store) GetAll(metric string) map[DimensionKey]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[DimensionKey]float64)
	if dimMap, ok := s.counters[metric]; ok {
		for k, v := range dimMap {
			result[k] = v
		}
	}
	return result
}

// Prune removes deduplication records older than the configured pruneAfter
// duration. This bounds memory usage while still protecting against
// double counting within the deduplication window.
func (s *Store) Prune() {
	cutoff := time.Now().Add(-s.pruneAfter)

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, recorded := range s.applied {
		if recorded.Before(cutoff) {
			delete(s.applied, key)
		}
	}
}

// Reset clears all accumulated counters and deduplication state. This is
// appropriate on process restart where Prometheus expects counters to begin
// at zero.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.counters = make(map[string]map[DimensionKey]float64)
	s.applied = make(map[dedupKey]time.Time)
}

// MakeDimensionKey creates a DimensionKey from label name-value pairs. Labels
// are joined with a null byte separator to avoid ambiguity. The caller must
// supply an even number of strings (alternating name, value).
func MakeDimensionKey(labels ...string) DimensionKey {
	return DimensionKey(strings.Join(labels, "\x00"))
}
