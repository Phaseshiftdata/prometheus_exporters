package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector/arp"
	exporterpkg "github.com/phaseshiftdata/prometheus_exporters/src/exporter"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/conntrack"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/firewall"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/iface"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/ipsec"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/netgraph"
)

// TestE2EIpsecExporterMetrics starts a real HTTP server with mock-backed
// collectors (including IPsec) and verifies that scraping /metrics returns
// all expected metric families from both the network and IPsec collectors.
func TestE2EIpsecExporterMetrics(t *testing.T) {
	// Pick a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	reg := prometheus.NewRegistry()

	// Wire all collectors with mock backends.
	mockArp := arp.NewWithLister(&mockNeighborLister{})
	mockIface := iface.NewWithLister(&mockLinkLister{})
	mockNetgraph := netgraph.NewWithSource(&mockConnectionSource{})
	mockConntrack := conntrack.NewWithSources(&mockSocketSource{}, &mockConntrackSource{})
	mockFirewall := firewall.NewWithReader(&mockNftablesReader{})
	mockIpsec := ipsec.NewWithClient(&mockVICIClient{})

	for _, c := range []prometheus.Collector{mockArp, mockIface, mockNetgraph, mockConntrack, mockFirewall, mockIpsec} {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- exporterpkg.Serve(ctx, addr, "IPsec Exporter", reg) }()

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

	// Verify network collector metric families.
	networkFamilies := []string{
		"network_arp_entry",
		"network_interface_type",
		"network_graph_edge",
		"network_port_listen",
		"network_port_connections",
		"network_conntrack_accounting_enabled",
		"network_firewall_drop_packets_total",
		"network_firewall_drop_bytes_total",
	}
	for _, family := range networkFamilies {
		if !strings.Contains(metrics, family) {
			t.Errorf("expected network metric family %q not found in /metrics output", family)
		}
	}

	// Verify IPsec collector metric families.
	ipsecFamilies := []string{
		"ipsec_up",
		"ipsec_ike_sas",
		"ipsec_ike_sa_state",
		"ipsec_ike_sa_established_seconds",
		"ipsec_child_sa_state",
		"ipsec_child_sa_bytes_in",
		"ipsec_child_sa_bytes_out",
		"ipsec_child_sa_packets_in",
		"ipsec_child_sa_packets_out",
		"ipsec_child_sa_installed_seconds",
		"ipsec_uptime_seconds",
		"ipsec_workers_total",
		"ipsec_idle_workers",
		"ipsec_active_workers",
		"ipsec_queues",
	}
	for _, family := range ipsecFamilies {
		if !strings.Contains(metrics, family) {
			t.Errorf("expected IPsec metric family %q not found in /metrics output", family)
		}
	}

	// Verify specific label values are present.
	labelChecks := []string{
		`name="site-alpha"`,        // IKE SA name
		`remote_host="1.2.3.4"`,        // peer IP
		`local_ts="203.0.113.10/32"`,  // traffic selector
		`remote_ts="10.10.10.0/24"`,    // remote subnet
		`priority="critical"`,          // queue priority
	}
	for _, label := range labelChecks {
		if !strings.Contains(metrics, label) {
			t.Errorf("expected label %s not found in /metrics output", label)
		}
	}

	// Verify the landing page.
	resp2, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body2), "IPsec Exporter") {
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

// --- Mock implementations ---

type mockNeighborLister struct{}

func (m *mockNeighborLister) ListNeighbors() ([]arp.Neighbor, error) {
	return []arp.Neighbor{
		{IP: net.ParseIP("192.168.1.1"), MAC: net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, Device: "eth0", State: 0x02},
	}, nil
}

type mockLinkLister struct{}

func (m *mockLinkLister) ListLinks() ([]iface.LinkInfo, error) {
	return []iface.LinkInfo{
		{Name: "eth0", Type: "physical", Driver: "ixgbe"},
	}, nil
}

type mockConnectionSource struct{}

func (m *mockConnectionSource) ListConnections() ([]netgraph.Connection, error) {
	return []netgraph.Connection{
		{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "LISTEN", Protocol: "tcp"},
		{LocalIP: "192.168.1.10", LocalPort: 9090, RemoteIP: "192.168.1.20", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
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
	return []firewall.ChainPolicy{}, nil
}

type mockVICIClient struct{}

func (m *mockVICIClient) IsAvailable() bool { return true }

func (m *mockVICIClient) ListSAs() ([]ipsec.IKESAInfo, error) {
	return []ipsec.IKESAInfo{
		{
			Name:            "site-alpha",
			UID:             "1",
			RemoteHost:      "1.2.3.4",
			State:           2, // ESTABLISHED
			EstablishedSecs: 86400,
			ChildSAs: []ipsec.ChildSAInfo{
				{
					Name:          "net",
					UID:           "2",
					State:         3, // INSTALLED
					LocalTS:       "203.0.113.10/32",
					RemoteTS:      "10.10.10.0/24",
					BytesIn:       482910234,
					BytesOut:      129301022,
					PacketsIn:     3920122,
					PacketsOut:    1204811,
					InstalledSecs: 41200,
				},
			},
		},
	}, nil
}

func (m *mockVICIClient) GetStats() (ipsec.CharonStats, error) {
	return ipsec.CharonStats{
		Uptime:        1209600,
		Workers:       16,
		IdleWorkers:   14,
		ActiveWorkers: 2,
		Queues: map[string]int{
			"critical": 0,
			"high":     0,
			"medium":   1,
			"low":      0,
		},
		HalfOpenIKE: 0,
	}, nil
}

// --- Helpers for new e2e tests ---

// startE2EServer creates a full e2e server with the given IPsec mock and default
// network mocks. Returns the address and a cancel function.
func startE2EServer(t *testing.T, viciClient ipsec.VICIClient) (string, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	reg := prometheus.NewRegistry()
	mockArp := arp.NewWithLister(&mockNeighborLister{})
	mockIface := iface.NewWithLister(&mockLinkLister{})
	mockNetgraph := netgraph.NewWithSource(&mockConnectionSource{})
	mockConntrack := conntrack.NewWithSources(&mockSocketSource{}, &mockConntrackSource{})
	mockFirewall := firewall.NewWithReader(&mockNftablesReader{})
	mockIpsec := ipsec.NewWithClient(viciClient)

	for _, c := range []prometheus.Collector{mockArp, mockIface, mockNetgraph, mockConntrack, mockFirewall, mockIpsec} {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = exporterpkg.Serve(ctx, addr, "IPsec Exporter", reg) }()
	waitForServer(t, addr)
	return addr, cancel
}

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

// --- Tunnel-down mock ---

type mockVICIClientTunnelDown struct{}

func (m *mockVICIClientTunnelDown) IsAvailable() bool { return true }

func (m *mockVICIClientTunnelDown) ListSAs() ([]ipsec.IKESAInfo, error) {
	return []ipsec.IKESAInfo{
		{
			Name:            "site-alpha",
			UID:             "1",
			RemoteHost:      "1.2.3.4",
			State:           0, // CREATED — tunnel configured but not established
			EstablishedSecs: 0,
			ChildSAs:        nil, // no child SAs when tunnel is down
		},
	}, nil
}

func (m *mockVICIClientTunnelDown) GetStats() (ipsec.CharonStats, error) {
	return ipsec.CharonStats{
		Uptime: 3600, Workers: 16, IdleWorkers: 16, ActiveWorkers: 0,
		Queues:  map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0},
		HalfOpenIKE: 0,
	}, nil
}

// TestE2ETunnelDown verifies metrics when an IKE SA is configured but not
// established (state CREATED=0) and no child SAs are present.
func TestE2ETunnelDown(t *testing.T) {
	addr, cancel := startE2EServer(t, &mockVICIClientTunnelDown{})
	defer cancel()

	metrics := scrapeMetrics(t, addr)

	// ipsec_up should be 1 (VICI is reachable).
	if !strings.Contains(metrics, "ipsec_up 1") {
		t.Error("expected ipsec_up 1")
	}

	// IKE SA state should be 0 (CREATED).
	expected := `ipsec_ike_sa_state{name="site-alpha",remote_host="1.2.3.4",uid="1"} 0`
	if !strings.Contains(metrics, expected) {
		t.Errorf("expected IKE SA state 0, not found in metrics.\nLooking for: %s", expected)
	}

	// No child SA metrics should be present (tunnel is down, no traffic).
	childMetrics := []string{
		"ipsec_child_sa_bytes_in",
		"ipsec_child_sa_bytes_out",
		"ipsec_child_sa_packets_in",
		"ipsec_child_sa_packets_out",
	}
	for _, m := range childMetrics {
		// Check there are no actual metric lines (HELP/TYPE lines are okay).
		for _, line := range strings.Split(metrics, "\n") {
			if strings.HasPrefix(line, m+"{") {
				t.Errorf("expected no child SA metric lines, but found: %s", line)
			}
		}
	}
}

// --- Tunnel-flap mock ---

type mockVICIClientTunnelFlap struct{}

func (m *mockVICIClientTunnelFlap) IsAvailable() bool { return true }

func (m *mockVICIClientTunnelFlap) ListSAs() ([]ipsec.IKESAInfo, error) {
	// Two IKE SAs for the same connection name but different UIDs.
	// This is what happens during a rekey/flap.
	return []ipsec.IKESAInfo{
		{
			Name:            "site-alpha",
			UID:             "42",
			RemoteHost:      "1.2.3.4",
			State:           5, // REKEYED — old SA
			EstablishedSecs: 86400,
			ChildSAs: []ipsec.ChildSAInfo{
				{
					Name: "net", UID: "100", State: 6, // REKEYED
					LocalTS: "203.0.113.10/32", RemoteTS: "10.10.10.0/24",
					BytesIn: 1000000, BytesOut: 500000,
					PacketsIn: 10000, PacketsOut: 5000,
					InstalledSecs: 86000,
				},
			},
		},
		{
			Name:            "site-alpha",
			UID:             "43",
			RemoteHost:      "1.2.3.4",
			State:           2, // ESTABLISHED — new SA
			EstablishedSecs: 30,
			ChildSAs: []ipsec.ChildSAInfo{
				{
					Name: "net", UID: "101", State: 3, // INSTALLED
					LocalTS: "203.0.113.10/32", RemoteTS: "10.10.10.0/24",
					BytesIn: 1024, BytesOut: 512,
					PacketsIn: 10, PacketsOut: 5,
					InstalledSecs: 25,
				},
			},
		},
	}, nil
}

func (m *mockVICIClientTunnelFlap) GetStats() (ipsec.CharonStats, error) {
	return ipsec.CharonStats{
		Uptime: 90000, Workers: 16, IdleWorkers: 14, ActiveWorkers: 2,
		Queues:  map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0},
		HalfOpenIKE: 0,
	}, nil
}

// TestE2ETunnelFlap verifies that during a rekey/flap, both the old (REKEYED)
// and new (ESTABLISHED) IKE SAs appear with distinct UIDs and both have
// child SA byte counters present.
func TestE2ETunnelFlap(t *testing.T) {
	addr, cancel := startE2EServer(t, &mockVICIClientTunnelFlap{})
	defer cancel()

	metrics := scrapeMetrics(t, addr)

	// Both IKE SAs should appear with distinct UIDs.
	oldSA := `ipsec_ike_sa_state{name="site-alpha",remote_host="1.2.3.4",uid="42"} 5`
	newSA := `ipsec_ike_sa_state{name="site-alpha",remote_host="1.2.3.4",uid="43"} 2`
	if !strings.Contains(metrics, oldSA) {
		t.Errorf("old SA (uid=42, state=5) not found in metrics")
	}
	if !strings.Contains(metrics, newSA) {
		t.Errorf("new SA (uid=43, state=2) not found in metrics")
	}

	// ipsec_ike_sas should be 2.
	if !strings.Contains(metrics, "ipsec_ike_sas 2") {
		t.Error("expected ipsec_ike_sas 2")
	}

	// Both child SAs should have byte counters.
	oldChildBytes := `ipsec_child_sa_bytes_in{ike_sa_name="site-alpha",local_ts="203.0.113.10/32",name="net",remote_host="1.2.3.4",remote_ts="10.10.10.0/24",uid="100"} 1e+06`
	newChildBytes := `ipsec_child_sa_bytes_in{ike_sa_name="site-alpha",local_ts="203.0.113.10/32",name="net",remote_host="1.2.3.4",remote_ts="10.10.10.0/24",uid="101"} 1024`

	if !strings.Contains(metrics, `uid="100"`) {
		t.Error("old child SA (uid=100) not found in metrics")
	}
	if !strings.Contains(metrics, `uid="101"`) {
		t.Error("new child SA (uid=101) not found in metrics")
	}

	// Verify byte counters for both are present — check by uid.
	_ = oldChildBytes
	_ = newChildBytes
	for _, uid := range []string{"100", "101"} {
		for _, metric := range []string{"ipsec_child_sa_bytes_in", "ipsec_child_sa_bytes_out", "ipsec_child_sa_packets_in", "ipsec_child_sa_packets_out"} {
			pattern := fmt.Sprintf(`%s{`, metric)
			found := false
			for _, line := range strings.Split(metrics, "\n") {
				if strings.HasPrefix(line, pattern) && strings.Contains(line, fmt.Sprintf(`uid="%s"`, uid)) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %s with uid=%s not found", metric, uid)
			}
		}
	}

	// Both child SA states: old=6 (REKEYED), new=3 (INSTALLED).
	if !strings.Contains(metrics, `ipsec_child_sa_state{ike_sa_name="site-alpha",local_ts="203.0.113.10/32",name="net",remote_host="1.2.3.4",remote_ts="10.10.10.0/24",uid="100"} 6`) {
		t.Error("old child SA state (6=REKEYED) not found")
	}
	if !strings.Contains(metrics, `ipsec_child_sa_state{ike_sa_name="site-alpha",local_ts="203.0.113.10/32",name="net",remote_host="1.2.3.4",remote_ts="10.10.10.0/24",uid="101"} 3`) {
		t.Error("new child SA state (3=INSTALLED) not found")
	}
}

// --- Partial failure mock (ListSAs succeeds, GetStats fails) ---

type mockVICIClientPartialFailure struct{}

func (m *mockVICIClientPartialFailure) IsAvailable() bool { return true }

func (m *mockVICIClientPartialFailure) ListSAs() ([]ipsec.IKESAInfo, error) {
	return []ipsec.IKESAInfo{
		{
			Name: "site-alpha", UID: "1", RemoteHost: "1.2.3.4",
			State: 2, EstablishedSecs: 86400,
			ChildSAs: []ipsec.ChildSAInfo{
				{
					Name: "net", UID: "2", State: 3,
					LocalTS: "203.0.113.10/32", RemoteTS: "10.10.10.0/24",
					BytesIn: 100, BytesOut: 200, PacketsIn: 1, PacketsOut: 2,
					InstalledSecs: 1000,
				},
			},
		},
	}, nil
}

func (m *mockVICIClientPartialFailure) GetStats() (ipsec.CharonStats, error) {
	return ipsec.CharonStats{}, fmt.Errorf("vici stats: connection reset by peer")
}

// TestE2EPartialFailure verifies that when ListSAs succeeds but GetStats
// returns an error, SA metrics are still present but charon health metrics
// are absent. ipsec_up should be 1 since VICI is reachable.
//
// We use a custom server with ContinueOnError handling because the default
// promhttp handler returns HTTP 500 when any collector emits an InvalidMetric.
func TestE2EPartialFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	reg := prometheus.NewRegistry()
	mockIpsec := ipsec.NewWithClient(&mockVICIClientPartialFailure{})
	if err := reg.Register(mockIpsec); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Use ContinueOnError so the handler still returns 200 with partial data
	// when GetStats fails and emits an InvalidMetric.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	}))

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	go func() { srv.ListenAndServe() }()

	waitForServer(t, addr)

	metrics := scrapeMetrics(t, addr)

	// ipsec_up should be 1.
	if !strings.Contains(metrics, "ipsec_up 1") {
		t.Error("expected ipsec_up 1")
	}

	// SA metrics should be present.
	if !strings.Contains(metrics, `ipsec_ike_sa_state{name="site-alpha"`) {
		t.Error("expected IKE SA state metric to be present")
	}
	if !strings.Contains(metrics, `ipsec_child_sa_bytes_in{`) {
		t.Error("expected child SA bytes_in metric to be present")
	}

	// Charon health metrics should be absent (stats call failed, which causes
	// an InvalidMetric to be emitted for ipsec_uptime_seconds, so we won't
	// see a clean ipsec_uptime_seconds gauge line).
	// The collector returns an InvalidMetric on stats error, so
	// ipsec_workers_total and ipsec_idle_workers won't appear as normal gauges.
	charonMetrics := []string{
		"ipsec_workers_total",
		"ipsec_idle_workers",
		"ipsec_active_workers",
		"ipsec_queues",
	}
	for _, m := range charonMetrics {
		for _, line := range strings.Split(metrics, "\n") {
			if strings.HasPrefix(line, m+"{") || (strings.HasPrefix(line, m+" ") && !strings.HasPrefix(line, "# ")) {
				t.Errorf("expected charon metric %q to be absent, but found: %s", m, line)
			}
		}
	}
}

