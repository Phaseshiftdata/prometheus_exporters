package collector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/asymmetric-effort/prometheus-exporters/internal/cloudflare"
	"github.com/asymmetric-effort/prometheus-exporters/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// testSetup provides common test infrastructure for collector tests.
type testSetup struct {
	store       *store.Store
	selfMetrics *SelfMetrics
	registry    *prometheus.Registry
	logger      *zap.Logger
}

func newTestSetup(t *testing.T) testSetup {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	return testSetup{
		store:       store.NewStore(10 * time.Minute),
		selfMetrics: NewSelfMetrics("test", "test", "test"),
		registry:    prometheus.NewRegistry(),
		logger:      logger,
	}
}

// createTestClient creates a cloudflare.Client whose HTTP requests are
// redirected to the given test server.
func createTestClient(server *httptest.Server) *cloudflare.Client {
	c := cloudflare.NewClient("test-token", 5*time.Second)

	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = server.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}
	c.SetHTTPClient(httpClient)
	return c
}

// makeGraphQLResponse wraps data in a GraphQL response envelope.
func makeGraphQLResponse(data interface{}) []byte {
	dataBytes, _ := json.Marshal(data)
	resp := struct {
		Data   json.RawMessage `json:"data"`
		Errors []interface{}   `json:"errors"`
	}{
		Data: dataBytes,
	}
	b, _ := json.Marshal(resp)
	return b
}

// makeGraphQLErrorResponse wraps errors in a GraphQL response envelope.
func makeGraphQLErrorResponse(errorMsg string) []byte {
	resp := struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}{
		Data: json.RawMessage(`{}`),
		Errors: []struct {
			Message string `json:"message"`
		}{
			{Message: errorMsg},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// makeRESTResponse wraps result in a REST response envelope.
func makeRESTResponse(result interface{}) []byte {
	resultBytes, _ := json.Marshal(result)
	resp := struct {
		Success  bool            `json:"success"`
		Errors   []interface{}   `json:"errors"`
		Messages []interface{}   `json:"messages"`
		Result   json.RawMessage `json:"result"`
	}{
		Success: true,
		Result:  resultBytes,
	}
	b, _ := json.Marshal(resp)
	return b
}

// collectPromMetrics collects prometheus metrics from a prometheus.Collector.
func collectPromMetrics(t *testing.T, c prometheus.Collector) []prometheus.Metric {
	t.Helper()
	ch := make(chan prometheus.Metric, 100)
	go func() {
		c.Collect(ch)
		close(ch)
	}()

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}
	return metrics
}

// describePromMetrics collects descriptors from a prometheus.Collector.
func describePromMetrics(t *testing.T, c prometheus.Collector) []*prometheus.Desc {
	t.Helper()
	ch := make(chan *prometheus.Desc, 100)
	go func() {
		c.Describe(ch)
		close(ch)
	}()

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	return descs
}
