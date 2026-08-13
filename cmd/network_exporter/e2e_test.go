package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector/arp"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/conntrack"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/firewall"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/iface"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/netgraph"
)

// TestE2ENetworkExporterMetrics starts a real HTTP server with mock-backed
// collectors and verifies that scraping /metrics returns all expected metric
// families.
func TestE2ENetworkExporterMetrics(t *testing.T) {
	// Pick a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	reg := prometheus.NewRegistry()

	// Wire collectors with mock backends so they produce deterministic output
	// without needing root or real netlink/procfs.
	mockArp := arp.NewWithLister(&mockNeighborLister{})
	mockIface := iface.NewWithLister(&mockLinkLister{})
	mockNetgraph := netgraph.NewWithSource(&mockConnectionSource{})
	mockConntrack := conntrack.NewWithSources(&mockSocketSource{}, &mockConntrackSource{})
	mockFirewall := firewall.NewWithReader(&mockNftablesReader{})

	for _, c := range []prometheus.Collector{mockArp, mockIface, mockNetgraph, mockConntrack, mockFirewall} {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- serve(ctx, addr, reg) }()

	// Wait for server to be ready.
	waitForServer(t, addr)

	// Scrape /metrics.
	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	metrics := string(body)

	// Verify each collector contributed its expected metric families.
	expectedFamilies := []string{
		"network_arp_entry",
		"network_interface_type",
		"network_graph_edge",
		"network_port_listen",
		"network_port_connections",
		"network_conntrack_accounting_enabled",
		"network_firewall_drop_packets_total",
		"network_firewall_drop_bytes_total",
	}
	for _, family := range expectedFamilies {
		if !strings.Contains(metrics, family) {
			t.Errorf("expected metric family %q not found in /metrics output", family)
		}
	}

	// Verify the landing page returns HTML.
	resp2, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body2), "Network Exporter") {
		t.Error("landing page does not contain expected title")
	}

	// Clean shutdown.
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not shut down within 5s")
	}
}

// alwaysFailingCollector stands in for a collector whose data source is not
// present on the host -- the firewall collector shelling out to an nft(8) that
// the distroless image never had was the real instance of this. It reads
// nftables over netlink now, but the failure mode this guards against is a
// property of promhttp, not of any one collector.
type alwaysFailingCollector struct {
	desc *prometheus.Desc
}

func (c *alwaysFailingCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

func (c *alwaysFailingCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.NewInvalidMetric(c.desc, errors.New("data source unavailable"))
}

// TestE2EFailingCollectorDoesNotBlankMetrics is the regression test for the
// HTTP 500 that hid every metric on monitor01.
//
// promhttp defaults to HTTPErrorOnError, so a single collector returning an
// error discarded the entire response body and the other four collectors with
// it. The endpoint must serve what it can instead.
func TestE2EFailingCollectorDoesNotBlankMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()

	healthy := []prometheus.Collector{
		arp.NewWithLister(&mockNeighborLister{}),
		iface.NewWithLister(&mockLinkLister{}),
		netgraph.NewWithSource(&mockConnectionSource{}),
		conntrack.NewWithSources(&mockSocketSource{}, &mockConntrackSource{}),
	}
	broken := &alwaysFailingCollector{
		desc: prometheus.NewDesc("network_broken_metric", "Never collectable.", nil, nil),
	}

	for _, c := range append(healthy, broken) {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = serve(ctx, addr, reg) }()
	waitForServer(t, addr)

	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 despite a failing collector, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	metrics := string(body)

	// The four healthy collectors must still be fully represented.
	for _, family := range []string{
		"network_arp_entry",
		"network_interface_type",
		"network_bridge_member",
		"network_graph_edge",
		"network_port_listen",
		"network_port_connections",
		"network_conntrack_accounting_enabled",
	} {
		if !strings.Contains(metrics, family) {
			t.Errorf("metric family %q was lost to the failing collector", family)
		}
	}

	// The broken collector contributes nothing, which is the intended gap.
	if strings.Contains(metrics, "network_broken_metric") {
		t.Error("the failing collector should contribute no samples")
	}
}