// --- Complete VICI failure mock ---

type mockVICIClientUnavailable struct{}

func (m *mockVICIClientUnavailable) IsAvailable() bool { return false }

func (m *mockVICIClientUnavailable) ListSAs() ([]ipsec.IKESAInfo, error) {
	return nil, fmt.Errorf("not available")
}

func (m *mockVICIClientUnavailable) GetStats() (ipsec.CharonStats, error) {
	return ipsec.CharonStats{}, fmt.Errorf("not available")
}

// TestE2ECompleteVICIFailure verifies that when VICI is completely unavailable,
// ipsec_up=0, no other ipsec_* metrics exist, but network_* metrics are still
// present and healthy.
func TestE2ECompleteVICIFailure(t *testing.T) {
	addr, cancel := startE2EServer(t, &mockVICIClientUnavailable{})
	defer cancel()

	metrics := scrapeMetrics(t, addr)

	// ipsec_up should be 0.
	if !strings.Contains(metrics, "ipsec_up 0") {
		t.Error("expected ipsec_up 0")
	}

	// No other ipsec_* metric lines should be present (except HELP/TYPE for ipsec_up).
	for _, line := range strings.Split(metrics, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "ipsec_") && !strings.HasPrefix(trimmed, "ipsec_up ") {
			t.Errorf("unexpected ipsec metric when VICI is down: %s", trimmed)
		}
	}

	// Network metrics should still be present and healthy.
	networkFamilies := []string{
		"network_arp_entry",
		"network_interface_type",
		"network_graph_edge",
		"network_port_listen",
		"network_port_connections",
		"network_conntrack_accounting_enabled",
		"network_firewall_drop_packets_total",
		"network_firewall_drop_bytes_total",
	}
	for _, family := range networkFamilies {
		if !strings.Contains(metrics, family) {
			t.Errorf("expected network metric family %q to still be present when VICI is down", family)
		}
	}
}

