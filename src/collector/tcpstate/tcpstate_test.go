package tcpstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

func TestBasicConnections(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.5", RemotePort: 52431, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.10", RemotePort: 40000, State: "TIME_WAIT"},
		},
	}
	c := NewWithSource(src)

	expected := `
# HELP network_tcp_connection Per-TCP-connection state indicator; value is always 1.
# TYPE network_tcp_connection gauge
network_tcp_connection{local_addr="10.0.0.1",local_port="443",peer_addr="192.168.1.5",peer_port="52431",state="ESTABLISHED"} 1
network_tcp_connection{local_addr="10.0.0.1",local_port="80",peer_addr="192.168.1.10",peer_port="40000",state="TIME_WAIT"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_tcp_connection"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestLoopbackExclusion(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			// Both sides loopback IPv4 -- should be excluded.
			{LocalIP: "127.0.0.1", LocalPort: 80, RemoteIP: "127.0.0.1", RemotePort: 45000, State: "ESTABLISHED"},
			// Both sides 0.0.0.0 -- should be excluded.
			{LocalIP: "0.0.0.0", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN"},
			// Both sides loopback IPv6 -- should be excluded.
			{LocalIP: "::1", LocalPort: 80, RemoteIP: "::1", RemotePort: 45000, State: "ESTABLISHED"},
			// Both sides :: -- should be excluded.
			{LocalIP: "::", LocalPort: 80, RemoteIP: "::", RemotePort: 0, State: "LISTEN"},
			// Only one side loopback -- should NOT be excluded.
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "127.0.0.1", RemotePort: 45000, State: "ESTABLISHED"},
			// Non-loopback -- should NOT be excluded.
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.5", RemotePort: 52431, State: "ESTABLISHED"},
		},
	}
	c := NewWithSource(src)

	count := testutil.CollectAndCount(c, "network_tcp_connection")
	if count != 2 {
		t.Errorf("expected 2 connections (loopback excluded), got %d", count)
	}
}

func TestLoopbackMixed(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			// Mixed: local is loopback, remote is 0.0.0.0 -- should be excluded.
			{LocalIP: "127.0.0.1", LocalPort: 80, RemoteIP: "0.0.0.0", RemotePort: 0, State: "LISTEN"},
			// Mixed: local is ::, remote is ::1 -- should be excluded.
			{LocalIP: "::", LocalPort: 80, RemoteIP: "::1", RemotePort: 45000, State: "ESTABLISHED"},
		},
	}
	c := NewWithSource(src)

	count := testutil.CollectAndCount(c, "network_tcp_connection")
	if count != 0 {
		t.Errorf("expected 0 connections (loopback excluded), got %d", count)
	}
}

func TestStateFilter(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.5", RemotePort: 52431, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.10", RemotePort: 40000, State: "TIME_WAIT"},
			{LocalIP: "10.0.0.1", LocalPort: 22, RemoteIP: "192.168.1.20", RemotePort: 50000, State: "LISTEN"},
			{LocalIP: "10.0.0.1", LocalPort: 8080, RemoteIP: "192.168.1.30", RemotePort: 60000, State: "CLOSE_WAIT"},
		},
	}
	c := NewWithOptions(src, DefaultMaxConnections, []string{"ESTABLISHED", "LISTEN"})

	count := testutil.CollectAndCount(c, "network_tcp_connection")
	if count != 2 {
		t.Errorf("expected 2 connections (only ESTABLISHED and LISTEN), got %d", count)
	}
}

func TestStateFilterCaseInsensitive(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.5", RemotePort: 52431, State: "ESTABLISHED"},
		},
	}
	// Filter with lowercase -- should still match.
	c := NewWithOptions(src, DefaultMaxConnections, []string{"established"})

	count := testutil.CollectAndCount(c, "network_tcp_connection")
	if count != 1 {
		t.Errorf("expected 1 connection with case-insensitive filter, got %d", count)
	}
}

func TestStateFilterWithWhitespace(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.5", RemotePort: 52431, State: "ESTABLISHED"},
		},
	}
	// Filter with leading/trailing whitespace.
	c := NewWithOptions(src, DefaultMaxConnections, []string{" ESTABLISHED "})

	count := testutil.CollectAndCount(c, "network_tcp_connection")
	if count != 1 {
		t.Errorf("expected 1 connection with trimmed filter, got %d", count)
	}
}

func TestTruncation(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.1", RemotePort: 52431, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.2", RemotePort: 52432, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.3", RemotePort: 52433, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.4", RemotePort: 52434, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.5", RemotePort: 52435, State: "ESTABLISHED"},
		},
	}

	// maxConns=2 with 5 connections: only 2 emitted, truncated=1.
	c := NewWithOptions(src, 2, nil)

	expected := `
