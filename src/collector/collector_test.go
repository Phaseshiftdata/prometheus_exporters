package collector

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// stubCollector is a minimal Collector implementation for compile-time
// interface verification.
type stubCollector struct{}

func (s *stubCollector) Name() string                                { return "stub" }
func (s *stubCollector) Describe(ch chan<- *prometheus.Desc)          {}
func (s *stubCollector) Collect(ch chan<- prometheus.Metric)          {}

var _ Collector = (*stubCollector)(nil)

func TestCollectorInterface(t *testing.T) {
	c := &stubCollector{}
	if c.Name() != "stub" {
		t.Errorf("expected name 'stub', got %q", c.Name())
	}
	// Verify Describe and Collect do not panic with nil-safe channels.
	ch := make(chan *prometheus.Desc, 1)
	c.Describe(ch)
	mch := make(chan prometheus.Metric, 1)
	c.Collect(mch)
}