// --- Tunnel-down with CONNECTING state ---

type mockVICIClientConnecting struct{}

func (m *mockVICIClientConnecting) IsAvailable() bool { return true }

func (m *mockVICIClientConnecting) ListSAs() ([]ipsec.IKESAInfo, error) {
	return []ipsec.IKESAInfo{
		{
			Name:            "site-alpha",
			UID:             "5",
			RemoteHost:      "1.2.3.4",
			State:           1, // CONNECTING
			EstablishedSecs: 0,
			ChildSAs: []ipsec.ChildSAInfo{
				{
					Name: "net", UID: "6", State: 0, // CREATED
					LocalTS: "203.0.113.10/32", RemoteTS: "10.10.10.0/24",
					BytesIn: 0, BytesOut: 0, PacketsIn: 0, PacketsOut: 0,
					InstalledSecs: 0,
				},
			},
		},
	}, nil
}

func (m *mockVICIClientConnecting) GetStats() (ipsec.CharonStats, error) {
	return ipsec.CharonStats{
		Uptime: 100, Workers: 16, IdleWorkers: 15, ActiveWorkers: 1,
		Queues:  map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0},
		HalfOpenIKE: 1,
	}, nil
}

// TestE2ETunnelDownConnecting verifies that state=1 (CONNECTING) with a child
// SA in state=0 (CREATED) shows the down/connecting state correctly.
func TestE2ETunnelDownConnecting(t *testing.T) {
	addr, cancel := startE2EServer(t, &mockVICIClientConnecting{})
	defer cancel()

	metrics := scrapeMetrics(t, addr)

	// IKE SA state should be 1 (CONNECTING).
	if !strings.Contains(metrics, `ipsec_ike_sa_state{name="site-alpha",remote_host="1.2.3.4",uid="5"} 1`) {
		t.Error("expected IKE SA state 1 (CONNECTING)")
	}

	// Child SA state should be 0 (CREATED).
	if !strings.Contains(metrics, `uid="6"} 0`) {
		t.Error("expected child SA state 0 (CREATED)")
	}

	// Byte counters should be 0 (no traffic when tunnel is connecting).
	for _, line := range strings.Split(metrics, "\n") {
		if strings.HasPrefix(line, "ipsec_child_sa_bytes_in{") && strings.Contains(line, `uid="6"`) {
			if !strings.HasSuffix(line, " 0") {
				t.Errorf("expected 0 bytes_in during CONNECTING, got: %s", line)
			}
		}
	}
}

