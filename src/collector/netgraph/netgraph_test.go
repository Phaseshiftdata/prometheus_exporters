package netgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/phaseshiftdata/prometheus_exporters/src/procnet"
)

// Compile-time interface checks.
var (
	_ ConnectionSource = (*procfsSource)(nil)
	_ ConnectionSource = (*mockSource)(nil)
)

// mockSource is a test double for ConnectionSource.
type mockSource struct {
	connections []Connection
	err         error
}

func (m *mockSource) ListConnections() ([]Connection, error) {
	return m.connections, m.err
}

func TestBasicInbound(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.5", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	c := NewWithSource(src)

	expected := `
# HELP network_graph_edge Presence indicator for a network topology edge; value is always 1.
# TYPE network_graph_edge gauge
network_graph_edge{direction="inbound",local_port="80",remote_host="192.168.1.5"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_graph_edge"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestBasicOutbound(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 54321, RemoteIP: "203.0.113.10", RemotePort: 443, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	c := NewWithSource(src)

	expected := `
# HELP network_graph_edge Presence indicator for a network topology edge; value is always 1.
# TYPE network_graph_edge gauge
network_graph_edge{direction="outbound",local_port="443",remote_host="203.0.113.10"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_graph_edge"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestDeduplication(t *testing.T) {
	conns := []Connection{
		{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
	}
	// 50 connections from the same remote host to the same local port.
	for i := 0; i < 50; i++ {
		conns = append(conns, Connection{
			LocalIP: "10.0.0.1", LocalPort: 80,
			RemoteIP: "192.168.1.5", RemotePort: uint16(40000 + i),
			State: "ESTABLISHED", Protocol: "tcp",
		})
	}
	src := &mockSource{connections: conns}
	c := NewWithSource(src)

	count := testutil.CollectAndCount(c, "network_graph_edge")
	if count != 1 {
		t.Errorf("expected 1 deduplicated edge, got %d", count)
	}
}

func TestLoopbackFiltering(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "127.0.0.1", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	c := NewWithSource(src)

	expected := `
# HELP network_graph_edge Presence indicator for a network topology edge; value is always 1.
# TYPE network_graph_edge gauge
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_graph_edge"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestZeroIPFiltering(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 8080, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			// Connection from 0.0.0.0 should be filtered out.
			{LocalIP: "10.0.0.1", LocalPort: 8080, RemoteIP: "0.0.0.0", RemotePort: 50000, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	c := NewWithSource(src)

	expected := `
# HELP network_graph_edge Presence indicator for a network topology edge; value is always 1.
# TYPE network_graph_edge gauge
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_graph_edge"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestLocalIPLoopbackFiltering(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			// Outbound connection with local IP 127.0.0.1 should be filtered.
			{LocalIP: "127.0.0.1", LocalPort: 54321, RemoteIP: "10.0.0.5", RemotePort: 443, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	c := NewWithSource(src)

	expected := `
# HELP network_graph_edge Presence indicator for a network topology edge; value is always 1.
# TYPE network_graph_edge gauge
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_graph_edge"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestLocalIPZeroFiltering(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			// Outbound connection with local IP 0.0.0.0 should be filtered.
			{LocalIP: "0.0.0.0", LocalPort: 54321, RemoteIP: "10.0.0.5", RemotePort: 443, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	c := NewWithSource(src)

	expected := `
# HELP network_graph_edge Presence indicator for a network topology edge; value is always 1.
# TYPE network_graph_edge gauge
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_graph_edge"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestMixedInboundOutbound(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			// Listen on port 80.
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			// Inbound to port 80.
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.5", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
			// Outbound to port 443.
			{LocalIP: "10.0.0.1", LocalPort: 54321, RemoteIP: "203.0.113.10", RemotePort: 443, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	c := NewWithSource(src)

	expected := `
# HELP network_graph_edge Presence indicator for a network topology edge; value is always 1.
# TYPE network_graph_edge gauge
network_graph_edge{direction="inbound",local_port="80",remote_host="192.168.1.5"} 1
network_graph_edge{direction="outbound",local_port="443",remote_host="203.0.113.10"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_graph_edge"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestUDPConnections(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			// UDP listening socket.
			{LocalIP: "10.0.0.1", LocalPort: 53, RemoteIP: "0.0.0.0", RemotePort: 0, State: "CLOSE", Protocol: "udp"},
			// UDP "connection" to our listening port.
			{LocalIP: "10.0.0.1", LocalPort: 53, RemoteIP: "192.168.1.100", RemotePort: 40000, State: "ESTABLISHED", Protocol: "udp"},
			// UDP outbound.
			{LocalIP: "10.0.0.1", LocalPort: 39999, RemoteIP: "8.8.8.8", RemotePort: 53, State: "ESTABLISHED", Protocol: "udp"},
		},
	}
	c := NewWithSource(src)

	expected := `
# HELP network_graph_edge Presence indicator for a network topology edge; value is always 1.
# TYPE network_graph_edge gauge
network_graph_edge{direction="inbound",local_port="53",remote_host="192.168.1.100"} 1
network_graph_edge{direction="outbound",local_port="53",remote_host="8.8.8.8"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_graph_edge"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestEmptyConnectionList(t *testing.T) {
	src := &mockSource{connections: []Connection{}}
	c := NewWithSource(src)

	expected := `
# HELP network_graph_edge Presence indicator for a network topology edge; value is always 1.
# TYPE network_graph_edge gauge
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_graph_edge"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestSourceError(t *testing.T) {
	src := &mockSource{err: fmt.Errorf("procfs unavailable")}
	c := NewWithSource(src)

	ch := make(chan prometheus.Metric, 1)
	c.Collect(ch)
	close(ch)

	m := <-ch
	if m == nil {
		t.Fatal("expected an invalid metric, got nil")
	}
}

func TestMultipleRemoteHostsSameLocalPort(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.1", RemotePort: 45000, State: "ESTABLISHED", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.2", RemotePort: 45001, State: "ESTABLISHED", Protocol: "tcp"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.3", RemotePort: 45002, State: "ESTABLISHED", Protocol: "tcp"},
		},
	}
	c := NewWithSource(src)

	count := testutil.CollectAndCount(c, "network_graph_edge")
	if count != 3 {
		t.Errorf("expected 3 edges (one per remote host), got %d", count)
	}
}

func TestName(t *testing.T) {
	c := NewWithSource(&mockSource{})
	if c.Name() != "netgraph" {
		t.Fatalf("expected name 'netgraph', got %q", c.Name())
	}
}

func TestDescribe(t *testing.T) {
	c := NewWithSource(&mockSource{})
	ch := make(chan *prometheus.Desc, 2)
	c.Describe(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	if count != 2 {
		t.Fatalf("expected 2 descriptors, got %d", count)
	}
}

func TestNewWithFakeProcfs(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100A8C0:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
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

	c := New(dir)
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.Name() != "netgraph" {
		t.Errorf("expected name 'netgraph', got %q", c.Name())
	}

	// Should produce an inbound edge: 192.168.0.5 -> port 80
	expected := `
# HELP network_graph_edge Presence indicator for a network topology edge; value is always 1.
# TYPE network_graph_edge gauge
network_graph_edge{direction="inbound",local_port="80",remote_host="192.168.0.5"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_graph_edge"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestEntryToConnection(t *testing.T) {
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
	conn := entryToConnection(e)
	if conn.LocalIP != "10.0.0.1" || conn.LocalPort != 80 || conn.RemoteIP != "192.168.1.1" || conn.RemotePort != 45000 || conn.State != "ESTABLISHED" || conn.Protocol != "tcp" {
		t.Errorf("entryToConnection produced unexpected result: %+v", conn)
	}
}

func TestProcfsSourceListConnectionsTCPError(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No tcp/tcp6/udp/udp6 files — should fail on TCP parse.
	src := &procfsSource{procPath: dir}
	_, err := src.ListConnections()
	if err == nil {
		t.Error("expected error when TCP files are missing")
	}
}

func TestProcfsSourceListConnectionsUDPError(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// TCP files present, UDP files missing.
	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(tcp), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte("header\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No udp/udp6 files.
	src := &procfsSource{procPath: dir}
	_, err := src.ListConnections()
	if err == nil {
		t.Error("expected error when UDP files are missing")
	}
}

func TestProcfsSourceListConnections(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
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

	src := &procfsSource{procPath: dir}
	conns, err := src.ListConnections()
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(conns) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(conns))
	}
	if conns[0].Protocol != "tcp" || conns[0].LocalPort != 80 {
		t.Errorf("unexpected first connection: %+v", conns[0])
	}
	if conns[1].Protocol != "udp" || conns[1].LocalPort != 53 {
		t.Errorf("unexpected second connection: %+v", conns[1])
	}
}
