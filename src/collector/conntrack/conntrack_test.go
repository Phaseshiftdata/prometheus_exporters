package conntrack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/vishvananda/netlink"

	"github.com/phaseshiftdata/prometheus_exporters/src/procnet"
)

// Compile-time interface checks.
var (
	_ SocketSource    = (*procfsSocketSource)(nil)
	_ SocketSource    = (*mockSocketSource)(nil)
	_ ConntrackSource = (*netlinkConntrackSource)(nil)
	_ ConntrackSource = (*mockConntrackSource)(nil)
)

// mockSocketSource is a test double for SocketSource.
type mockSocketSource struct {
	sockets []SocketEntry
	err     error
}

func (m *mockSocketSource) ListSockets() ([]SocketEntry, error) {
	return m.sockets, m.err
}

// mockConntrackSource is a test double for ConntrackSource.
type mockConntrackSource struct {
	flows []ConntrackFlow
	err   error
}

func (m *mockConntrackSource) ListFlows() ([]ConntrackFlow, error) {
	return m.flows, m.err
}

func TestListeningPortDiscoveryTCP(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "0.0.0.0", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			{LocalIP: "0.0.0.0", LocalPort: 443, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
		},
	}
	flows := &mockConntrackSource{flows: []ConntrackFlow{}}
	c := NewWithSources(sockets, flows)

	expected := `
# HELP network_port_listen Presence of a listening port; value is always 1.
# TYPE network_port_listen gauge
network_port_listen{bind_address="0.0.0.0",port="80",protocol="tcp"} 1
network_port_listen{bind_address="0.0.0.0",port="443",protocol="tcp"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_port_listen"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestListeningPortDiscoveryUDP(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "0.0.0.0", LocalPort: 53, RemoteIP: "0.0.0.0", RemotePort: 0, State: "CLOSE", Protocol: "udp"},
		},
	}
	flows := &mockConntrackSource{flows: []ConntrackFlow{}}
	c := NewWithSources(sockets, flows)

	expected := `
# HELP network_port_listen Presence of a listening port; value is always 1.
# TYPE network_port_listen gauge
network_port_listen{bind_address="0.0.0.0",port="53",protocol="udp"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_port_listen"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestConnectionCountsByState(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "0.0.0.0", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.1", RemotePort: 40000, State: "ESTABLISHED", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.2", RemotePort: 40001, State: "ESTABLISHED", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.3", RemotePort: 40002, State: "TIME_WAIT", Protocol: "tcp"},
		},
	}
	flows := &mockConntrackSource{flows: []ConntrackFlow{}}
	c := NewWithSources(sockets, flows)

	expected := `
# HELP network_port_connections Number of connections per port, protocol, and state.
# TYPE network_port_connections gauge
network_port_connections{port="80",protocol="tcp",state="ESTABLISHED"} 2
network_port_connections{port="80",protocol="tcp",state="TIME_WAIT"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_port_connections"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestByteCountsFromConntrack(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "0.0.0.0", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
		},
	}
	flows := &mockConntrackSource{
		flows: []ConntrackFlow{
			{Protocol: "tcp", SrcPort: 40000, DstPort: 80, BytesIn: 1000, BytesOut: 5000},
			{Protocol: "tcp", SrcPort: 40001, DstPort: 80, BytesIn: 2000, BytesOut: 3000},
			// Flow to non-listening port — should be ignored.
			{Protocol: "tcp", SrcPort: 40002, DstPort: 443, BytesIn: 999, BytesOut: 999},
		},
	}
	c := NewWithSources(sockets, flows)

	expected := `