// --- Stateful mock for multi-scrape scenarios ---

// statefulVICIClient returns different data based on an atomic call counter.
type statefulVICIClient struct {
	callCount atomic.Int64
	available atomic.Bool
	saSlice   [][]ipsec.IKESAInfo
	statsErr  []error
}

func (m *statefulVICIClient) IsAvailable() bool { return m.available.Load() }

func (m *statefulVICIClient) ListSAs() ([]ipsec.IKESAInfo, error) {
	idx := int(m.callCount.Add(1)) - 1
	if idx >= len(m.saSlice) {
		idx = len(m.saSlice) - 1
	}
	return m.saSlice[idx], nil
}

func (m *statefulVICIClient) GetStats() (ipsec.CharonStats, error) {
	idx := int(m.callCount.Load()) - 1 // use same index as the preceding ListSAs
	if idx < 0 {
		idx = 0
	}
	if idx < len(m.statsErr) && m.statsErr[idx] != nil {
		return ipsec.CharonStats{}, m.statsErr[idx]
	}
	return ipsec.CharonStats{
		Uptime: 1000, Workers: 16, IdleWorkers: 14, ActiveWorkers: 2,
		Queues:  map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0},
		HalfOpenIKE: 0,
	}, nil
}

// --- Tunnel autodiscovery tests ---

