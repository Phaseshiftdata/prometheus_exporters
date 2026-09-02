package openbao

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// mockServer creates an httptest.Server that handles OpenBao API endpoints.
func mockServer(t *testing.T, opts mockOpts) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sys/health":
			if opts.healthStatus == 0 {
				opts.healthStatus = http.StatusOK
			}
			w.WriteHeader(opts.healthStatus)
			w.Write([]byte(opts.healthBody))
		case "/v1/sys/metrics":
			if opts.metricsStatus == 0 {
				opts.metricsStatus = http.StatusOK
			}
			w.WriteHeader(opts.metricsStatus)
			w.Write([]byte(opts.metricsBody))
		case "/v1/sys/storage/raft/configuration":
			if opts.raftStatus == 0 {
				opts.raftStatus = http.StatusOK
			}
			w.WriteHeader(opts.raftStatus)
			w.Write([]byte(opts.raftBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

type mockOpts struct {
	healthBody    string
	healthStatus  int
	metricsBody   string
	metricsStatus int
	raftBody      string
	raftStatus    int
}

func TestCollectorName(t *testing.T) {
	c := New(NewClient("http://localhost:8200", ""), 0)
	if c.Name() != "openbao" {
		t.Errorf("expected name openbao, got %q", c.Name())
	}
}

func TestCollectorDescribe(t *testing.T) {
	c := New(NewClient("http://localhost:8200", ""), 0)
	ch := make(chan *prometheus.Desc, 20)
	c.Describe(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	if count != 9 {
		t.Errorf("expected 9 descriptors, got %d", count)
	}
}

func TestCollectHealthyCluster(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody: `{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"prod","cluster_id":"abc"}`,
		metricsBody: `# HELP openbao_runtime_alloc_bytes Allocated bytes.
# TYPE openbao_runtime_alloc_bytes gauge
openbao_runtime_alloc_bytes 12345
`,
		raftBody: `{"data":{"config":{"index":100,"servers":[
			{"node_id":"node1","address":"10.0.0.1:8201","leader":true,"voter":true},
			{"node_id":"node2","address":"10.0.0.2:8201","leader":false,"voter":true}
		]}}}`,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "tok", srv.Client())
	coll := New(client, 0)

	// Manually trigger discovery.
	coll.discover()

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)

	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	// Check that key metrics are present.
	names := make(map[string]bool)
	for _, mf := range metrics {
		names[mf.GetName()] = true
	}

	expected := []string{
		"openbao_up",
		"openbao_initialized",
		"openbao_sealed",
		"openbao_standby",
		"openbao_leader",
		"openbao_peers",
		"openbao_raft_committed_index",
		"openbao_raft_applied_index",
		"openbao_node_info",
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected metric %q not found in gathered metrics", name)
		}
	}

	// Verify specific values.
	upVal := findGaugeValue(metrics, "openbao_up", nil)
	if upVal != 1 {
		t.Errorf("expected openbao_up=1, got %f", upVal)
	}
}

func TestCollectSealedNode(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody:    `{"initialized":true,"sealed":true,"standby":false,"version":"2.0.0","cluster_name":"prod"}`,
		healthStatus:  http.StatusServiceUnavailable,
		metricsBody:   "",
		metricsStatus: http.StatusServiceUnavailable,
		raftStatus:    http.StatusForbidden,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	upVal := findGaugeValue(metrics, "openbao_up", nil)
	if upVal != 1 {
		t.Errorf("expected up=1, got %f", upVal)
	}
	sealedVal := findGaugeValue(metrics, "openbao_sealed", map[string]string{"node": "prod"})
	if sealedVal != 1 {
		t.Errorf("expected sealed=1, got %f", sealedVal)
	}
}

func TestCollectStandbyNode(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody:   `{"initialized":true,"sealed":false,"standby":true,"version":"2.0.0","cluster_name":"prod"}`,
		healthStatus: http.StatusTooManyRequests,
		metricsBody: `# HELP test_metric A test metric.
# TYPE test_metric gauge
test_metric 42
`,
		raftStatus: http.StatusForbidden,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	standbyVal := findGaugeValue(metrics, "openbao_standby", map[string]string{"node": "prod"})
	if standbyVal != 1 {
		t.Errorf("expected standby=1, got %f", standbyVal)
	}
	leaderVal := findGaugeValue(metrics, "openbao_leader", map[string]string{"node": "prod"})
	if leaderVal != 0 {
		t.Errorf("expected leader=0 for standby, got %f", leaderVal)
	}
}

