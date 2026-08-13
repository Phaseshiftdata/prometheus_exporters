package testutil

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

type emptyCollector struct{}

func (e *emptyCollector) Describe(ch chan<- *prometheus.Desc) {}
func (e *emptyCollector) Collect(ch chan<- prometheus.Metric)  {}

type singleCollector struct {
	desc *prometheus.Desc
}

func (s *singleCollector) Describe(ch chan<- *prometheus.Desc) { ch <- s.desc }
func (s *singleCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(s.desc, prometheus.GaugeValue, 1)
}

func TestCollectAndCount(t *testing.T) {
	c := &singleCollector{desc: prometheus.NewDesc("test_metric", "test", nil, nil)}
	count := CollectAndCount(t, c)
	if count != 1 {
		t.Errorf("expected 1 metric, got %d", count)
	}
}

func TestAssertNoMetrics(t *testing.T) {
	c := &emptyCollector{}
	AssertNoMetrics(t, c)
}

func TestAssertNoMetricsFails(t *testing.T) {
	// Verify that AssertNoMetrics reports an error when metrics are emitted.
	fakeT := &testing.T{}
	c := &singleCollector{desc: prometheus.NewDesc("test_fail_metric", "test", nil, nil)}
	AssertNoMetrics(fakeT, c)
	if !fakeT.Failed() {
		t.Error("expected AssertNoMetrics to fail when collector emits metrics")
	}
}