// TestE2ENewTunnelAppears verifies that when a new IPsec tunnel is
// established between scrapes, it appears in the metrics without restart.
func newAvailableStatefulClient(sas [][]ipsec.IKESAInfo) *statefulVICIClient {
	c := &statefulVICIClient{saSlice: sas}
	c.available.Store(true)
	return c
}

func TestE2ENewTunnelAppears(t *testing.T) {
	client := newAvailableStatefulClient([][]ipsec.IKESAInfo{
			// First scrape: one tunnel
			{
				{
					Name: "site-alpha", UID: "1", RemoteHost: "1.2.3.4",
					State: 2, EstablishedSecs: 86400,
					ChildSAs: []ipsec.ChildSAInfo{
						{Name: "net", UID: "10", State: 3, LocalTS: "203.0.113.10/32", RemoteTS: "10.10.10.0/24",
							BytesIn: 1000, BytesOut: 500, PacketsIn: 100, PacketsOut: 50, InstalledSecs: 3600},
					},
				},
			},
			// Second scrape: two tunnels (new one appeared)
			{
				{
					Name: "site-alpha", UID: "1", RemoteHost: "1.2.3.4",
					State: 2, EstablishedSecs: 86430,
					ChildSAs: []ipsec.ChildSAInfo{
						{Name: "net", UID: "10", State: 3, LocalTS: "203.0.113.10/32", RemoteTS: "10.10.10.0/24",
							BytesIn: 2000, BytesOut: 1000, PacketsIn: 200, PacketsOut: 100, InstalledSecs: 3630},
					},
				},
				{
					Name: "site-bravo", UID: "2", RemoteHost: "5.6.7.8",
					State: 2, EstablishedSecs: 30,
					ChildSAs: []ipsec.ChildSAInfo{
						{Name: "net", UID: "20", State: 3, LocalTS: "203.0.113.10/32", RemoteTS: "10.20.20.0/24",
							BytesIn: 512, BytesOut: 256, PacketsIn: 10, PacketsOut: 5, InstalledSecs: 25},
					},
				},
		},
	})

	addr, cancel := startE2EServer(t, client)
	defer cancel()

	// First scrape: only site-alpha should be present.
	metrics1 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics1, `name="site-alpha"`) {
		t.Error("first scrape: expected site-alpha")
	}
	if strings.Contains(metrics1, `name="site-bravo"`) {
		t.Error("first scrape: site-bravo should NOT be present yet")
	}
	if !strings.Contains(metrics1, "ipsec_ike_sas 1") {
		t.Error("first scrape: expected ipsec_ike_sas 1")
	}

	// Second scrape: both tunnels should appear.
	metrics2 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics2, `name="site-alpha"`) {
		t.Error("second scrape: expected site-alpha")
	}
	if !strings.Contains(metrics2, `name="site-bravo"`) {
		t.Error("second scrape: expected site-bravo (auto-discovered)")
	}
	if !strings.Contains(metrics2, "ipsec_ike_sas 2") {
		t.Error("second scrape: expected ipsec_ike_sas 2")
	}
	// Verify the new tunnel has child SA metrics.
	if !strings.Contains(metrics2, `remote_ts="10.20.20.0/24"`) {
		t.Error("second scrape: expected child SA traffic selector for new tunnel")
	}
}

