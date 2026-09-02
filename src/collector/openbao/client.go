// Package openbao provides a Prometheus collector for OpenBao cluster metrics.
package openbao

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxResponseBytes = 100 * 1024 * 1024 // 100 MiB

// HealthResponse represents the JSON response from /v1/sys/health.
type HealthResponse struct {
	Initialized                bool   `json:"initialized"`
	Sealed                     bool   `json:"sealed"`
	Standby                    bool   `json:"standby"`
	PerformanceStandby         bool   `json:"performance_standby"`
	ReplicationPerformanceMode string `json:"replication_performance_mode"`
	ReplicationDRMode          string `json:"replication_dr_mode"`
	ServerTimeUTC              int64  `json:"server_time_utc"`
	Version                    string `json:"version"`
	ClusterName                string `json:"cluster_name"`
	ClusterID                  string `json:"cluster_id"`
}

// RaftConfig represents the JSON response from /v1/sys/storage/raft/configuration.
type RaftConfig struct {
	Data RaftConfigData `json:"data"`
}

// RaftConfigData holds the raft configuration data.
type RaftConfigData struct {
	Config RaftConfigInner `json:"config"`
}

// RaftConfigInner holds the raft server list and index information.
type RaftConfigInner struct {
	Servers []RaftServer `json:"servers"`
	Index   uint64       `json:"index"`
}

// RaftServer represents a single server in the raft cluster.
type RaftServer struct {
	NodeID          string `json:"node_id"`
	Address         string `json:"address"`
	Leader          bool   `json:"leader"`
	Voter           bool   `json:"voter"`
	ProtocolVersion string `json:"protocol_version"`
}

// Client communicates with the OpenBao HTTP API.
type Client struct {
	httpClient *http.Client
	address    string
	token      []byte
}

// NewClient creates a new OpenBao API client. If token is empty, requests
// are made without authentication (sufficient for /v1/sys/health).
func NewClient(addr, token string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		address: addr,
		token:   []byte(token),
	}
}

// NewClientWithHTTP creates a new OpenBao API client with a custom http.Client.
// Useful for testing.
func NewClientWithHTTP(addr, token string, hc *http.Client) *Client {
	return &Client{
		httpClient: hc,
		address:    addr,
		token:      []byte(token),
	}
}

// ZeroToken overwrites the in-memory token with zeros.
func (c *Client) ZeroToken() {
	for i := range c.token {
		c.token[i] = 0
	}
}

// Health fetches /v1/sys/health from the given address. If addr is empty
// the client's configured address is used. The endpoint returns different
// HTTP status codes for different states (200=active, 429=standby,
// 472=DR standby, 501=uninitialized, 503=sealed) but always returns a
// JSON body. All status codes are treated as valid responses.
func (c *Client) Health(addr string) (*HealthResponse, error) {
	if addr == "" {
		addr = c.address
	}
	url := addr + "/v1/sys/health"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating health request: %w", err)
	}
	c.setToken(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("health request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading health response: %w", err)
	}

	var health HealthResponse
	if err := json.Unmarshal(body, &health); err != nil {
		return nil, fmt.Errorf("decoding health response: %w", err)
	}
	return &health, nil
}

// Metrics fetches /v1/sys/metrics?format=prometheus and returns the raw
// Prometheus text exposition format body.
func (c *Client) Metrics() (string, error) {
	url := c.address + "/v1/sys/metrics?format=prometheus"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating metrics request: %w", err)
	}
	c.setToken(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("metrics request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metrics request returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("reading metrics response: %w", err)
	}
	return string(body), nil
}

// RaftConfiguration fetches /v1/sys/storage/raft/configuration. This
// endpoint requires authentication and may not be available on all
// deployments. Returns nil, nil when the endpoint returns 403 or 404.
func (c *Client) RaftConfiguration() (*RaftConfig, error) {
	url := c.address + "/v1/sys/storage/raft/configuration"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating raft config request: %w", err)
	}
	c.setToken(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("raft config request: %w", err)
	}
	defer resp.Body.Close()

	// 403 and 404 are expected when auth is missing or raft is not in use.
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("raft config request returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading raft config response: %w", err)
	}

	var cfg RaftConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("decoding raft config response: %w", err)
	}
	return &cfg, nil
}

func (c *Client) setToken(req *http.Request) {
	if len(c.token) > 0 {
		req.Header.Set("X-Vault-Token", string(c.token))
	}
}
