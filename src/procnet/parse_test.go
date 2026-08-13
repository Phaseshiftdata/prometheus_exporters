package procnet

import (
	"os"
	"path/filepath"
	"testing"
)

func testdataPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "proc")
}

func TestParseTCP(t *testing.T) {
	entries, err := ParseTCP(testdataPath(t))
	if err != nil {
		t.Fatalf("ParseTCP: %v", err)
	}

	// tcp has 12 entries, tcp6 has 3 entries
	if len(entries) != 15 {
		t.Fatalf("expected 15 entries, got %d", len(entries))
	}

	for _, e := range entries {
		if e.Protocol != "tcp" {
			t.Errorf("expected protocol tcp, got %s", e.Protocol)
		}
	}
}

func TestParseUDP(t *testing.T) {
	entries, err := ParseUDP(testdataPath(t))
	if err != nil {
		t.Fatalf("ParseUDP: %v", err)
	}

	// udp has 2 entries, udp6 has 1 entry
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	for _, e := range entries {
		if e.Protocol != "udp" {
			t.Errorf("expected protocol udp, got %s", e.Protocol)
		}
	}
}

func TestIPv4ByteOrder(t *testing.T) {
	entries, err := ParseTCP(testdataPath(t))
	if err != nil {
		t.Fatalf("ParseTCP: %v", err)
	}

	// First entry: 0100007F:0050 -> 127.0.0.1:80
	e := entries[0]
	if e.LocalIP != "127.0.0.1" {
		t.Errorf("expected local IP 127.0.0.1, got %s", e.LocalIP)
	}
	if e.LocalPort != 80 {
		t.Errorf("expected local port 80, got %d", e.LocalPort)
	}
	if e.RemoteIP != "0.0.0.0" {
		t.Errorf("expected remote IP 0.0.0.0, got %s", e.RemoteIP)
	}
	if e.RemotePort != 0 {
		t.Errorf("expected remote port 0, got %d", e.RemotePort)
	}

	// Second entry: 0100007F:0CEA -> 127.0.0.1:3306, remote 0100007F:0050 -> 127.0.0.1:80
	e = entries[1]
	if e.LocalIP != "127.0.0.1" {
		t.Errorf("expected local IP 127.0.0.1, got %s", e.LocalIP)
	}
	if e.LocalPort != 3306 {
		t.Errorf("expected local port 3306, got %d", e.LocalPort)
	}
	if e.RemoteIP != "127.0.0.1" {
		t.Errorf("expected remote IP 127.0.0.1, got %s", e.RemoteIP)
	}
	if e.RemotePort != 80 {
		t.Errorf("expected remote port 80, got %d", e.RemotePort)
	}

	// Third entry: 017AA8C0:1F90 -> 192.168.122.1:8080, remote 0200A8C0:D432 -> 192.168.0.2:54322
	e = entries[2]
	if e.LocalIP != "192.168.122.1" {
		t.Errorf("expected local IP 192.168.122.1, got %s", e.LocalIP)
	}
	if e.LocalPort != 8080 {
		t.Errorf("expected local port 8080, got %d", e.LocalPort)
	}
	if e.RemoteIP != "192.168.0.2" {
		t.Errorf("expected remote IP 192.168.0.2, got %s", e.RemoteIP)
	}
	if e.RemotePort != 54322 {
		t.Errorf("expected remote port 54322, got %d", e.RemotePort)
	}
}

func TestIPv6Parsing(t *testing.T) {
	entries, err := ParseTCP(testdataPath(t))
	if err != nil {
		t.Fatalf("ParseTCP: %v", err)
	}

	// tcp6 entries start at index 12.
	// Entry 0 of tcp6: all zeros -> ::
	e := entries[12]
	if e.LocalIP != "::" {
		t.Errorf("expected local IP ::, got %s", e.LocalIP)
	}
	if e.LocalPort != 80 {
		t.Errorf("expected local port 80, got %d", e.LocalPort)
	}

	// Entry 1 of tcp6: 00000000000000000000000001000000 -> ::1
	e = entries[13]
	if e.LocalIP != "::1" {
		t.Errorf("expected local IP ::1, got %s", e.LocalIP)
	}
	if e.LocalPort != 80 {
		t.Errorf("expected local port 80, got %d", e.LocalPort)
	}
	if e.RemoteIP != "::1" {
		t.Errorf("expected remote IP ::1, got %s", e.RemoteIP)
	}

	// Entry 2 of tcp6: 0000000000000000FFFF00000100007F -> 127.0.0.1
	// (Go's net.IP.String() renders IPv4-mapped IPv6 as plain IPv4)
	e = entries[14]
	if e.LocalIP != "127.0.0.1" {
		t.Errorf("expected local IP ::ffff:127.0.0.1, got %s", e.LocalIP)
	}
	if e.LocalPort != 443 {
		t.Errorf("expected local port 443, got %d", e.LocalPort)
	}
}