// TestE2ETunnelRemoved verifies that when an IPsec tunnel is removed
// between scrapes, it disappears from the metrics.
func TestE2ETunnelRemoved(t *testing.T) {
	client := newAvailableStatefulClient([][]ipsec.IKESAInfo{
			// First scrape: two tunnels
			{
				{
					Name: "site-alpha", UID: "1", RemoteHost: "1.2.3.4",
					State: 2, EstablishedSecs: 86400,
					ChildSAs: []ipsec.ChildSAInfo{
						{Name: "net", UID: "10", State: 3, LocalTS: "203.0.113.10/32", RemoteTS: "10.10.10.0/24",
							BytesIn: 1000, BytesOut: 500, PacketsIn: 100, PacketsOut: 50, InstalledSecs: 3600},
					},
				},
				{
					Name: "site-bravo", UID: "2", RemoteHost: "5.6.7.8",
					State: 2, EstablishedSecs: 7200,
					ChildSAs: []ipsec.ChildSAInfo{
						{Name: "net", UID: "20", State: 3, LocalTS: "203.0.113.10/32", RemoteTS: "10.20.20.0/24",
							BytesIn: 50000, BytesOut: 25000, PacketsIn: 5000, PacketsOut: 2500, InstalledSecs: 7100},
					},
				},
			},
			// Second scrape: site-bravo removed (connection deleted or peer unreachable)
			{
				{
					Name: "site-alpha", UID: "1", RemoteHost: "1.2.3.4",
					State: 2, EstablishedSecs: 86430,
					ChildSAs: []ipsec.ChildSAInfo{
						{Name: "net", UID: "10", State: 3, LocalTS: "203.0.113.10/32", RemoteTS: "10.10.10.0/24",
							BytesIn: 2000, BytesOut: 1000, PacketsIn: 200, PacketsOut: 100, InstalledSecs: 3630},
					},
				},
			},
	})

	addr, cancel := startE2EServer(t, client)
	defer cancel()

	// First scrape: both tunnels present.
	metrics1 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics1, `name="site-alpha"`) {
		t.Error("first scrape: expected site-alpha")
	}
	if !strings.Contains(metrics1, `name="site-bravo"`) {
		t.Error("first scrape: expected site-bravo")
	}
	if !strings.Contains(metrics1, "ipsec_ike_sas 2") {
		t.Error("first scrape: expected ipsec_ike_sas 2")
	}

	// Second scrape: only site-alpha remains.
	metrics2 := scrapeMetrics(t, addr)
	if !strings.Contains(metrics2, `name="site-alpha"`) {
		t.Error("second scrape: expected site-alpha")
	}
	if strings.Contains(metrics2, `name="site-bravo"`) {
		t.Error("second scrape: site-bravo should be gone (tunnel removed)")
	}
	if !strings.Contains(metrics2, "ipsec_ike_sas 1") {
		t.Error("second scrape: expected ipsec_ike_sas 1")
	}
	// Verify the removed tunnel's traffic selectors are gone.
	if strings.Contains(metrics2, `remote_ts="10.20.20.0/24"`) {
		t.Error("second scrape: removed tunnel's traffic selectors should be gone")
	}
}