# HELP network_tcp_connections_truncated Set to 1 when the TCP connection count exceeds the maximum limit and output is truncated.
# TYPE network_tcp_connections_truncated gauge
network_tcp_connections_truncated 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_tcp_connections_truncated"); err != nil {
		t.Errorf("truncation metric mismatch: %v", err)
	}

	entryCount := testutil.CollectAndCount(c, "network_tcp_connection")
	if entryCount != 2 {
		t.Errorf("expected 2 connections emitted, got %d", entryCount)
	}
}

func TestNoTruncation(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.1", RemotePort: 52431, State: "ESTABLISHED"},
		},
	}

	c := NewWithOptions(src, 10, nil)

	expected := `
# HELP network_tcp_connections_truncated Set to 1 when the TCP connection count exceeds the maximum limit and output is truncated.
# TYPE network_tcp_connections_truncated gauge
network_tcp_connections_truncated 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_tcp_connections_truncated"); err != nil {
		t.Errorf("truncation metric mismatch: %v", err)
	}
}

func TestEmptyConnections(t *testing.T) {
	src := &mockSource{connections: []Connection{}}
	c := NewWithSource(src)

	expected := `
# HELP network_tcp_connection Per-TCP-connection state indicator; value is always 1.
# TYPE network_tcp_connection gauge
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_tcp_connection"); err != nil {
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

