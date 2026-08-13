package github

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestNewMetrics_Registration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	if m.WorkflowRunsTotal == nil {
		t.Fatal("WorkflowRunsTotal not initialized")
	}
	if m.OpenPullRequests == nil {
		t.Fatal("OpenPullRequests not initialized")
	}
	if m.CommitsTotal == nil {
		t.Fatal("CommitsTotal not initialized")
	}
	if m.RateLimitRemaining == nil {
		t.Fatal("RateLimitRemaining not initialized")
	}
	if m.ScrapeErrorsTotal == nil {
		t.Fatal("ScrapeErrorsTotal not initialized")
	}
	if m.LastSuccessTimestamp == nil {
		t.Fatal("LastSuccessTimestamp not initialized")
	}
	if m.PollDurationSeconds == nil {
		t.Fatal("PollDurationSeconds not initialized")
	}

	// Verify metrics are gathered.
	if _, err := reg.Gather(); err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// At minimum, the gauge (RateLimitRemaining) should be present after set.
	m.RateLimitRemaining.Set(100)
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "github_exporter_rate_limit_remaining" {
			found = true
			if f.GetMetric()[0].GetGauge().GetValue() != 100 {
				t.Fatalf("expected rate limit 100, got %v", f.GetMetric()[0].GetGauge().GetValue())
			}
		}
	}
	if !found {
		t.Fatal("github_exporter_rate_limit_remaining not found in gathered metrics")
	}
}

func TestMetrics_CounterIncrement(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.WorkflowRunsTotal.WithLabelValues("my-repo", "ci", "success").Inc()
	m.WorkflowRunsTotal.WithLabelValues("my-repo", "ci", "success").Inc()
	m.WorkflowRunsTotal.WithLabelValues("my-repo", "ci", "failure").Inc()

	var metric dto.Metric

	counter, err := m.WorkflowRunsTotal.GetMetricWithLabelValues("my-repo", "ci", "success")
	if err != nil {
		t.Fatalf("failed to get metric: %v", err)
	}
	counter.Write(&metric)
	if metric.GetCounter().GetValue() != 2 {
		t.Fatalf("expected 2, got %v", metric.GetCounter().GetValue())
	}
}

func TestMetrics_GaugeSet(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.OpenPullRequests.WithLabelValues("my-repo").Set(5)

	var metric dto.Metric
	gauge, err := m.OpenPullRequests.GetMetricWithLabelValues("my-repo")
	if err != nil {
		t.Fatalf("failed to get metric: %v", err)
	}
	gauge.Write(&metric)
	if metric.GetGauge().GetValue() != 5 {
		t.Fatalf("expected 5, got %v", metric.GetGauge().GetValue())
	}
}

func TestMetrics_Histogram(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.PollDurationSeconds.Observe(1.5)
	m.PollDurationSeconds.Observe(2.5)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "github_exporter_poll_duration_seconds" {
			found = true
			h := f.GetMetric()[0].GetHistogram()
			if h.GetSampleCount() != 2 {
				t.Fatalf("expected 2 samples, got %d", h.GetSampleCount())
			}
		}
	}
	if !found {
		t.Fatal("histogram not found in gathered metrics")
	}
}

func TestMetrics_ScrapeErrors(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ScrapeErrorsTotal.WithLabelValues("repos").Inc()
	m.ScrapeErrorsTotal.WithLabelValues("repos").Inc()
	m.ScrapeErrorsTotal.WithLabelValues("workflows").Inc()

	var metric dto.Metric
	counter, err := m.ScrapeErrorsTotal.GetMetricWithLabelValues("repos")
	if err != nil {
		t.Fatalf("failed to get metric: %v", err)
	}
	counter.Write(&metric)
	if metric.GetCounter().GetValue() != 2 {
		t.Fatalf("expected 2, got %v", metric.GetCounter().GetValue())
	}
}

func TestMetrics_DoubleRegistrationPanics(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewMetrics(reg)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on double registration")
		}
	}()
	NewMetrics(reg)
}