// TestE2ETunnelFlapThenRecover verifies a full flap cycle: tunnel goes down
// (DELETING), disappears, then comes back with a new UID.
func TestE2ETunnelFlapThenRecover(t *testing.T) {
	client := newAvailableStatefulClient([][]ipsec.IKESAInfo{
			// Scrape 1: tunnel healthy
			{
				{
					Name: "site-charlie", UID: "50", RemoteHost: "9.8.7.6",
					State: 2, EstablishedSecs: 43200,
					ChildSAs: []ipsec.ChildSAInfo{
						{Name: "net-net", UID: "500", State: 3, LocalTS: "10.99.0.1/32", RemoteTS: "10.99.1.0/24",
							BytesIn: 100000, BytesOut: 50000, PacketsIn: 10000, PacketsOut: 5000, InstalledSecs: 43000},
					},
				},
			},
			// Scrape 2: tunnel is DELETING (flap in progress)
			{
				{
					Name: "site-charlie", UID: "50", RemoteHost: "9.8.7.6",
					State: 6, EstablishedSecs: 43230, // DELETING
					ChildSAs: []ipsec.ChildSAInfo{
						{Name: "net-net", UID: "500", State: 8, LocalTS: "10.99.0.1/32", RemoteTS: "10.99.1.0/24",
							BytesIn: 100500, BytesOut: 50250, PacketsIn: 10050, PacketsOut: 5025, InstalledSecs: 43030},
					},
				},
			},
			// Scrape 3: tunnel recovered with new UID
			{
				{
					Name: "site-charlie", UID: "51", RemoteHost: "9.8.7.6",
					State: 2, EstablishedSecs: 5, // newly ESTABLISHED
					ChildSAs: []ipsec.ChildSAInfo{
						{Name: "net-net", UID: "501", State: 3, LocalTS: "10.99.0.1/32", RemoteTS: "10.99.1.0/24",
							BytesIn: 100, BytesOut: 50, PacketsIn: 2, PacketsOut: 1, InstalledSecs: 3},
					},
				},
			},
	})

	addr, cancel := startE2EServer(t, client)
	defer cancel()

	// Scrape 1: healthy tunnel.
	m1 := scrapeMetrics(t, addr)
	if !strings.Contains(m1, `ipsec_ike_sa_state{name="site-charlie",remote_host="9.8.7.6",uid="50"} 2`) {
		t.Error("scrape 1: expected site-charlie uid=50 state=2 (ESTABLISHED)")
	}

	// Scrape 2: tunnel DELETING.
	m2 := scrapeMetrics(t, addr)
	if !strings.Contains(m2, `uid="50"`) {
		t.Error("scrape 2: expected uid=50 still present (DELETING)")
	}
	// Check state is 6 (DELETING).
	if !strings.Contains(m2, `ipsec_ike_sa_state{name="site-charlie",remote_host="9.8.7.6",uid="50"} 6`) {
		t.Error("scrape 2: expected site-charlie state=6 (DELETING)")
	}

	// Scrape 3: recovered with new UID.
	m3 := scrapeMetrics(t, addr)
	if strings.Contains(m3, `uid="50"`) {
		t.Error("scrape 3: old uid=50 should be gone")
	}
	if !strings.Contains(m3, `ipsec_ike_sa_state{name="site-charlie",remote_host="9.8.7.6",uid="51"} 2`) {
		t.Error("scrape 3: expected site-charlie uid=51 state=2 (ESTABLISHED, recovered)")
	}
	// Byte counters reset with new SA.
	if !strings.Contains(m3, `uid="501"`) {
		t.Error("scrape 3: expected new child SA uid=501")
	}
}