func TestName(t *testing.T) {
	c := NewWithSource(&mockSource{})
	if c.Name() != "tcpstate" {
		t.Fatalf("expected name 'tcpstate', got %q", c.Name())
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

func TestNew(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"tcp", "tcp6"} {
		if err := os.WriteFile(filepath.Join(netDir, f), []byte("header\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c := New(dir)
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.Name() != "tcpstate" {
		t.Errorf("expected name 'tcpstate', got %q", c.Name())
	}
}

func TestNewWithMax(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"tcp", "tcp6"} {
		if err := os.WriteFile(filepath.Join(netDir, f), []byte("header\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c := NewWithMax(dir, 100, nil)
	if c == nil {
		t.Fatal("NewWithMax() returned nil")
	}
	if c.Name() != "tcpstate" {
		t.Errorf("expected name 'tcpstate', got %q", c.Name())
	}
}

func TestNewWithOptionsNegativeMax(t *testing.T) {
	src := &mockSource{}
	c := NewWithOptions(src, -1, nil)
	if c == nil {
		t.Fatal("NewWithOptions(-1) returned nil")
	}
	tc := c.(*tcpStateCollector)
	if tc.maxConns != DefaultMaxConnections {
		t.Errorf("expected maxConns=%d for negative input, got %d", DefaultMaxConnections, tc.maxConns)
	}
}

func TestNewWithOptionsEmptyStates(t *testing.T) {
	src := &mockSource{}
	c := NewWithOptions(src, DefaultMaxConnections, []string{})
	tc := c.(*tcpStateCollector)
	if tc.states != nil {
		t.Errorf("expected nil states for empty slice, got %v", tc.states)
	}
}

func TestProcfsSource(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100A8C0:01BB 0500A8C0:C000 01 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(tcp), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte("header\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := &procfsSource{procPath: dir}
	conns, err := src.ListConnections()
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].LocalIP != "192.168.0.1" || conns[0].LocalPort != 443 {
		t.Errorf("unexpected connection: %+v", conns[0])
	}
	if conns[0].RemoteIP != "192.168.0.5" || conns[0].RemotePort != 49152 {
		t.Errorf("unexpected remote: %+v", conns[0])
	}
	if conns[0].State != "ESTABLISHED" {
		t.Errorf("unexpected state: %q", conns[0].State)
	}
}

func TestProcfsSourceError(t *testing.T) {
	src := &procfsSource{procPath: "/nonexistent/path"}
	_, err := src.ListConnections()
	if err == nil {
		t.Error("expected error when TCP files are missing")
	}
}

func TestNewWithFakeProcfs(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100A8C0:01BB 0500A8C0:C000 01 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(tcp), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte("header\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(dir)

	expected := `
# HELP network_tcp_connection Per-TCP-connection state indicator; value is always 1.
# TYPE network_tcp_connection gauge
network_tcp_connection{local_addr="192.168.0.1",local_port="443",peer_addr="192.168.0.5",peer_port="49152",state="ESTABLISHED"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_tcp_connection"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestIsLoopback(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"::", true},
		{"10.0.0.1", false},
		{"192.168.1.1", false},
		{"::ffff:127.0.0.1", false},
	}
	for _, tt := range tests {
		if got := isLoopback(tt.ip); got != tt.want {
			t.Errorf("isLoopback(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestAllTCPStates(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.1", RemotePort: 40001, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.2", RemotePort: 40002, State: "SYN_SENT"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.3", RemotePort: 40003, State: "SYN_RECV"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.4", RemotePort: 40004, State: "FIN_WAIT1"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.5", RemotePort: 40005, State: "FIN_WAIT2"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.6", RemotePort: 40006, State: "TIME_WAIT"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.7", RemotePort: 40007, State: "CLOSE"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.8", RemotePort: 40008, State: "CLOSE_WAIT"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.9", RemotePort: 40009, State: "LAST_ACK"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.10", RemotePort: 40010, State: "LISTEN"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.11", RemotePort: 40011, State: "CLOSING"},
		},
	}
	c := NewWithSource(src)

	count := testutil.CollectAndCount(c, "network_tcp_connection")
	if count != 11 {
		t.Errorf("expected 11 connections (all states), got %d", count)
	}
}

func TestTruncationWithLoopbackSkipped(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			// Loopback -- skipped, does not count toward limit.
			{LocalIP: "127.0.0.1", LocalPort: 80, RemoteIP: "127.0.0.1", RemotePort: 45000, State: "ESTABLISHED"},
			// Real connections.
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.1", RemotePort: 52431, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.2", RemotePort: 52432, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.3", RemotePort: 52433, State: "ESTABLISHED"},
		},
	}

	// maxConns=2: loopback skipped, first 2 real connections emitted, truncated=1.
	c := NewWithOptions(src, 2, nil)

	expected := `
# HELP network_tcp_connections_truncated Set to 1 when the TCP connection count exceeds the maximum limit and output is truncated.
# TYPE network_tcp_connections_truncated gauge
network_tcp_connections_truncated 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_tcp_connections_truncated"); err != nil {
		t.Errorf("truncation metric mismatch: %v", err)
	}

	entryCount := testutil.CollectAndCount(c, "network_tcp_connection")
	if entryCount != 2 {
		t.Errorf("expected 2 connections emitted, got %d", entryCount)
	}
}

func TestTruncationWithStateFilter(t *testing.T) {
	src := &mockSource{
		connections: []Connection{
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.1", RemotePort: 52431, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 80, RemoteIP: "192.168.1.2", RemotePort: 40000, State: "TIME_WAIT"},
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.3", RemotePort: 52433, State: "ESTABLISHED"},
			{LocalIP: "10.0.0.1", LocalPort: 443, RemoteIP: "192.168.1.4", RemotePort: 52434, State: "ESTABLISHED"},
		},
	}

	// maxConns=1, filter ESTABLISHED only: TIME_WAIT skipped, first ESTABLISHED emitted, truncated=1.
	c := NewWithOptions(src, 1, []string{"ESTABLISHED"})

	expected := `
# HELP network_tcp_connections_truncated Set to 1 when the TCP connection count exceeds the maximum limit and output is truncated.
# TYPE network_tcp_connections_truncated gauge
network_tcp_connections_truncated 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_tcp_connections_truncated"); err != nil {
		t.Errorf("truncation metric mismatch: %v", err)
	}

	entryCount := testutil.CollectAndCount(c, "network_tcp_connection")
	if entryCount != 1 {
		t.Errorf("expected 1 connection emitted, got %d", entryCount)
	}
}
