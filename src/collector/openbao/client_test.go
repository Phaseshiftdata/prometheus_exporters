package openbao

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("http://localhost:8200", "test-token")
	if c.address != "http://localhost:8200" {
		t.Errorf("expected address http://localhost:8200, got %q", c.address)
	}
	if string(c.token) != "test-token" {
		t.Errorf("expected token test-token, got %q", string(c.token))
	}
	if c.httpClient == nil {
		t.Error("expected non-nil httpClient")
	}
}

func TestNewClientEmptyToken(t *testing.T) {
	c := NewClient("http://localhost:8200", "")
	if len(c.token) != 0 {
		t.Errorf("expected empty token, got %q", string(c.token))
	}
}

func TestZeroToken(t *testing.T) {
	c := NewClient("http://localhost:8200", "secret-token")
	c.ZeroToken()
	for i, b := range c.token {
		if b != 0 {
			t.Errorf("token byte %d not zeroed: %d", i, b)
		}
	}
}

func TestHealthSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sys/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0","cluster_name":"test-cluster","cluster_id":"abc123"}`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "tok", srv.Client())
	h, err := c.Health("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.Initialized {
		t.Error("expected initialized=true")
	}
	if h.Sealed {
		t.Error("expected sealed=false")
	}
	if h.Standby {
		t.Error("expected standby=false")
	}
	if h.Version != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %q", h.Version)
	}
	if h.ClusterName != "test-cluster" {
		t.Errorf("expected cluster_name test-cluster, got %q", h.ClusterName)
	}
}

func TestHealthStandbyNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Standby nodes return 429.
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"initialized":true,"sealed":false,"standby":true,"version":"2.0.0","cluster_name":"test-cluster"}`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "", srv.Client())
	h, err := c.Health("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.Standby {
		t.Error("expected standby=true")
	}
}

func TestHealthSealedNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sealed nodes return 503.
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"initialized":true,"sealed":true,"standby":false,"version":"2.0.0"}`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "", srv.Client())
	h, err := c.Health("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.Sealed {
		t.Error("expected sealed=true")
	}
}

func TestHealthUninitializedNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Uninitialized nodes return 501.
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"initialized":false,"sealed":true,"standby":false,"version":"2.0.0"}`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "", srv.Client())
	h, err := c.Health("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Initialized {
		t.Error("expected initialized=false")
	}
}

func TestHealthCustomAddress(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"initialized":true,"sealed":false,"standby":true,"version":"2.0.0"}`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP("http://should-not-use:8200", "", srv.Client())
	_, err := c.Health(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/v1/sys/health" {
		t.Errorf("expected path /v1/sys/health, got %q", gotPath)
	}
}

func TestHealthConnectionError(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "")
	_, err := c.Health("")
	if err == nil {
		t.Error("expected error for unreachable address")
	}
}

func TestHealthInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "", srv.Client())
	_, err := c.Health("")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestHealthTokenHeader(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Vault-Token")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0"}`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "my-secret-token", srv.Client())
	_, err := c.Health("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotToken != "my-secret-token" {
		t.Errorf("expected token my-secret-token, got %q", gotToken)
	}
}

func TestHealthNoTokenHeader(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Vault-Token")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"initialized":true,"sealed":false,"standby":false,"version":"2.0.0"}`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "", srv.Client())
	_, err := c.Health("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotToken != "" {
		t.Errorf("expected no token header, got %q", gotToken)
	}
}

func TestMetricsSuccess(t *testing.T) {
	body := `# HELP openbao_runtime_alloc_bytes Current bytes allocated.
# TYPE openbao_runtime_alloc_bytes gauge
openbao_runtime_alloc_bytes 1234567
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sys/metrics" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("format") != "prometheus" {
			t.Errorf("expected format=prometheus, got %q", r.URL.Query().Get("format"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "tok", srv.Client())
	got, err := c.Metrics()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "openbao_runtime_alloc_bytes") {
		t.Errorf("metrics response missing expected content: %s", got)
	}
}

func TestMetricsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":["permission denied"]}`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "", srv.Client())
	_, err := c.Metrics()
	if err == nil {
		t.Error("expected error for 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestMetricsConnectionError(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "")
	_, err := c.Metrics()
	if err == nil {
		t.Error("expected error for unreachable address")
	}
}

func TestRaftConfigurationSuccess(t *testing.T) {
	body := `{
		"data": {
			"config": {
				"index": 42,
				"servers": [
					{"node_id": "node1", "address": "10.0.0.1:8201", "leader": true, "voter": true},
					{"node_id": "node2", "address": "10.0.0.2:8201", "leader": false, "voter": true},
					{"node_id": "node3", "address": "10.0.0.3:8201", "leader": false, "voter": true}
				]
			}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sys/storage/raft/configuration" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "tok", srv.Client())
	cfg, err := c.RaftConfiguration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Data.Config.Servers) != 3 {
		t.Errorf("expected 3 servers, got %d", len(cfg.Data.Config.Servers))
	}
	if cfg.Data.Config.Index != 42 {
		t.Errorf("expected index 42, got %d", cfg.Data.Config.Index)
	}
	if !cfg.Data.Config.Servers[0].Leader {
		t.Error("expected first server to be leader")
	}
}

func TestRaftConfigurationForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":["permission denied"]}`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "", srv.Client())
	cfg, err := c.RaftConfiguration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config for 403")
	}
}

func TestRaftConfigurationNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "", srv.Client())
	cfg, err := c.RaftConfiguration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config for 404")
	}
}

func TestRaftConfigurationServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "", srv.Client())
	_, err := c.RaftConfiguration()
	if err == nil {
		t.Error("expected error for 500")
	}
}

func TestRaftConfigurationInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "tok", srv.Client())
	_, err := c.RaftConfiguration()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestRaftConfigurationConnectionError(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "")
	_, err := c.RaftConfiguration()
	if err == nil {
		t.Error("expected error for unreachable address")
	}
}

func TestSetTokenPresent(t *testing.T) {
	c := NewClient("http://localhost:8200", "test-token")
	req, _ := http.NewRequest(http.MethodGet, "http://localhost:8200/test", nil)
	c.setToken(req)
	if req.Header.Get("X-Vault-Token") != "test-token" {
		t.Errorf("expected X-Vault-Token test-token, got %q", req.Header.Get("X-Vault-Token"))
	}
}

func TestSetTokenEmpty(t *testing.T) {
	c := NewClient("http://localhost:8200", "")
	req, _ := http.NewRequest(http.MethodGet, "http://localhost:8200/test", nil)
	c.setToken(req)
	if req.Header.Get("X-Vault-Token") != "" {
		t.Errorf("expected empty X-Vault-Token, got %q", req.Header.Get("X-Vault-Token"))
	}
}

func TestHealthDRStandbyNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// DR standby nodes return 472.
		w.WriteHeader(472)
		w.Write([]byte(`{"initialized":true,"sealed":false,"standby":true,"performance_standby":false,"replication_dr_mode":"secondary","version":"2.0.0"}`))
	}))
	defer srv.Close()

	c := NewClientWithHTTP(srv.URL, "", srv.Client())
	h, err := c.Health("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.Standby {
		t.Error("expected standby=true for DR standby")
	}
}

func TestNewClientWithHTTP(t *testing.T) {
	hc := &http.Client{}
	c := NewClientWithHTTP("http://test:8200", "tok", hc)
	if c.httpClient != hc {
		t.Error("expected custom http client")
	}
	if c.address != "http://test:8200" {
		t.Errorf("unexpected address: %q", c.address)
	}
}