func TestCollectUnreachableNode(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "")
	coll := New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	upVal := findGaugeValue(metrics, "openbao_up", nil)
	if upVal != 0 {
		t.Errorf("expected up=0 for unreachable node, got %f", upVal)
	}
}

func TestCollectNoRaftConfig(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody: `{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"single"}`,
		metricsBody: `# HELP test_gauge A gauge.
# TYPE test_gauge gauge
test_gauge 1
`,
		raftStatus: http.StatusNotFound,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	peersVal := findGaugeValue(metrics, "openbao_peers", nil)
	if peersVal != 1 {
		t.Errorf("expected peers=1 when no raft config, got %f", peersVal)
	}
}

func TestCollectWithRaftMembers(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody: `{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"cluster1"}`,
		metricsBody: `# HELP test_counter A counter.
# TYPE test_counter counter
test_counter 100
`,
		raftBody: `{"data":{"config":{"index":50,"servers":[
			{"node_id":"n1","address":"10.0.0.1:8201","leader":true,"voter":true},
			{"node_id":"n2","address":"10.0.0.2:8201","leader":false,"voter":true},
			{"node_id":"n3","address":"10.0.0.3:8201","leader":false,"voter":true}
		]}}}`,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "tok", srv.Client())
	coll := New(client, 0)
	coll.discover()

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	peersVal := findGaugeValue(metrics, "openbao_peers", nil)
	if peersVal != 3 {
		t.Errorf("expected peers=3, got %f", peersVal)
	}
	commitVal := findGaugeValue(metrics, "openbao_raft_committed_index", nil)
	if commitVal != 50 {
		t.Errorf("expected raft_committed_index=50, got %f", commitVal)
	}
}

func TestCollectMetricsStored(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody: `{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"test"}`,
		metricsBody: `# HELP test_gauge A gauge.
# TYPE test_gauge gauge
test_gauge 42
`,
		raftStatus: http.StatusNotFound,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	upVal := findGaugeValue(metrics, "openbao_up", nil)
	if upVal != 1 {
		t.Errorf("expected up=1, got %f", upVal)
	}

	// Native metrics should be stored for retrieval.
	native := coll.NativeMetrics()
	if !strings.Contains(native, "test_gauge 42") {
		t.Errorf("expected native metrics to contain test_gauge 42, got %q", native)
	}
}

func TestCollectMetricsFetchError(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody:    `{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"test"}`,
		metricsStatus: http.StatusForbidden,
		metricsBody:   `{"errors":["permission denied"]}`,
		raftStatus:    http.StatusNotFound,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	upVal := findGaugeValue(metrics, "openbao_up", nil)
	if upVal != 1 {
		t.Errorf("expected up=1 even when metrics fetch fails, got %f", upVal)
	}
}

func TestCollectEmptyClusterName(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody: `{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":""}`,
		metricsBody: `# TYPE t gauge
t 1
`,
		raftStatus: http.StatusNotFound,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	// Should use "seed" as the node name.
	sealedVal := findGaugeValue(metrics, "openbao_sealed", map[string]string{"node": "seed"})
	if sealedVal != 0 {
		t.Errorf("expected sealed=0 for seed node, got %f", sealedVal)
	}
}

func TestBoolToFloat(t *testing.T) {
	if boolToFloat(true) != 1 {
		t.Error("expected 1 for true")
	}
	if boolToFloat(false) != 0 {
		t.Error("expected 0 for false")
	}
}

func TestNativeMetricsEmpty(t *testing.T) {
	coll := New(NewClient("http://localhost:8200", ""), 0)
	if coll.NativeMetrics() != "" {
		t.Error("expected empty native metrics initially")
	}
}

func TestNativeMetricsStored(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody: `{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"test"}`,
		metricsBody: `# HELP native_gauge A gauge.
# TYPE native_gauge gauge
native_gauge 99
`,
		raftStatus: http.StatusNotFound,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	// Collect to trigger metrics fetch.
	ch := make(chan prometheus.Metric, 50)
	coll.Collect(ch)
	close(ch)
	// Drain channel.
	for range ch {
	}

	native := coll.NativeMetrics()
	if !strings.Contains(native, "native_gauge 99") {
		t.Errorf("expected native metrics to contain native_gauge, got %q", native)
	}
}