# HELP network_port_bytes_in Total inbound bytes per port from conntrack.
# TYPE network_port_bytes_in gauge
network_port_bytes_in{port="80",protocol="tcp"} 3000
# HELP network_port_bytes_out Total outbound bytes per port from conntrack.
# TYPE network_port_bytes_out gauge
network_port_bytes_out{port="80",protocol="tcp"} 8000
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_port_bytes_in", "network_port_bytes_out"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestConntrackUnavailable(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "0.0.0.0", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
		},
	}
	flows := &mockConntrackSource{err: fmt.Errorf("nf_conntrack_acct not enabled")}
	c := NewWithSources(sockets, flows)

	expected := `
# HELP network_conntrack_accounting_enabled Whether conntrack accounting is available (1) or not (0).
# TYPE network_conntrack_accounting_enabled gauge
network_conntrack_accounting_enabled 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_conntrack_accounting_enabled"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}

	// Byte metrics should not be emitted.
	countIn := testutil.CollectAndCount(c, "network_port_bytes_in")
	if countIn != 0 {
		t.Errorf("expected 0 bytes_in metrics when conntrack unavailable, got %d", countIn)
	}
	countOut := testutil.CollectAndCount(c, "network_port_bytes_out")
	if countOut != 0 {
		t.Errorf("expected 0 bytes_out metrics when conntrack unavailable, got %d", countOut)
	}
}

func TestConntrackAccountingEnabled(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "0.0.0.0", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
		},
	}
	flows := &mockConntrackSource{flows: []ConntrackFlow{}}
	c := NewWithSources(sockets, flows)

	expected := `
# HELP network_conntrack_accounting_enabled Whether conntrack accounting is available (1) or not (0).
# TYPE network_conntrack_accounting_enabled gauge
network_conntrack_accounting_enabled 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_conntrack_accounting_enabled"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestEmptySocketList(t *testing.T) {
	sockets := &mockSocketSource{sockets: []SocketEntry{}}
	flows := &mockConntrackSource{flows: []ConntrackFlow{}}
	c := NewWithSources(sockets, flows)

	// No listen, no connections, accounting enabled, no bytes.
	countListen := testutil.CollectAndCount(c, "network_port_listen")
	if countListen != 0 {
		t.Errorf("expected 0 listen metrics, got %d", countListen)
	}
	countConns := testutil.CollectAndCount(c, "network_port_connections")
	if countConns != 0 {
		t.Errorf("expected 0 connection metrics, got %d", countConns)
	}
}

func TestEmptyConntrack(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "0.0.0.0", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.1", RemotePort: 40000, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	flows := &mockConntrackSource{flows: []ConntrackFlow{}}
	c := NewWithSources(sockets, flows)

	// Byte metrics should not be emitted when there are no flows.
	countIn := testutil.CollectAndCount(c, "network_port_bytes_in")
	if countIn != 0 {
		t.Errorf("expected 0 bytes_in metrics, got %d", countIn)
	}
}

