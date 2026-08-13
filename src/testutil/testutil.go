// Package testutil provides shared test helpers for the exporter test suite.
package testutil

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// CollectAndCount registers c in a fresh registry and returns the number of
// metrics it emitted. It fails the test on any collection error.
func CollectAndCount(t *testing.T, c prometheus.Collector) int {
	t.Helper()
	return testutil.CollectAndCount(c)
}

// AssertNoMetrics verifies that the collector emits zero metrics.
func AssertNoMetrics(t *testing.T, c prometheus.Collector) {
	t.Helper()
	count := testutil.CollectAndCount(c)
	if count != 0 {
		t.Errorf("expected 0 metrics, got %d", count)
	}
}