func TestAllTCPStates(t *testing.T) {
	entries, err := ParseTCP(testdataPath(t))
	if err != nil {
		t.Fatalf("ParseTCP: %v", err)
	}

	// Entries from tcp file and their expected states.
	expectedStates := []struct {
		index int
		state string
	}{
		{0, "LISTEN"},       // 0A
		{1, "ESTABLISHED"},  // 01
		{2, "TIME_WAIT"},    // 06
		{3, "LISTEN"},       // 0A
		{4, "SYN_SENT"},     // 02
		{5, "SYN_RECV"},     // 03
		{6, "FIN_WAIT1"},    // 04
		{7, "FIN_WAIT2"},    // 05
		{8, "CLOSE"},        // 07
		{9, "CLOSE_WAIT"},   // 08
		{10, "LAST_ACK"},    // 09
		{11, "CLOSING"},     // 0B
	}

	for _, tc := range expectedStates {
		if entries[tc.index].State != tc.state {
			t.Errorf("entry %d: expected state %s, got %s", tc.index, tc.state, entries[tc.index].State)
		}
	}
}

func TestUDPStates(t *testing.T) {
	entries, err := ParseUDP(testdataPath(t))
	if err != nil {
		t.Fatalf("ParseUDP: %v", err)
	}

	// First UDP entry: state 07 -> CLOSE
	if entries[0].State != "CLOSE" {
		t.Errorf("expected state CLOSE, got %s", entries[0].State)
	}

	// Second UDP entry: state 01 -> ESTABLISHED
	if entries[1].State != "ESTABLISHED" {
		t.Errorf("expected state ESTABLISHED, got %s", entries[1].State)
	}
}

func TestQueueAndMetadata(t *testing.T) {
	entries, err := ParseTCP(testdataPath(t))
	if err != nil {
		t.Fatalf("ParseTCP: %v", err)
	}

	// Second entry has tx=1, rx=2
	e := entries[1]
	if e.TxQueue != 1 {
		t.Errorf("expected TxQueue 1, got %d", e.TxQueue)
	}
	if e.RxQueue != 2 {
		t.Errorf("expected RxQueue 2, got %d", e.RxQueue)
	}
	if e.UID != 1000 {
		t.Errorf("expected UID 1000, got %d", e.UID)
	}
	if e.Inode != 23456 {
		t.Errorf("expected inode 23456, got %d", e.Inode)
	}

	// First entry has uid=0, inode=12345
	e = entries[0]
	if e.UID != 0 {
		t.Errorf("expected UID 0, got %d", e.UID)
	}
	if e.Inode != 12345 {
		t.Errorf("expected inode 12345, got %d", e.Inode)
	}
}