// TestE2EFirewallIsAlwaysVisible pairs with the test above: whatever the
// firewall collector finds -- a readable ruleset, an empty one, or a netlink
// socket that refuses every message -- it must expose its up gauge and must
// not take the rest of /metrics down with it.
//
// The outcome is deliberately not asserted, because it depends on the machine.
// CI has no CAP_NET_ADMIN, so the probe there gets EPERM and the gauge reads 0;
// a developer's Linux box with a ruleset gets 1. Pinning either one would make
// this test pass for the wrong reason somewhere.
func TestE2EFirewallIsAlwaysVisible(t *testing.T) {
	reg := prometheus.NewRegistry()
	// firewall.New() opens a real NETLINK_NETFILTER socket and probes it.
	if err := reg.Register(firewall.New()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.Register(arp.NewWithLister(&mockNeighborLister{})); err != nil {
		t.Fatalf("register: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = serve(ctx, addr, reg) }()
	waitForServer(t, addr)

	metrics := scrapeMetrics(t, addr)
	if !strings.Contains(metrics, "network_firewall_collector_up 0") &&
		!strings.Contains(metrics, "network_firewall_collector_up 1") {
		t.Error("expected network_firewall_collector_up to be exposed")
	}
	if !strings.Contains(metrics, "network_arp_entry") {
		t.Error("expected the arp collector to still be served")
	}
}

func waitForServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become ready", addr)
}

// --- Mock implementations for e2e ---

type mockNeighborLister struct{}

func (m *mockNeighborLister) ListNeighbors() ([]arp.Neighbor, error) {
	return []arp.Neighbor{
		{IP: net.ParseIP("192.168.1.1"), MAC: net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, Device: "eth0", State: 0x02}, // NUD_REACHABLE
		{IP: net.ParseIP("192.168.1.2"), MAC: net.HardwareAddr{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}, Device: "eth0", State: 0x04}, // NUD_STALE
	}, nil
}

type mockLinkLister struct{}

func (m *mockLinkLister) ListLinks() ([]iface.LinkInfo, error) {
	return []iface.LinkInfo{
		{Name: "eth0", Type: "physical", Driver: "ixgbe"},
		{Name: "br0", Type: "bridge", Driver: "bridge"},
		{Name: "veth0", Type: "veth", MasterName: "br0", MasterType: "bridge"},
	}, nil
}

type mockConnectionSource struct{}

func (m *mockConnectionSource) ListConnections() ([]netgraph.Connection, error) {
	return []netgraph.Connection{
		{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "LISTEN", Protocol: "tcp"},
		{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
		{LocalIP: "192.168.1.10", LocalPort: 50000, RemoteIP: "10.0.0.1", RemotePort: 443, State: "ESTABLISHED", Protocol: "tcp"},
	}, nil
}

type mockSocketSource struct{}

func (m *mockSocketSource) ListSockets() ([]conntrack.SocketEntry, error) {
	return []conntrack.SocketEntry{
		{LocalIP: "0.0.0.0", LocalPort: 9090, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
		{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
	}, nil
}

type mockConntrackSource struct{}

func (m *mockConntrackSource) ListFlows() ([]conntrack.ConntrackFlow, error) {
	return []conntrack.ConntrackFlow{
		{Protocol: "tcp", SrcPort: 45000, DstPort: 9090, BytesIn: 1024, BytesOut: 2048},
	}, nil
}

type mockNftablesReader struct{}

func (m *mockNftablesReader) GetDropRejectRules() ([]firewall.RuleInfo, error) {
	return []firewall.RuleInfo{
		{Family: "inet", Table: "filter", Chain: "input", Rule: "drop all", Verdict: "drop", Packets: 100, Bytes: 5000},
	}, nil
}

func (m *mockNftablesReader) GetChainPolicies() ([]firewall.ChainPolicy, error) {
	return []firewall.ChainPolicy{
		{Family: "inet", Table: "filter", Chain: "input", Policy: "drop", Packets: 50, Bytes: 2500},
	}, nil
}

// --- Helpers for topology autodiscovery e2e tests ---

// scrapeMetrics fetches /metrics from the given address and returns the body.
func scrapeMetrics(t *testing.T, addr string) string {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

// --- Stateful mocks for multi-scrape topology tests ---

// statefulConnectionSource returns different connection data on each call.
type statefulConnectionSource struct {
	mu       sync.Mutex
	responses [][]netgraph.Connection
	callCount int
}

func (m *statefulConnectionSource) ListConnections() ([]netgraph.Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.callCount
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.callCount++
	return m.responses[idx], nil
}

func (m *statefulConnectionSource) setResponses(responses [][]netgraph.Connection) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = responses
	m.callCount = 0
}

// statefulLinkLister returns different link data on each call.
type statefulLinkLister struct {
	mu       sync.Mutex
	responses [][]iface.LinkInfo
	callCount int
}

func (m *statefulLinkLister) ListLinks() ([]iface.LinkInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.callCount
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.callCount++
	return m.responses[idx], nil
}

// statefulNeighborLister returns different ARP data on each call.
type statefulNeighborLister struct {
	mu       sync.Mutex
	responses [][]arp.Neighbor
	callCount int
}

func (m *statefulNeighborLister) ListNeighbors() ([]arp.Neighbor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.callCount
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.callCount++
	return m.responses[idx], nil
}

// statefulSocketSource returns different socket data on each call.
type statefulSocketSource struct {
	mu       sync.Mutex
	responses [][]conntrack.SocketEntry
	callCount int
}

func (m *statefulSocketSource) ListSockets() ([]conntrack.SocketEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.callCount
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.callCount++
	return m.responses[idx], nil
}

// startCustomE2EServer starts a server with the given collectors and returns
// the address and cancel function.
func startCustomE2EServer(t *testing.T, collectors ...prometheus.Collector) (string, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	reg := prometheus.NewRegistry()
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = serve(ctx, addr, reg) }()
	waitForServer(t, addr)
	return addr, cancel
}

// TestE2ENewServiceAppears verifies that autodiscovery picks up new listening
// ports without restart.
func TestE2ENewServiceAppears(t *testing.T) {
	// The conntrack collector is what emits network_port_listen and
	// network_port_connections. The netgraph collector emits network_graph_edge.
	// Both consume connection/socket data, but from different sources.
	// We need stateful sources for both to observe the topology change.

	connSrc := &statefulConnectionSource{
		responses: [][]netgraph.Connection{
			// First scrape: only port 9090
			{
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
			},
			// Second scrape: ports 9090 AND 8080 (new service)
			{
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 8080, RemoteIP: "10.0.0.5", RemotePort: 50000, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 8080, RemoteIP: "10.0.0.5", RemotePort: 50000, State: "ESTABLISHED", Protocol: "tcp"},
			},
		},
	}

	socketSrc := &statefulSocketSource{
		responses: [][]conntrack.SocketEntry{
			// First scrape: only port 9090
			{
				{LocalIP: "0.0.0.0", LocalPort: 9090, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
			},
			// Second scrape: ports 9090 AND 8080
			{
				{LocalIP: "0.0.0.0", LocalPort: 9090, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
				{LocalIP: "0.0.0.0", LocalPort: 8080, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 8080, RemoteIP: "10.0.0.5", RemotePort: 50000, State: "ESTABLISHED", Protocol: "tcp"},
			},
		},
	}

	mockGraph := netgraph.NewWithSource(connSrc)
	mockConn := conntrack.NewWithSources(socketSrc, &mockConntrackSource{})

	addr, cancel := startCustomE2EServer(t, mockGraph, mockConn)
	defer cancel()

	// First scrape: only port 9090 should be present.
	metrics1 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics1, `local_port="9090"`) {
		t.Error("first scrape: expected port 9090 in graph edges")
	}
	if strings.Contains(metrics1, `local_port="8080"`) {
		t.Error("first scrape: port 8080 should NOT be present yet")
	}
	if !strings.Contains(metrics1, `port="9090"`) {
		t.Error("first scrape: expected port 9090 in conntrack listen")
	}
	if strings.Contains(metrics1, `port="8080"`) {
		t.Error("first scrape: port 8080 should NOT be in conntrack yet")
	}

	// Second scrape: both ports should be present.
	metrics2 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics2, `local_port="9090"`) {
		t.Error("second scrape: expected port 9090 in graph edges")
	}
	if !strings.Contains(metrics2, `local_port="8080"`) {
		t.Error("second scrape: expected port 8080 in graph edges (new service)")
	}
	if !strings.Contains(metrics2, `port="8080"`) {
		t.Error("second scrape: expected port 8080 in conntrack listen (new service)")
	}
}

// TestE2EServiceDisappears verifies that when a service stops listening,
// its metrics are cleaned up on the next scrape.
func TestE2EServiceDisappears(t *testing.T) {
	connSrc := &statefulConnectionSource{
		responses: [][]netgraph.Connection{
			// First scrape: ports 9090 and 8080
			{
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 8080, RemoteIP: "10.0.0.5", RemotePort: 50000, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 8080, RemoteIP: "10.0.0.5", RemotePort: 50000, State: "ESTABLISHED", Protocol: "tcp"},
			},
			// Second scrape: only port 9090 (8080 is gone)
			{
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
			},
		},
	}

	socketSrc := &statefulSocketSource{
		responses: [][]conntrack.SocketEntry{
			{
				{LocalIP: "0.0.0.0", LocalPort: 9090, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
				{LocalIP: "0.0.0.0", LocalPort: 8080, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 8080, RemoteIP: "10.0.0.5", RemotePort: 50000, State: "ESTABLISHED", Protocol: "tcp"},
			},
			{
				{LocalIP: "0.0.0.0", LocalPort: 9090, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
			},
		},
	}

	mockGraph := netgraph.NewWithSource(connSrc)
	mockConn := conntrack.NewWithSources(socketSrc, &mockConntrackSource{})

	addr, cancel := startCustomE2EServer(t, mockGraph, mockConn)
	defer cancel()

	// First scrape: both ports present.
	metrics1 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics1, `local_port="8080"`) {
		t.Error("first scrape: expected port 8080 in graph edges")
	}
	if !strings.Contains(metrics1, `port="8080"`) {
		t.Error("first scrape: expected port 8080 in conntrack listen")
	}

	// Second scrape: port 8080 should be gone.
	metrics2 := scrapeMetrics(t, addr)
	if strings.Contains(metrics2, `local_port="8080"`) {
		t.Error("second scrape: port 8080 should be gone from graph edges")
	}
	// Check conntrack listen metric for port 8080 is gone.
	for _, line := range strings.Split(metrics2, "\n") {
		if strings.HasPrefix(line, "network_port_listen{") && strings.Contains(line, `port="8080"`) {
			t.Errorf("second scrape: port 8080 should be gone from conntrack listen, found: %s", line)
		}
	}
}

// TestE2ENewPeerConnects verifies that when a new remote host connects to an
// existing service, it appears in the graph edges.
func TestE2ENewPeerConnects(t *testing.T) {
	connSrc := &statefulConnectionSource{
		responses: [][]netgraph.Connection{
			// First scrape: one remote host
			{
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
			},
			// Second scrape: two remote hosts
			{
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "LISTEN", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
				{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "10.0.0.5", RemotePort: 46000, State: "ESTABLISHED", Protocol: "tcp"},
			},
		},
	}

	mockGraph := netgraph.NewWithSource(connSrc)
	addr, cancel := startCustomE2EServer(t, mockGraph)
	defer cancel()

	// First scrape: only 192.168.1.20
	metrics1 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics1, `remote_host="192.168.1.20"`) {
		t.Error("first scrape: expected 192.168.1.20 in graph edges")
	}
	if strings.Contains(metrics1, `remote_host="10.0.0.5"`) {
		t.Error("first scrape: 10.0.0.5 should NOT be present yet")
	}

	// Second scrape: both hosts
	metrics2 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics2, `remote_host="192.168.1.20"`) {
		t.Error("second scrape: expected 192.168.1.20 in graph edges")
	}
	if !strings.Contains(metrics2, `remote_host="10.0.0.5"`) {
		t.Error("second scrape: expected 10.0.0.5 in graph edges (new peer)")
	}
}

// TestE2EInterfaceTopologyChange verifies that new interfaces (e.g. a veth
// added as a bridge member) appear in metrics on the next scrape.
func TestE2EInterfaceTopologyChange(t *testing.T) {
	linkLister := &statefulLinkLister{
		responses: [][]iface.LinkInfo{
			// First scrape: eth0 + br0
			{
				{Name: "eth0", Type: "physical", Driver: "ixgbe"},
				{Name: "br0", Type: "bridge", Driver: "bridge"},
			},
			// Second scrape: eth0 + br0 + veth1 as bridge member
			{
				{Name: "eth0", Type: "physical", Driver: "ixgbe"},
				{Name: "br0", Type: "bridge", Driver: "bridge"},
				{Name: "veth1", Type: "veth", MasterName: "br0", MasterType: "bridge"},
			},
		},
	}

	mockIface := iface.NewWithLister(linkLister)
	addr, cancel := startCustomE2EServer(t, mockIface)
	defer cancel()

	// First scrape: eth0 and br0 present, no veth1.
	metrics1 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics1, `device="eth0"`) {
		t.Error("first scrape: expected eth0")
	}
	if !strings.Contains(metrics1, `device="br0"`) {
		t.Error("first scrape: expected br0")
	}
	if strings.Contains(metrics1, `device="veth1"`) {
		t.Error("first scrape: veth1 should NOT be present yet")
	}
	if strings.Contains(metrics1, `network_bridge_member{bridge="br0",member="veth1"}`) {
		t.Error("first scrape: veth1 bridge membership should NOT be present yet")
	}

	// Second scrape: veth1 appears with bridge membership.
	metrics2 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics2, `device="veth1"`) {
		t.Error("second scrape: expected veth1 to appear")
	}
	if !strings.Contains(metrics2, `network_bridge_member{bridge="br0",member="veth1"}`) {
		t.Error("second scrape: expected veth1 bridge membership")
	}
}