func TestNativeMetricsClearedOnError(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody:    `{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"test"}`,
		metricsStatus: http.StatusForbidden,
		metricsBody:   `{"errors":["forbidden"]}`,
		raftStatus:    http.StatusNotFound,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	ch := make(chan prometheus.Metric, 50)
	coll.Collect(ch)
	close(ch)
	for range ch {
	}

	native := coll.NativeMetrics()
	if native != "" {
		t.Errorf("expected empty native metrics on error, got %q", native)
	}
}

func TestDiscoverUpdatesMembers(t *testing.T) {
	srv := mockServer(t, mockOpts{
		raftBody: `{"data":{"config":{"index":10,"servers":[
			{"node_id":"a","address":"10.0.0.1:8201","leader":true},
			{"node_id":"b","address":"10.0.0.2:8201","leader":false}
		]}}}`,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "tok", srv.Client())
	coll := New(client, 0)

	coll.discover()

	coll.mu.RLock()
	count := len(coll.members)
	coll.mu.RUnlock()

	if count != 2 {
		t.Errorf("expected 2 members, got %d", count)
	}
}

func TestDiscoverFailsGracefully(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "")
	coll := New(client, 0)

	// Should not panic.
	coll.discover()

	coll.mu.RLock()
	count := len(coll.members)
	coll.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected 0 members on failure, got %d", count)
	}
}

func TestDiscoverNilConfig(t *testing.T) {
	srv := mockServer(t, mockOpts{
		raftStatus: http.StatusForbidden,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	coll.discover()

	coll.mu.RLock()
	count := len(coll.members)
	coll.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected 0 members when raft is forbidden, got %d", count)
	}
}

func TestCollectorWithPollInterval(t *testing.T) {
	srv := mockServer(t, mockOpts{
		raftBody: `{"data":{"config":{"index":1,"servers":[
			{"node_id":"a","address":"10.0.0.1:8201","leader":true}
		]}}}`,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "tok", srv.Client())
	// Use a short poll interval; the background goroutine starts immediately.
	_ = New(client, 100*time.Millisecond)

	// Wait for at least one discovery cycle.
	time.Sleep(250 * time.Millisecond)
}

func TestCollectRaftConfigError(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody: `{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"test"}`,
		metricsBody: `# TYPE g gauge
g 1
`,
		raftStatus: http.StatusInternalServerError,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	upVal := findGaugeValue(metrics, "openbao_up", nil)
	if upVal != 1 {
		t.Errorf("expected up=1, got %f", upVal)
	}
}

func TestCollectMemberWithEmptyAddress(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody: `{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"test"}`,
		metricsBody: `# TYPE g gauge
g 1
`,
		raftStatus: http.StatusNotFound,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	// Manually set a member with empty address.
	coll.mu.Lock()
	coll.members = []RaftServer{{NodeID: "empty", Address: ""}}
	coll.mu.Unlock()

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	upVal := findGaugeValue(metrics, "openbao_up", nil)
	if upVal != 1 {
		t.Errorf("expected up=1, got %f", upVal)
	}
}

func TestCollectInitializedFalse(t *testing.T) {
	srv := mockServer(t, mockOpts{
		healthBody:   `{"initialized":false,"sealed":true,"standby":false,"version":"2.0.0","cluster_name":"new"}`,
		healthStatus: http.StatusNotImplemented,
		metricsBody:  "",
		metricsStatus: http.StatusServiceUnavailable,
		raftStatus:   http.StatusForbidden,
	})
	defer srv.Close()

	client := NewClientWithHTTP(srv.URL, "", srv.Client())
	coll := New(client, 0)

	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	initVal := findGaugeValue(metrics, "openbao_initialized", nil)
	if initVal != 0 {
		t.Errorf("expected initialized=0, got %f", initVal)
	}
}

// findGaugeValue searches gathered metric families for a metric with the given
// name and optional label set, returning its gauge value. Returns -1 if not found.
func findGaugeValue(families []*dto.MetricFamily, name string, labels map[string]string) float64 {
	for _, mf := range families {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if matchLabels(m, labels) {
				if m.GetGauge() != nil {
					return m.GetGauge().GetValue()
				}
				if m.GetUntyped() != nil {
					return m.GetUntyped().GetValue()
				}
			}
		}
	}
	return -1
}

// matchLabels returns true if metric m has all the given label key-value pairs.
func matchLabels(m *dto.Metric, labels map[string]string) bool {
	if len(labels) == 0 {
		return true
	}
	lps := m.GetLabel()
	for k, v := range labels {
		found := false
		for _, lp := range lps {
			if lp.GetName() == k && lp.GetValue() == v {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