func TestMultiplePorts(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "0.0.0.0", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			{LocalIP: "0.0.0.0", LocalPort: 443, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.1", RemotePort: 40000, State: "ESTABLISHED", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.2", RemotePort: 40001, State: "ESTABLISHED", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.3", RemotePort: 40002, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	flows := &mockConntrackSource{
		flows: []ConntrackFlow{
			{Protocol: "tcp", SrcPort: 40000, DstPort: 80, BytesIn: 100, BytesOut: 200},
			{Protocol: "tcp", SrcPort: 40001, DstPort: 443, BytesIn: 300, BytesOut: 400},
		},
	}
	c := NewWithSources(sockets, flows)

	expected := `
# HELP network_port_connections Number of connections per port, protocol, and state.
# TYPE network_port_connections gauge
network_port_connections{port="80",protocol="tcp",state="ESTABLISHED"} 1
network_port_connections{port="443",protocol="tcp",state="ESTABLISHED"} 2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_port_connections"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}

	countListen := testutil.CollectAndCount(c, "network_port_listen")
	if countListen != 2 {
		t.Errorf("expected 2 listen metrics, got %d", countListen)
	}
}

func TestBindAddressOnListenMetric(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			{LocalIP: "192.168.1.1", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
		},
	}
	flows := &mockConntrackSource{flows: []ConntrackFlow{}}
	c := NewWithSources(sockets, flows)

	expected := `
# HELP network_port_listen Presence of a listening port; value is always 1.
# TYPE network_port_listen gauge
network_port_listen{bind_address="10.0.0.1",port="80",protocol="tcp"} 1
network_port_listen{bind_address="192.168.1.1",port="80",protocol="tcp"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_port_listen"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestSocketSourceError(t *testing.T) {
	sockets := &mockSocketSource{err: fmt.Errorf("procfs unavailable")}
	flows := &mockConntrackSource{flows: []ConntrackFlow{}}
	c := NewWithSources(sockets, flows)

	ch := make(chan prometheus.Metric, 10)
	c.Collect(ch)
	close(ch)

	m := <-ch
	if m == nil {
		t.Fatal("expected an invalid metric, got nil")
	}
}

func TestNilConntrackSource(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "0.0.0.0", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
		},
	}
	c := NewWithSources(sockets, nil)

	expected := `
# HELP network_conntrack_accounting_enabled Whether conntrack accounting is available (1) or not (0).
# TYPE network_conntrack_accounting_enabled gauge
network_conntrack_accounting_enabled 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_conntrack_accounting_enabled"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestName(t *testing.T) {
	c := NewWithSources(&mockSocketSource{}, &mockConntrackSource{})
	if c.Name() != "conntrack" {
		t.Fatalf("expected name 'conntrack', got %q", c.Name())
	}
}

func TestDescribe(t *testing.T) {
	c := NewWithSources(&mockSocketSource{}, &mockConntrackSource{})
	ch := make(chan *prometheus.Desc, 10)
	c.Describe(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	if count != 5 {
		t.Errorf("expected 5 descriptors, got %d", count)
	}
}

func TestNew(t *testing.T) {
	c := New("/proc")
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.Name() != "conntrack" {
		t.Errorf("expected name 'conntrack', got %q", c.Name())
	}
}

func TestEntryToSocket(t *testing.T) {
	e := procnet.Entry{
		LocalIP:    "10.0.0.1",
		LocalPort:  80,
		RemoteIP:   "192.168.1.1",
		RemotePort: 45000,
		State:      "ESTABLISHED",
		Protocol:   "tcp",
		TxQueue:    0,
		RxQueue:    0,
		UID:        1000,
		Inode:      12345,
	}
	s := entryToSocket(e)
	if s.LocalIP != "10.0.0.1" || s.LocalPort != 80 || s.RemoteIP != "192.168.1.1" || s.RemotePort != 45000 || s.State != "ESTABLISHED" || s.Protocol != "tcp" {
		t.Errorf("entryToSocket produced unexpected result: %+v", s)
	}
}

func TestProcfsSocketSourceTCPError(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No TCP files.
	src := &procfsSocketSource{procPath: dir}
	_, err := src.ListSockets()
	if err == nil {
		t.Error("expected error when TCP files are missing")
	}
}

func TestProcfsSocketSourceUDPError(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(tcp), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte("header\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No UDP files.
	src := &procfsSocketSource{procPath: dir}
	_, err := src.ListSockets()
	if err == nil {
		t.Error("expected error when UDP files are missing")
	}
}

func TestProcfsSocketSourceListSockets(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100A8C0:0050 0500A8C0:C000 01 00000000:00000000 00:00000000 00000000     0        0 23456 1 0000000000000000 100 0 0 10 0
`
	udp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 11111 2 0000000000000000 0