// TestE2EARPTableChange verifies that ARP table changes (new entries,
// state changes) are reflected on the next scrape.
func TestE2EARPTableChange(t *testing.T) {
	neighLister := &statefulNeighborLister{
		responses: [][]arp.Neighbor{
			// First scrape: two entries
			{
				{IP: net.ParseIP("192.168.1.1"), MAC: net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, Device: "eth0", State: 0x02}, // REACHABLE
				{IP: net.ParseIP("192.168.1.2"), MAC: net.HardwareAddr{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}, Device: "eth0", State: 0x02}, // REACHABLE
			},
			// Second scrape: add new entry, change 192.168.1.2 to FAILED
			{
				{IP: net.ParseIP("192.168.1.1"), MAC: net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, Device: "eth0", State: 0x02}, // REACHABLE
				{IP: net.ParseIP("192.168.1.2"), MAC: net.HardwareAddr{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}, Device: "eth0", State: 0x20}, // FAILED
				{IP: net.ParseIP("192.168.1.3"), MAC: net.HardwareAddr{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}, Device: "eth0", State: 0x02}, // REACHABLE
			},
		},
	}

	mockArp := arp.NewWithLister(neighLister)
	addr, cancel := startCustomE2EServer(t, mockArp)
	defer cancel()

	// First scrape: two entries, both reachable.
	metrics1 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics1, `ip="192.168.1.1"`) {
		t.Error("first scrape: expected 192.168.1.1")
	}
	if !strings.Contains(metrics1, `ip="192.168.1.2"`) {
		t.Error("first scrape: expected 192.168.1.2")
	}
	if strings.Contains(metrics1, `ip="192.168.1.3"`) {
		t.Error("first scrape: 192.168.1.3 should NOT be present yet")
	}
	// Both should be reachable.
	count := strings.Count(metrics1, `state="reachable"`)
	if count != 2 {
		t.Errorf("first scrape: expected 2 reachable entries, got %d", count)
	}

	// Second scrape: new entry appears, state change reflected.
	metrics2 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics2, `ip="192.168.1.3"`) {
		t.Error("second scrape: expected 192.168.1.3 (new entry)")
	}

	// 192.168.1.2 should now be failed.
	for _, line := range strings.Split(metrics2, "\n") {
		if strings.Contains(line, `ip="192.168.1.2"`) && strings.HasPrefix(line, "network_arp_entry{") {
			if !strings.Contains(line, `state="failed"`) {
				t.Errorf("second scrape: expected 192.168.1.2 to be in failed state, got: %s", line)
			}
		}
	}

	// 192.168.1.1 should still be reachable.
	for _, line := range strings.Split(metrics2, "\n") {
		if strings.Contains(line, `ip="192.168.1.1"`) && strings.HasPrefix(line, "network_arp_entry{") {
			if !strings.Contains(line, `state="reachable"`) {
				t.Errorf("second scrape: expected 192.168.1.1 to remain reachable, got: %s", line)
			}
		}
	}
}

// Ensure sync import is used (needed by stateful mocks).
var _ sync.Mutex