func TestEmptyFile(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create files with header only.
	header := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseTCP(dir)
	if err != nil {
		t.Fatalf("ParseTCP on empty files: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestMalformedLines(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
this is a malformed line
   short line
   2: ZZZZZZZZ:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   3: 0100007F:0050 00000000:0000 0A badqueue 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   4: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 notanumber 1 0000000000000000 100 0 0 10 0
   5: 0100007F 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   6: 0100007F:0050 00000000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   7: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000   bad        0 12345 1 0000000000000000 100 0 0 10 0
   8: 0100007F:ZZZZ 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   9: 000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
`

	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create tcp6 so it doesn't fail on missing file.
	header := "  sl  local_address rem_address\n"
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseTCP(dir)
	if err != nil {
		t.Fatalf("ParseTCP with malformed lines: %v", err)
	}

	// Only the first valid line should parse.
	if len(entries) != 1 {
		t.Errorf("expected 1 valid entry, got %d", len(entries))
	}
}

func TestFileNotFound(t *testing.T) {
	_, err := ParseTCP("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}

	_, err = ParseUDP("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestParseOnlyTCP4(t *testing.T) {
	// Test when only tcp exists (no tcp6).
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseTCP(dir)
	if err != nil {
		t.Fatalf("ParseTCP with only tcp4: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestParseOnlyTCP6(t *testing.T) {
	// Test when only tcp6 exists (no tcp).
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:0050 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 99999 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseTCP(dir)
	if err != nil {
		t.Fatalf("ParseTCP with only tcp6: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestParseOnlyUDP4(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 11111 2 0000000000000000 0
`
	if err := os.WriteFile(filepath.Join(netDir, "udp"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseUDP(dir)
	if err != nil {
		t.Fatalf("ParseUDP with only udp4: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestParseOnlyUDP6(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:14E9 00000000000000000000000000000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 33333 2 0000000000000000 0
`
	if err := os.WriteFile(filepath.Join(netDir, "udp6"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseUDP(dir)
	if err != nil {
		t.Fatalf("ParseUDP with only udp6: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestUnknownState(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0050 00000000:0000 FF 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte("header\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseTCP(dir)
	if err != nil {
		t.Fatalf("ParseTCP: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].State != "UNKNOWN" {
		t.Errorf("expected state UNKNOWN, got %s", entries[0].State)
	}
}

func TestUDPIPv6Address(t *testing.T) {
	entries, err := ParseUDP(testdataPath(t))
	if err != nil {
		t.Fatalf("ParseUDP: %v", err)
	}

	// udp6 entry: all zeros on port 0x14E9 = 5353
	last := entries[len(entries)-1]
	if last.LocalIP != "::" {
		t.Errorf("expected local IP ::, got %s", last.LocalIP)
	}
	if last.LocalPort != 5353 {
		t.Errorf("expected local port 5353, got %d", last.LocalPort)
	}
}

func TestUDPMetadata(t *testing.T) {
	entries, err := ParseUDP(testdataPath(t))
	if err != nil {
		t.Fatalf("ParseUDP: %v", err)
	}

	// First UDP entry: 00000000:0035 -> 0.0.0.0:53
	e := entries[0]
	if e.LocalIP != "0.0.0.0" {
		t.Errorf("expected local IP 0.0.0.0, got %s", e.LocalIP)
	}
	if e.LocalPort != 53 {
		t.Errorf("expected local port 53, got %d", e.LocalPort)
	}
	if e.UID != 0 {
		t.Errorf("expected UID 0, got %d", e.UID)
	}
	if e.Inode != 11111 {
		t.Errorf("expected inode 11111, got %d", e.Inode)
	}

	// Second UDP entry
	e = entries[1]
	if e.LocalIP != "127.0.0.1" {
		t.Errorf("expected local IP 127.0.0.1, got %s", e.LocalIP)
	}
	if e.UID != 1000 {
		t.Errorf("expected UID 1000, got %d", e.UID)
	}
}

func TestParseLineDirectly(t *testing.T) {
	// Test parseLine with a line that has too few fields.
	_, err := parseLine("short", "tcp")
	if err == nil {
		t.Error("expected error for short line")
	}

	// Test parseLine with invalid tx_queue:rx_queue (no colon).
	_, err = parseLine("0: 0100007F:0050 00000000:0000 0A noqueue 00:00000000 00000000 0 0 12345", "tcp")
	if err == nil {
		t.Error("expected error for invalid queue field")
	}

	// Test parseLine with invalid tx_queue hex.
	_, err = parseLine("0: 0100007F:0050 00000000:0000 0A ZZZZZZZZ:00000000 00:00000000 00000000 0 0 12345", "tcp")
	if err == nil {
		t.Error("expected error for invalid tx_queue hex")
	}

	// Test parseLine with invalid rx_queue hex.
	_, err = parseLine("0: 0100007F:0050 00000000:0000 0A 00000000:ZZZZZZZZ 00:00000000 00000000 0 0 12345", "tcp")
	if err == nil {
		t.Error("expected error for invalid rx_queue hex")
	}
}

func TestParseAddressEdgeCases(t *testing.T) {
	// No colon in address.
	_, _, err := parseAddress("noport")
	if err == nil {
		t.Error("expected error for address without colon")
	}

	// Invalid port hex.
	_, _, err = parseAddress("0100007F:ZZZZ")
	if err == nil {
		t.Error("expected error for invalid port hex")
	}

	// Invalid IP hex.
	_, _, err = parseAddress("ZZZZZZZZ:0050")
	if err == nil {
		t.Error("expected error for invalid IP hex")
	}

	// Odd-length IP hex (not 8 or 32).
	_, _, err = parseAddress("0100007F00:0050")
	if err == nil {
		t.Error("expected error for unexpected IP hex length")
	}

	// Invalid hex in IPv6.
	_, _, err = parseAddress("ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ:0050")
	if err == nil {
		t.Error("expected error for invalid IPv6 hex")
	}
}

func TestStateName(t *testing.T) {
	tests := []struct {
		hex  string
		name string
	}{
		{"01", "ESTABLISHED"},
		{"02", "SYN_SENT"},
		{"03", "SYN_RECV"},
		{"04", "FIN_WAIT1"},
		{"05", "FIN_WAIT2"},
		{"06", "TIME_WAIT"},
		{"07", "CLOSE"},
		{"08", "CLOSE_WAIT"},
		{"09", "LAST_ACK"},
		{"0A", "LISTEN"},
		{"0B", "CLOSING"},
		{"FF", "UNKNOWN"},
		{"00", "UNKNOWN"},
	}
	for _, tc := range tests {
		got := stateName(tc.hex)
		if got != tc.name {
			t.Errorf("stateName(%s) = %s, want %s", tc.hex, got, tc.name)
		}
	}
}

func TestHexToBytes(t *testing.T) {
	// Odd length.
	_, err := hexToBytes("ABC")
	if err == nil {
		t.Error("expected error for odd-length hex")
	}

	// Valid.
	b, err := hexToBytes("0100007F")
	if err != nil {
		t.Fatalf("hexToBytes: %v", err)
	}
	if len(b) != 4 || b[0] != 0x01 || b[1] != 0x00 || b[2] != 0x00 || b[3] != 0x7F {
		t.Errorf("unexpected bytes: %v", b)
	}

	// Invalid hex char.
	_, err = hexToBytes("ZZZZ")
	if err == nil {
		t.Error("expected error for invalid hex chars")
	}
}

func TestEmptyLines(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// File with blank lines interspersed.
	content := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode

   0: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0

`
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte("header\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseTCP(dir)
	if err != nil {
		t.Fatalf("ParseTCP: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestEmptyFileNoHeader(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Completely empty file (no header).
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseTCP(dir)
	if err != nil {
		t.Fatalf("ParseTCP: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestTCPUnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root can read any file")
	}
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a tcp file with no read permission to trigger a non-ErrNotExist error.
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte("header\n"), 0o000); err != nil {
		t.Fatal(err)
	}

	_, err := ParseTCP(dir)
	if err == nil {
		t.Error("expected error for unreadable tcp file")
	}
}

func TestUDPUnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root can read any file")
	}
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a udp file with no read permission.
	if err := os.WriteFile(filepath.Join(netDir, "udp"), []byte("header\n"), 0o000); err != nil {
		t.Fatal(err)
	}

	_, err := ParseUDP(dir)
	if err == nil {
		t.Error("expected error for unreadable udp file")
	}
}

func TestTCPUnreadableTCP6(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root can read any file")
	}
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// tcp is fine, tcp6 is unreadable.
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte("header\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp6"), []byte("header\n"), 0o000); err != nil {
		t.Fatal(err)
	}

	_, err := ParseTCP(dir)
	if err == nil {
		t.Error("expected error for unreadable tcp6 file")
	}
}

func TestUDPUnreadableUDP6(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root can read any file")
	}
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// udp is fine, udp6 is unreadable.
	if err := os.WriteFile(filepath.Join(netDir, "udp"), []byte("header\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "udp6"), []byte("header\n"), 0o000); err != nil {
		t.Fatal(err)
	}

	_, err := ParseUDP(dir)
	if err == nil {
		t.Error("expected error for unreadable udp6 file")
	}
}

func TestPort443Entry(t *testing.T) {
	entries, err := ParseTCP(testdataPath(t))
	if err != nil {
		t.Fatalf("ParseTCP: %v", err)
	}

	// Fourth entry (index 3): 00000000:01BB -> 0.0.0.0:443
	e := entries[3]
	if e.LocalIP != "0.0.0.0" {
		t.Errorf("expected local IP 0.0.0.0, got %s", e.LocalIP)
	}
	if e.LocalPort != 443 {
		t.Errorf("expected local port 443, got %d", e.LocalPort)
	}
}