`
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(tcp), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte("header\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "udp"), []byte(udp), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "udp6"), []byte("header\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := &procfsSocketSource{procPath: dir}
	sockets, err := src.ListSockets()
	if err != nil {
		t.Fatalf("ListSockets: %v", err)
	}
	if len(sockets) != 3 {
		t.Fatalf("expected 3 sockets, got %d", len(sockets))
	}
	if sockets[0].Protocol != "tcp" || sockets[0].LocalPort != 80 || sockets[0].State != "LISTEN" {
		t.Errorf("unexpected first socket: %+v", sockets[0])
	}
	if sockets[2].Protocol != "udp" || sockets[2].LocalPort != 53 {
		t.Errorf("unexpected third socket: %+v", sockets[2])
	}
}

func TestConvertFlowEntries(t *testing.T) {
	entries := []conntrackFlowEntry{
		{Protocol: 6, SrcPort: 40000, DstPort: 80, FwdBytes: 5000, RevBytes: 1000},  // tcp
		{Protocol: 17, SrcPort: 50000, DstPort: 53, FwdBytes: 200, RevBytes: 100},    // udp
		{Protocol: 0, SrcPort: 0, DstPort: 0, FwdBytes: 0, RevBytes: 0},              // skip: proto 0
		{Protocol: 1, SrcPort: 0, DstPort: 0, FwdBytes: 0, RevBytes: 0},              // skip: icmp
		{Protocol: 132, SrcPort: 0, DstPort: 0, FwdBytes: 0, RevBytes: 0},            // skip: sctp
	}

	result := convertFlowEntries(entries)
	if len(result) != 2 {
		t.Fatalf("expected 2 flows, got %d", len(result))
	}
	if result[0].Protocol != "tcp" || result[0].DstPort != 80 || result[0].BytesOut != 5000 || result[0].BytesIn != 1000 {
		t.Errorf("unexpected TCP flow: %+v", result[0])
	}
	if result[1].Protocol != "udp" || result[1].DstPort != 53 {
		t.Errorf("unexpected UDP flow: %+v", result[1])
	}
}

func TestNetlinkConntrackSourceListFlows(t *testing.T) {
	origFn := conntrackTableListFn
	defer func() { conntrackTableListFn = origFn }()

	conntrackTableListFn = func(table netlink.ConntrackTableType, family netlink.InetFamily) ([]*netlink.ConntrackFlow, error) {
		return []*netlink.ConntrackFlow{
			{
				Forward: netlink.IPTuple{
					Protocol: 6,
					SrcPort:  40000,
					DstPort:  80,
					Bytes:    5000,
				},
				Reverse: netlink.IPTuple{
					Bytes: 1000,
				},
			},
			{
				Forward: netlink.IPTuple{
					Protocol: 17,
					SrcPort:  50000,
					DstPort:  53,
					Bytes:    200,
				},
				Reverse: netlink.IPTuple{
					Bytes: 100,
				},
			},
			{
				Forward: netlink.IPTuple{
					Protocol: 1, // ICMP - should be filtered
				},
			},
		}, nil
	}

	src := &netlinkConntrackSource{}
	flows, err := src.ListFlows()
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(flows) != 2 {
		t.Fatalf("expected 2 flows, got %d", len(flows))
	}
	if flows[0].Protocol != "tcp" || flows[0].DstPort != 80 {
		t.Errorf("unexpected first flow: %+v", flows[0])
	}
}

func TestNetlinkConntrackSourceListFlowsError(t *testing.T) {
	origFn := conntrackTableListFn
	defer func() { conntrackTableListFn = origFn }()

	conntrackTableListFn = func(table netlink.ConntrackTableType, family netlink.InetFamily) ([]*netlink.ConntrackFlow, error) {
		return nil, fmt.Errorf("conntrack unavailable")
	}

	src := &netlinkConntrackSource{}
	_, err := src.ListFlows()
	if err == nil {
		t.Error("expected error")
	}
}

func TestConvertFlowEntriesEmpty(t *testing.T) {
	result := convertFlowEntries(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 flows, got %d", len(result))
	}
}

func TestConnectionsNotOnListeningPortIgnored(t *testing.T) {
	sockets := &mockSocketSource{
		sockets: []SocketEntry{
			{LocalIP: "0.0.0.0", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			// Outbound connection — local port 54321 is not a listening port.
			{LocalIP: "10.0.0.1", LocalPort: 54321, RemoteIP: "8.8.8.8", RemotePort: 443, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	flows := &mockConntrackSource{flows: []ConntrackFlow{}}
	c := NewWithSources(sockets, flows)

	expected := `
# HELP network_port_connections Number of connections per port, protocol, and state.
# TYPE network_port_connections gauge
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_port_connections"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}
