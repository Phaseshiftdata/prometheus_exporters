package openbao

import (
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Collector implements collector.Collector for OpenBao cluster metrics.
// Native OpenBao metrics from /v1/sys/metrics are stored and can be
// retrieved via NativeMetrics() for inclusion in the HTTP response.
type Collector struct {
	client       *Client
	pollInterval time.Duration

	// Cached cluster members discovered via raft configuration.
	mu      sync.RWMutex
	members []RaftServer

	// nativeMetrics stores the last fetched native metrics text.
	nativeMu      sync.RWMutex
	nativeMetrics string

	// Metric descriptors.
	up              *prometheus.Desc
	initialized     *prometheus.Desc
	sealed          *prometheus.Desc
	standby         *prometheus.Desc
	leader          *prometheus.Desc
	raftCommitIndex *prometheus.Desc
	raftAppliedIdx  *prometheus.Desc
	peers           *prometheus.Desc
	nodeInfo        *prometheus.Desc
}

// New creates a new OpenBao collector. The pollInterval controls how
// often cluster members are re-discovered via raft configuration.
func New(client *Client, pollInterval time.Duration) *Collector {
	c := &Collector{
		client:       client,
		pollInterval: pollInterval,
		up: prometheus.NewDesc(
			"openbao_up",
			"1 if the seed node is reachable.",
			nil, nil,
		),
		initialized: prometheus.NewDesc(
			"openbao_initialized",
			"1 if the cluster is initialized.",
			nil, nil,
		),
		sealed: prometheus.NewDesc(
			"openbao_sealed",
			"1 if the node is sealed.",
			[]string{"node"}, nil,
		),
		standby: prometheus.NewDesc(
			"openbao_standby",
			"1 if the node is in standby mode.",
			[]string{"node"}, nil,
		),
		leader: prometheus.NewDesc(
			"openbao_leader",
			"1 on the leader node.",
			[]string{"node"}, nil,
		),
		raftCommitIndex: prometheus.NewDesc(
			"openbao_raft_committed_index",
			"Raft committed index.",
			nil, nil,
		),
		raftAppliedIdx: prometheus.NewDesc(
			"openbao_raft_applied_index",
			"Raft applied index.",
			nil, nil,
		),
		peers: prometheus.NewDesc(
			"openbao_peers",
			"Number of raft peers.",
			nil, nil,
		),
		nodeInfo: prometheus.NewDesc(
			"openbao_node_info",
			"Information about an OpenBao node.",
			[]string{"node", "version"}, nil,
		),
	}

	// Start background goroutine for cluster discovery.
	if pollInterval > 0 {
		go c.discoverLoop()
	}

	return c
}

// Name returns the collector name.
func (c *Collector) Name() string { return "openbao" }

// Describe sends all metric descriptors to the channel.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.initialized
	ch <- c.sealed
	ch <- c.standby
	ch <- c.leader
	ch <- c.raftCommitIndex
	ch <- c.raftAppliedIdx
	ch <- c.peers
	ch <- c.nodeInfo
}

// Collect fetches metrics from the OpenBao API and sends them to the channel.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	// Fetch seed node health.
	health, err := c.client.Health("")
	if err != nil {
		slog.Debug("openbao health check failed", "error", err)
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)

	initVal := boolToFloat(health.Initialized)
	ch <- prometheus.MustNewConstMetric(c.initialized, prometheus.GaugeValue, initVal)

	// Seed node health metrics.
	seedNode := health.ClusterName
	if seedNode == "" {
		seedNode = "seed"
	}

	ch <- prometheus.MustNewConstMetric(c.sealed, prometheus.GaugeValue, boolToFloat(health.Sealed), seedNode)
	ch <- prometheus.MustNewConstMetric(c.standby, prometheus.GaugeValue, boolToFloat(health.Standby), seedNode)
	ch <- prometheus.MustNewConstMetric(c.nodeInfo, prometheus.GaugeValue, 1, seedNode, health.Version)

	isLeader := !health.Standby && !health.Sealed
	ch <- prometheus.MustNewConstMetric(c.leader, prometheus.GaugeValue, boolToFloat(isLeader), seedNode)

	// Cluster member metrics.
	c.mu.RLock()
	members := c.members
	c.mu.RUnlock()

	peerCount := len(members)
	if peerCount == 0 {
		peerCount = 1 // At least the seed node.
	}
	ch <- prometheus.MustNewConstMetric(c.peers, prometheus.GaugeValue, float64(peerCount))

	// Fetch per-member health for discovered members (skip seed).
	for _, m := range members {
		if m.Address == "" {
			continue
		}

		// Build address for health check using the member's address.
		// The raft address is typically hostname:8201 (raft port), but
		// the API is on port 8200. We cannot reliably derive the API
		// address from the raft address, so we report raft-level info.
		ch <- prometheus.MustNewConstMetric(c.leader, prometheus.GaugeValue, boolToFloat(m.Leader), m.NodeID)
	}

	// Fetch raft configuration for index metrics.
	raftCfg, err := c.client.RaftConfiguration()
	if err != nil {
		slog.Debug("openbao raft config fetch failed", "error", err)
	}
	if raftCfg != nil {
		ch <- prometheus.MustNewConstMetric(c.raftCommitIndex, prometheus.GaugeValue, float64(raftCfg.Data.Config.Index))
		ch <- prometheus.MustNewConstMetric(c.raftAppliedIdx, prometheus.GaugeValue, float64(raftCfg.Data.Config.Index))
	}

	// Fetch native metrics and store for inclusion in HTTP response.
	metricsText, err := c.client.Metrics()
	if err != nil {
		slog.Debug("openbao metrics fetch failed", "error", err)
		c.nativeMu.Lock()
		c.nativeMetrics = ""
		c.nativeMu.Unlock()
		return
	}
	c.nativeMu.Lock()
	c.nativeMetrics = metricsText
	c.nativeMu.Unlock()
}

// NativeMetrics returns the last fetched native metrics text from OpenBao.
// This is intended to be appended to the HTTP response after the
// registry-based metrics.
func (c *Collector) NativeMetrics() string {
	c.nativeMu.RLock()
	defer c.nativeMu.RUnlock()
	return c.nativeMetrics
}

// discoverLoop periodically discovers cluster members via raft configuration.
func (c *Collector) discoverLoop() {
	c.discover()
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for range ticker.C {
		c.discover()
	}
}

// discover fetches the raft configuration and caches the member list.
func (c *Collector) discover() {
	cfg, err := c.client.RaftConfiguration()
	if err != nil {
		slog.Debug("openbao cluster discovery failed", "error", err)
		return
	}
	if cfg == nil {
		return
	}

	c.mu.Lock()
	c.members = cfg.Data.Config.Servers
	c.mu.Unlock()

	slog.Debug("openbao cluster discovery complete", "peers", len(cfg.Data.Config.Servers))
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
