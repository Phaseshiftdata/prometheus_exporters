package ipsec

import (
	"fmt"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/strongswan/govici/vici"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
)

// Compile-time interface checks.
var (
	_ VICIClient          = (*mockVICIClient)(nil)
	_ collector.Collector = (*ipsecCollector)(nil)
)

// mockVICIClient implements VICIClient for testing.
type mockVICIClient struct {
	available bool
	sas       []IKESAInfo
	sasErr    error
	stats     CharonStats
	statsErr  error
}

func (m *mockVICIClient) IsAvailable() bool              { return m.available }
func (m *mockVICIClient) ListSAs() ([]IKESAInfo, error)  { return m.sas, m.sasErr }
func (m *mockVICIClient) GetStats() (CharonStats, error) { return m.stats, m.statsErr }

func TestName(t *testing.T) {
	c := NewWithClient(&mockVICIClient{})
	if c.Name() != "ipsec" {
		t.Errorf("expected name 'ipsec', got %q", c.Name())
	}
}

func TestDescribe(t *testing.T) {
	c := NewWithClient(&mockVICIClient{})
	ch := make(chan *prometheus.Desc, 20)
	c.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	if len(descs) != 16 {
		t.Fatalf("expected 16 descriptors, got %d", len(descs))
	}
}

func TestVICIUnavailable(t *testing.T) {
	client := &mockVICIClient{available: false}
	c := NewWithClient(client)

	expected := `
# HELP ipsec_up Whether the VICI socket is reachable (1 = up, 0 = down).
# TYPE ipsec_up gauge
ipsec_up 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "ipsec_up"); err != nil {
		t.Error(err)
	}

	// No other metrics should be emitted.
	count := testutil.CollectAndCount(c,
		"ipsec_ike_sas",
		"ipsec_ike_sa_state",
		"ipsec_uptime_seconds",
	)
	if count != 0 {
		t.Errorf("expected 0 metrics when unavailable, got %d", count)
	}
}

func TestEmptySAList(t *testing.T) {
	client := &mockVICIClient{
		available: true,
		sas:       []IKESAInfo{},
		stats: CharonStats{
			Uptime:        3600,
			Workers:       16,
			IdleWorkers:   10,
			ActiveWorkers: 6,
			Queues:        map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0},
			HalfOpenIKE:   0,
		},
	}

	c := NewWithClient(client)

	expected := `
# HELP ipsec_up Whether the VICI socket is reachable (1 = up, 0 = down).
# TYPE ipsec_up gauge
ipsec_up 1
# HELP ipsec_ike_sas Total number of IKE SAs.
# TYPE ipsec_ike_sas gauge
ipsec_ike_sas 0
# HELP ipsec_half_open_ike_sas Number of half-open IKE SAs.
# TYPE ipsec_half_open_ike_sas gauge
ipsec_half_open_ike_sas 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"ipsec_up", "ipsec_ike_sas", "ipsec_half_open_ike_sas",
	); err != nil {
		t.Error(err)
	}
}

func TestMultipleIKESAsWithChildSAs(t *testing.T) {
	client := &mockVICIClient{
		available: true,
		sas: []IKESAInfo{
			{
				Name:            "gw-east",
				UID:             "1",
				RemoteHost:      "10.0.0.1",
				State:           2, // ESTABLISHED
				EstablishedSecs: 7200,
				ChildSAs: []ChildSAInfo{
					{
						Name:          "net-east",
						UID:           "10",
						State:         3, // INSTALLED
						LocalTS:       "192.168.1.0/24",
						RemoteTS:      "192.168.2.0/24",
						BytesIn:       1000000,
						BytesOut:      2000000,
						PacketsIn:     5000,
						PacketsOut:    6000,
						InstalledSecs: 3600,
					},
					{
						Name:          "net-east-v6",
						UID:           "11",
						State:         3,
						LocalTS:       "fd00:1::/64",
						RemoteTS:      "fd00:2::/64",
						BytesIn:       500000,
						BytesOut:      600000,
						PacketsIn:     2500,
						PacketsOut:    3000,
						InstalledSecs: 3500,
					},
				},
			},
			{
				Name:            "gw-west",
				UID:             "2",
				RemoteHost:      "10.0.0.2",
				State:           2,
				EstablishedSecs: 1800,
				ChildSAs: []ChildSAInfo{
					{
						Name:          "net-west",
						UID:           "20",
						State:         3,
						LocalTS:       "192.168.1.0/24",
						RemoteTS:      "192.168.3.0/24",
						BytesIn:       300000,
						BytesOut:      400000,
						PacketsIn:     1500,
						PacketsOut:    2000,
						InstalledSecs: 1700,
					},
				},
			},
		},
		stats: CharonStats{
			Uptime:        86400,
			Workers:       16,
			IdleWorkers:   12,
			ActiveWorkers: 4,
			Queues:        map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3},
			HalfOpenIKE:   1,
		},
	}

	c := NewWithClient(client)

	// Check IKE SA count.
	expected := `
# HELP ipsec_ike_sas Total number of IKE SAs.
# TYPE ipsec_ike_sas gauge
ipsec_ike_sas 2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "ipsec_ike_sas"); err != nil {
		t.Error(err)
	}

	// Check IKE SA states.
	expected = `
# HELP ipsec_ike_sa_state Numeric IKE SA state (0=CREATED..7=DESTROYING).
# TYPE ipsec_ike_sa_state gauge
ipsec_ike_sa_state{name="gw-east",remote_host="10.0.0.1",uid="1"} 2
ipsec_ike_sa_state{name="gw-west",remote_host="10.0.0.2",uid="2"} 2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "ipsec_ike_sa_state"); err != nil {
		t.Error(err)
	}

	// Check child SA byte counts.
	expected = `
# HELP ipsec_child_sa_bytes_in Bytes received on this child SA.
# TYPE ipsec_child_sa_bytes_in gauge
ipsec_child_sa_bytes_in{ike_sa_name="gw-east",local_ts="192.168.1.0/24",name="net-east",remote_host="10.0.0.1",remote_ts="192.168.2.0/24",uid="10"} 1e+06
ipsec_child_sa_bytes_in{ike_sa_name="gw-east",local_ts="fd00:1::/64",name="net-east-v6",remote_host="10.0.0.1",remote_ts="fd00:2::/64",uid="11"} 500000
ipsec_child_sa_bytes_in{ike_sa_name="gw-west",local_ts="192.168.1.0/24",name="net-west",remote_host="10.0.0.2",remote_ts="192.168.3.0/24",uid="20"} 300000
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "ipsec_child_sa_bytes_in"); err != nil {
		t.Error(err)
	}

	// Check charon stats.
	expected = `
# HELP ipsec_uptime_seconds Charon daemon uptime in seconds.
# TYPE ipsec_uptime_seconds gauge
ipsec_uptime_seconds 86400
# HELP ipsec_workers_total Total number of charon worker threads.
# TYPE ipsec_workers_total gauge
ipsec_workers_total 16
# HELP ipsec_idle_workers Number of idle charon worker threads.
# TYPE ipsec_idle_workers gauge
ipsec_idle_workers 12
# HELP ipsec_active_workers Number of active charon worker threads.
# TYPE ipsec_active_workers gauge
ipsec_active_workers 4
# HELP ipsec_half_open_ike_sas Number of half-open IKE SAs.
# TYPE ipsec_half_open_ike_sas gauge
ipsec_half_open_ike_sas 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"ipsec_uptime_seconds", "ipsec_workers_total",
		"ipsec_idle_workers", "ipsec_active_workers", "ipsec_half_open_ike_sas",
	); err != nil {
		t.Error(err)
	}

	// Check queue metrics.
	expected = `
# HELP ipsec_queues Number of queued jobs by priority.
# TYPE ipsec_queues gauge
ipsec_queues{priority="critical"} 0
ipsec_queues{priority="high"} 1
ipsec_queues{priority="low"} 3
ipsec_queues{priority="medium"} 2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "ipsec_queues"); err != nil {
		t.Error(err)
	}
}

func TestRekeyScenario(t *testing.T) {
	client := &mockVICIClient{
		available: true,
		sas: []IKESAInfo{
			{
				Name:            "gw-rekeyed",
				UID:             "100",
				RemoteHost:      "10.0.0.5",
				State:           5, // REKEYED
				EstablishedSecs: 14400,
			},
			{
				Name:            "gw-rekeyed",
				UID:             "101",
				RemoteHost:      "10.0.0.5",
				State:           2, // ESTABLISHED (new)
				EstablishedSecs: 60,
			},
		},
		stats: CharonStats{
			Uptime:        86400,
			Workers:       16,
			IdleWorkers:   14,
			ActiveWorkers: 2,
			Queues:        map[string]int{},
			HalfOpenIKE:   0,
		},
	}

	c := NewWithClient(client)

	expected := `
# HELP ipsec_ike_sa_state Numeric IKE SA state (0=CREATED..7=DESTROYING).
# TYPE ipsec_ike_sa_state gauge
ipsec_ike_sa_state{name="gw-rekeyed",remote_host="10.0.0.5",uid="100"} 5
ipsec_ike_sa_state{name="gw-rekeyed",remote_host="10.0.0.5",uid="101"} 2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "ipsec_ike_sa_state"); err != nil {
		t.Error(err)
	}

	expected = `
# HELP ipsec_ike_sas Total number of IKE SAs.
# TYPE ipsec_ike_sas gauge
ipsec_ike_sas 2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "ipsec_ike_sas"); err != nil {
		t.Error(err)
	}
}

func TestAllIKEStates(t *testing.T) {
	states := []struct {
		name  string
		value int
	}{
		{"CREATED", 0},
		{"CONNECTING", 1},
		{"ESTABLISHED", 2},
		{"PASSIVE", 3},
		{"REKEYING", 4},
		{"REKEYED", 5},
		{"DELETING", 6},
		{"DESTROYING", 7},
	}

	for _, s := range states {
		got := IKEStateValue(s.name)
		if got != s.value {
			t.Errorf("IKEStateValue(%q) = %d, want %d", s.name, got, s.value)
		}
	}

	if got := IKEStateValue("UNKNOWN"); got != -1 {
		t.Errorf("IKEStateValue(UNKNOWN) = %d, want -1", got)
	}
}

func TestAllChildSAStates(t *testing.T) {
	states := []struct {
		name  string
		value int
	}{
		{"CREATED", 0},
		{"ROUTED", 1},
		{"INSTALLING", 2},
		{"INSTALLED", 3},
		{"UPDATING", 4},
		{"REKEYING", 5},
		{"REKEYED", 6},
		{"RETRYING", 7},
		{"DELETING", 8},
		{"DELETED", 9},
		{"DESTROYING", 10},
	}

	for _, s := range states {
		got := ChildStateValue(s.name)
		if got != s.value {
			t.Errorf("ChildStateValue(%q) = %d, want %d", s.name, got, s.value)
		}
	}

	if got := ChildStateValue("UNKNOWN"); got != -1 {
		t.Errorf("ChildStateValue(UNKNOWN) = %d, want -1", got)
	}
}

func TestAllIKEStatesAsMetrics(t *testing.T) {
	var sas []IKESAInfo
	for state := 0; state <= 7; state++ {
		sas = append(sas, IKESAInfo{
			Name:            "gw",
			UID:             fmt.Sprintf("%d", state),
			RemoteHost:      "10.0.0.1",
			State:           state,
			EstablishedSecs: 0,
		})
	}

	client := &mockVICIClient{
		available: true,
		sas:       sas,
		stats: CharonStats{
			Uptime: 100,
			Queues: map[string]int{},
		},
	}

	c := NewWithClient(client)
	count := testutil.CollectAndCount(c, "ipsec_ike_sa_state")
	if count != 8 {
		t.Errorf("expected 8 IKE SA state metrics, got %d", count)
	}
}

func TestAllChildSAStatesAsMetrics(t *testing.T) {
	var children []ChildSAInfo
	for state := 0; state <= 10; state++ {
		children = append(children, ChildSAInfo{
			Name:     "child",
			UID:      fmt.Sprintf("%d", state),
			State:    state,
			LocalTS:  "10.0.0.0/8",
			RemoteTS: "172.16.0.0/12",
		})
	}

	client := &mockVICIClient{
		available: true,
		sas: []IKESAInfo{
			{
				Name:       "gw",
				UID:        "1",
				RemoteHost: "10.0.0.1",
				State:      2,
				ChildSAs:   children,
			},
		},
		stats: CharonStats{
			Uptime: 100,
			Queues: map[string]int{},
		},
	}

	c := NewWithClient(client)
	count := testutil.CollectAndCount(c, "ipsec_child_sa_state")
	if count != 11 {
		t.Errorf("expected 11 child SA state metrics, got %d", count)
	}
}

func TestCharonStatsWithQueues(t *testing.T) {
	client := &mockVICIClient{
		available: true,
		sas:       []IKESAInfo{},
		stats: CharonStats{
			Uptime:        12345.5,
			Workers:       32,
			IdleWorkers:   20,
			ActiveWorkers: 12,
			Queues: map[string]int{
				"critical": 1,
				"high":     5,
				"medium":   10,
				"low":      25,
			},
			HalfOpenIKE: 3,
		},
	}

	c := NewWithClient(client)

	expected := `
# HELP ipsec_uptime_seconds Charon daemon uptime in seconds.
# TYPE ipsec_uptime_seconds gauge
ipsec_uptime_seconds 12345.5
# HELP ipsec_workers_total Total number of charon worker threads.
# TYPE ipsec_workers_total gauge
ipsec_workers_total 32
# HELP ipsec_idle_workers Number of idle charon worker threads.
# TYPE ipsec_idle_workers gauge
ipsec_idle_workers 20
# HELP ipsec_active_workers Number of active charon worker threads.
# TYPE ipsec_active_workers gauge
ipsec_active_workers 12
# HELP ipsec_half_open_ike_sas Number of half-open IKE SAs.
# TYPE ipsec_half_open_ike_sas gauge
ipsec_half_open_ike_sas 3
# HELP ipsec_queues Number of queued jobs by priority.
# TYPE ipsec_queues gauge
ipsec_queues{priority="critical"} 1
ipsec_queues{priority="high"} 5
ipsec_queues{priority="low"} 25
ipsec_queues{priority="medium"} 10
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"ipsec_uptime_seconds", "ipsec_workers_total",
		"ipsec_idle_workers", "ipsec_active_workers",
		"ipsec_half_open_ike_sas", "ipsec_queues",
	); err != nil {
		t.Error(err)
	}
}

func TestSAListError(t *testing.T) {
	client := &mockVICIClient{
		available: true,
		sasErr:    fmt.Errorf("vici dial failed"),
	}

	c := NewWithClient(client)
	metrics := make(chan prometheus.Metric, 20)
	c.Collect(metrics)
	close(metrics)

	// Should have ipsec_up=1 and an invalid metric.
	count := 0
	for range metrics {
		count++
	}
	if count < 2 {
		t.Errorf("expected at least 2 metrics on SA error, got %d", count)
	}
}

func TestStatsError(t *testing.T) {
	client := &mockVICIClient{
		available: true,
		sas:       []IKESAInfo{},
		statsErr:  fmt.Errorf("stats failed"),
	}

	c := NewWithClient(client)
	metrics := make(chan prometheus.Metric, 20)
	c.Collect(metrics)
	close(metrics)

	count := 0
	for range metrics {
		count++
	}
	// ipsec_up=1, ipsec_ike_sas=0, plus invalid metric for stats error.
	if count < 3 {
		t.Errorf("expected at least 3 metrics on stats error, got %d", count)
	}
}

func TestChildSATrafficMetrics(t *testing.T) {
	client := &mockVICIClient{
		available: true,
		sas: []IKESAInfo{
			{
				Name:       "tunnel",
				UID:        "42",
				RemoteHost: "10.1.1.1",
				State:      2,
				ChildSAs: []ChildSAInfo{
					{
						Name:          "net",
						UID:           "99",
						State:         3,
						LocalTS:       "10.0.0.0/8",
						RemoteTS:      "172.16.0.0/12",
						BytesIn:       123456,
						BytesOut:      654321,
						PacketsIn:     1000,
						PacketsOut:    2000,
						InstalledSecs: 999.5,
					},
				},
			},
		},
		stats: CharonStats{
			Uptime: 100,
			Queues: map[string]int{},
		},
	}

	c := NewWithClient(client)

	expected := `
# HELP ipsec_child_sa_bytes_out Bytes sent on this child SA.
# TYPE ipsec_child_sa_bytes_out gauge
ipsec_child_sa_bytes_out{ike_sa_name="tunnel",local_ts="10.0.0.0/8",name="net",remote_host="10.1.1.1",remote_ts="172.16.0.0/12",uid="99"} 654321
# HELP ipsec_child_sa_packets_in Packets received on this child SA.
# TYPE ipsec_child_sa_packets_in gauge
ipsec_child_sa_packets_in{ike_sa_name="tunnel",local_ts="10.0.0.0/8",name="net",remote_host="10.1.1.1",remote_ts="172.16.0.0/12",uid="99"} 1000
# HELP ipsec_child_sa_packets_out Packets sent on this child SA.
# TYPE ipsec_child_sa_packets_out gauge
ipsec_child_sa_packets_out{ike_sa_name="tunnel",local_ts="10.0.0.0/8",name="net",remote_host="10.1.1.1",remote_ts="172.16.0.0/12",uid="99"} 2000
# HELP ipsec_child_sa_installed_seconds Seconds since the child SA was installed.
# TYPE ipsec_child_sa_installed_seconds gauge
ipsec_child_sa_installed_seconds{ike_sa_name="tunnel",local_ts="10.0.0.0/8",name="net",remote_host="10.1.1.1",remote_ts="172.16.0.0/12",uid="99"} 999.5
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"ipsec_child_sa_bytes_out",
		"ipsec_child_sa_packets_in",
		"ipsec_child_sa_packets_out",
		"ipsec_child_sa_installed_seconds",
	); err != nil {
		t.Error(err)
	}
}

func TestIKEEstablishedSeconds(t *testing.T) {
	client := &mockVICIClient{
		available: true,
		sas: []IKESAInfo{
			{
				Name:            "gw",
				UID:             "1",
				RemoteHost:      "10.0.0.1",
				State:           2,
				EstablishedSecs: 42000.5,
			},
		},
		stats: CharonStats{
			Uptime: 100,
			Queues: map[string]int{},
		},
	}

	c := NewWithClient(client)
	expected := `
# HELP ipsec_ike_sa_established_seconds Seconds since the IKE SA was established.
# TYPE ipsec_ike_sa_established_seconds gauge
ipsec_ike_sa_established_seconds{name="gw",remote_host="10.0.0.1",uid="1"} 42000.5
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "ipsec_ike_sa_established_seconds"); err != nil {
		t.Error(err)
	}
}

func TestStateCaseSensitivity(t *testing.T) {
	// Lowercase should also work.
	if got := IKEStateValue("established"); got != 2 {
		t.Errorf("IKEStateValue(established) = %d, want 2", got)
	}
	if got := ChildStateValue("installed"); got != 3 {
		t.Errorf("ChildStateValue(installed) = %d, want 3", got)
	}
}

func TestNewConstructor(t *testing.T) {
	c := New("/nonexistent/vici.sock")
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.Name() != "ipsec" {
		t.Errorf("expected name 'ipsec', got %q", c.Name())
	}
}

// testMsg implements MessageGetter for testing.
type testMsg struct {
	data map[string]any
	keys []string
}

func newTestMsg(pairs ...any) *testMsg {
	m := &testMsg{data: make(map[string]any)}
	for i := 0; i < len(pairs)-1; i += 2 {
		key := pairs[i].(string)
		m.data[key] = pairs[i+1]
		m.keys = append(m.keys, key)
	}
	return m
}

func (m *testMsg) Get(key string) any { return m.data[key] }
func (m *testMsg) Keys() []string     { return m.keys }

func TestMsgStr(t *testing.T) {
	// nil message should return empty string.
	if got := msgStr(nil, "key"); got != "" {
		t.Errorf("msgStr(nil, key) = %q, want empty", got)
	}

	// Missing key should return empty string.
	m := newTestMsg("a", "hello")
	if got := msgStr(m, "missing"); got != "" {
		t.Errorf("msgStr(m, missing) = %q, want empty", got)
	}

	// Existing key should return value.
	if got := msgStr(m, "a"); got != "hello" {
		t.Errorf("msgStr(m, a) = %q, want 'hello'", got)
	}

	// Non-string value should return empty.
	m2 := newTestMsg("num", 42)
	if got := msgStr(m2, "num"); got != "" {
		t.Errorf("msgStr(m, num) = %q, want empty", got)
	}
}

func TestMsgSection(t *testing.T) {
	// nil message.
	if got := msgSection(nil, "key"); got != nil {
		t.Error("msgSection(nil, key) should be nil")
	}

	// Missing key.
	m := newTestMsg("a", "hello")
	if got := msgSection(m, "missing"); got != nil {
		t.Error("msgSection(m, missing) should be nil")
	}

	// Non-section value (string).
	if got := msgSection(m, "a"); got != nil {
		t.Error("msgSection(m, a) with string value should be nil")
	}

	// MessageGetter value.
	sub := newTestMsg("x", "y")
	m2 := newTestMsg("child", MessageGetter(sub))
	got := msgSection(m2, "child")
	if got == nil {
		t.Fatal("msgSection should return MessageGetter sub-section")
	}
	if msgStr(got, "x") != "y" {
		t.Errorf("expected 'y', got %q", msgStr(got, "x"))
	}
}

func TestParseIKESAs(t *testing.T) {
	childSA := newTestMsg(
		"uniqueid", "10",
		"state", "INSTALLED",
		"local-ts", "192.168.1.0/24",
		"remote-ts", "192.168.2.0/24",
		"bytes-in", "1000000",
		"bytes-out", "2000000",
		"packets-in", "5000",
		"packets-out", "6000",
		"install-time", "3600",
	)

	childSAs := newTestMsg("net-east", MessageGetter(childSA))

	ikeSA := newTestMsg(
		"uniqueid", "1",
		"remote-host", "10.0.0.1",
		"state", "ESTABLISHED",
		"established", "7200",
		"child-sas", MessageGetter(childSAs),
	)

	msg := newTestMsg("gw-east", MessageGetter(ikeSA))

	sas := parseIKESAs([]MessageGetter{msg})
	if len(sas) != 1 {
		t.Fatalf("expected 1 SA, got %d", len(sas))
	}

	sa := sas[0]
	if sa.Name != "gw-east" {
		t.Errorf("expected name 'gw-east', got %q", sa.Name)
	}
	if sa.UID != "1" {
		t.Errorf("expected UID '1', got %q", sa.UID)
	}
	if sa.RemoteHost != "10.0.0.1" {
		t.Errorf("expected remote host '10.0.0.1', got %q", sa.RemoteHost)
	}
	if sa.State != 2 {
		t.Errorf("expected state 2, got %d", sa.State)
	}
	if sa.EstablishedSecs != 7200 {
		t.Errorf("expected established 7200, got %f", sa.EstablishedSecs)
	}
	if len(sa.ChildSAs) != 1 {
		t.Fatalf("expected 1 child SA, got %d", len(sa.ChildSAs))
	}

	child := sa.ChildSAs[0]
	if child.Name != "net-east" {
		t.Errorf("expected child name 'net-east', got %q", child.Name)
	}
	if child.BytesIn != 1000000 {
		t.Errorf("expected bytes in 1000000, got %d", child.BytesIn)
	}
	if child.PacketsOut != 6000 {
		t.Errorf("expected packets out 6000, got %d", child.PacketsOut)
	}
}

func TestParseIKESAsNilSection(t *testing.T) {
	// IKE SA with nil section value (non-MessageGetter).
	msg := newTestMsg("gw", "not-a-section")
	sas := parseIKESAs([]MessageGetter{msg})
	if len(sas) != 0 {
		t.Errorf("expected 0 SAs for non-section value, got %d", len(sas))
	}
}

func TestParseIKESAsNoEstablished(t *testing.T) {
	// IKE SA without established field.
	ikeSA := newTestMsg(
		"uniqueid", "1",
		"remote-host", "10.0.0.1",
		"state", "CONNECTING",
	)
	msg := newTestMsg("gw", MessageGetter(ikeSA))
	sas := parseIKESAs([]MessageGetter{msg})
	if len(sas) != 1 {
		t.Fatalf("expected 1 SA, got %d", len(sas))
	}
	if sas[0].EstablishedSecs != 0 {
		t.Errorf("expected 0 established seconds, got %f", sas[0].EstablishedSecs)
	}
}

func TestParseIKESAsNilChildSection(t *testing.T) {
	// Child SA with nil sub-section value.
	childSAs := newTestMsg("child1", "not-a-section")
	ikeSA := newTestMsg(
		"uniqueid", "1",
		"remote-host", "10.0.0.1",
		"state", "ESTABLISHED",
		"child-sas", MessageGetter(childSAs),
	)
	msg := newTestMsg("gw", MessageGetter(ikeSA))
	sas := parseIKESAs([]MessageGetter{msg})
	if len(sas) != 1 {
		t.Fatalf("expected 1 SA, got %d", len(sas))
	}
	if len(sas[0].ChildSAs) != 0 {
		t.Errorf("expected 0 child SAs for non-section child, got %d", len(sas[0].ChildSAs))
	}
}

func TestParseCharonStats(t *testing.T) {
	uptimeSection := newTestMsg("running", "86400.5")
	workersSection := newTestMsg("total", "16", "idle", "12", "active", "4")
	queuesSection := newTestMsg("critical", "0", "high", "1", "medium", "2", "low", "3")
	ikesasSection := newTestMsg("half-open", "5")

	msg := newTestMsg(
		"uptime", MessageGetter(uptimeSection),
		"workers", MessageGetter(workersSection),
		"queues", MessageGetter(queuesSection),
		"ikesas", MessageGetter(ikesasSection),
	)

	stats := parseCharonStats(msg)
	if stats.Uptime != 86400.5 {
		t.Errorf("expected uptime 86400.5, got %f", stats.Uptime)
	}
	if stats.Workers != 16 {
		t.Errorf("expected workers 16, got %d", stats.Workers)
	}
	if stats.IdleWorkers != 12 {
		t.Errorf("expected idle workers 12, got %d", stats.IdleWorkers)
	}
	if stats.ActiveWorkers != 4 {
		t.Errorf("expected active workers 4, got %d", stats.ActiveWorkers)
	}
	if stats.HalfOpenIKE != 5 {
		t.Errorf("expected half-open 5, got %d", stats.HalfOpenIKE)
	}
	if stats.Queues["critical"] != 0 || stats.Queues["high"] != 1 || stats.Queues["medium"] != 2 || stats.Queues["low"] != 3 {
		t.Errorf("unexpected queues: %v", stats.Queues)
	}
}

func TestParseCharonStatsPartial(t *testing.T) {
	// Message with no sub-sections at all.
	msg := newTestMsg()
	stats := parseCharonStats(msg)
	if stats.Uptime != 0 {
		t.Errorf("expected uptime 0, got %f", stats.Uptime)
	}
	if stats.Workers != 0 {
		t.Errorf("expected workers 0, got %d", stats.Workers)
	}
	if len(stats.Queues) != 0 {
		t.Errorf("expected empty queues, got %v", stats.Queues)
	}
}

func TestWrapMsg(t *testing.T) {
	// nil message should return nil.
	if got := wrapMsg(nil); got != nil {
		t.Error("wrapMsg(nil) should return nil")
	}

	// Real vici.Message should be wrapped.
	m := vici.NewMessage()
	m.Set("testkey", "testval")
	wrapped := wrapMsg(m)
	if wrapped == nil {
		t.Fatal("wrapMsg should not return nil for non-nil message")
	}

	// Test Get.
	if got := wrapped.Get("testkey"); got != "testval" {
		t.Errorf("Get(testkey) = %v, want 'testval'", got)
	}
	if got := wrapped.Get("missing"); got != nil {
		t.Errorf("Get(missing) = %v, want nil", got)
	}

	// Test Keys.
	keys := wrapped.Keys()
	if len(keys) != 1 || keys[0] != "testkey" {
		t.Errorf("Keys() = %v, want [testkey]", keys)
	}
}

func TestWrapMsgs(t *testing.T) {
	m1 := vici.NewMessage()
	m1.Set("key1", "val1")
	m2 := vici.NewMessage()
	m2.Set("key2", "val2")

	wrapped := wrapMsgs([]*vici.Message{m1, m2})
	if len(wrapped) != 2 {
		t.Fatalf("expected 2 wrapped messages, got %d", len(wrapped))
	}
	if msgStr(wrapped[0], "key1") != "val1" {
		t.Error("first message not wrapped correctly")
	}
	if msgStr(wrapped[1], "key2") != "val2" {
		t.Error("second message not wrapped correctly")
	}

	// Empty slice.
	empty := wrapMsgs(nil)
	if len(empty) != 0 {
		t.Errorf("expected 0 wrapped messages for nil, got %d", len(empty))
	}
}

func TestMsgSectionWithViciMessage(t *testing.T) {
	// Create a vici.Message with a sub-message.
	sub := vici.NewMessage()
	sub.Set("foo", "bar")

	parent := vici.NewMessage()
	parent.Set("child", sub)

	wrapped := wrapMsg(parent)
	section := msgSection(wrapped, "child")
	if section == nil {
		t.Fatal("msgSection should return sub-section")
	}
	if got := msgStr(section, "foo"); got != "bar" {
		t.Errorf("expected 'bar', got %q", got)
	}
}

// mockSession implements viciSession for testing.
type mockSession struct {
	streamedMsgs []*vici.Message
	streamedErr  error
	commandMsg   *vici.Message
	commandErr   error
}

func (m *mockSession) Close() error { return nil }
func (m *mockSession) StreamedCommandRequest(cmd, event string, msg *vici.Message) ([]*vici.Message, error) {
	return m.streamedMsgs, m.streamedErr
}
func (m *mockSession) CommandRequest(cmd string, msg *vici.Message) (*vici.Message, error) {
	return m.commandMsg, m.commandErr
}

func TestViciClientIsAvailable(t *testing.T) {
	origFn := newViciSessionFn
	defer func() { newViciSessionFn = origFn }()

	// Available case.
	newViciSessionFn = func(socketPath string) (viciSession, error) {
		return &mockSession{}, nil
	}
	client := &viciClient{socketPath: "/test"}
	if !client.IsAvailable() {
		t.Error("expected IsAvailable to return true")
	}

	// Unavailable case.
	newViciSessionFn = func(socketPath string) (viciSession, error) {
		return nil, fmt.Errorf("connection refused")
	}
	if client.IsAvailable() {
		t.Error("expected IsAvailable to return false")
	}
}

func TestViciClientListSAs(t *testing.T) {
	origFn := newViciSessionFn
	defer func() { newViciSessionFn = origFn }()

	// Build a vici.Message with SA data.
	ikeSA := vici.NewMessage()
	ikeSA.Set("uniqueid", "1")
	ikeSA.Set("remote-host", "10.0.0.1")
	ikeSA.Set("state", "ESTABLISHED")
	ikeSA.Set("established", "7200")

	topMsg := vici.NewMessage()
	topMsg.Set("gw-east", ikeSA)

	newViciSessionFn = func(socketPath string) (viciSession, error) {
		return &mockSession{streamedMsgs: []*vici.Message{topMsg}}, nil
	}

	client := &viciClient{socketPath: "/test"}
	sas, err := client.ListSAs()
	if err != nil {
		t.Fatalf("ListSAs: %v", err)
	}
	if len(sas) != 1 {
		t.Fatalf("expected 1 SA, got %d", len(sas))
	}
	if sas[0].Name != "gw-east" || sas[0].State != 2 {
		t.Errorf("unexpected SA: %+v", sas[0])
	}
}

func TestViciClientListSAsDialError(t *testing.T) {
	origFn := newViciSessionFn
	defer func() { newViciSessionFn = origFn }()

	newViciSessionFn = func(socketPath string) (viciSession, error) {
		return nil, fmt.Errorf("connection refused")
	}

	client := &viciClient{socketPath: "/test"}
	_, err := client.ListSAs()
	if err == nil {
		t.Error("expected error")
	}
}

func TestViciClientListSAsStreamError(t *testing.T) {
	origFn := newViciSessionFn
	defer func() { newViciSessionFn = origFn }()

	newViciSessionFn = func(socketPath string) (viciSession, error) {
		return &mockSession{streamedErr: fmt.Errorf("stream error")}, nil
	}

	client := &viciClient{socketPath: "/test"}
	_, err := client.ListSAs()
	if err == nil {
		t.Error("expected error")
	}
}

func TestViciClientGetStats(t *testing.T) {
	origFn := newViciSessionFn
	defer func() { newViciSessionFn = origFn }()

	uptimeSection := vici.NewMessage()
	uptimeSection.Set("running", "3600")

	statsMsg := vici.NewMessage()
	statsMsg.Set("uptime", uptimeSection)

	newViciSessionFn = func(socketPath string) (viciSession, error) {
		return &mockSession{commandMsg: statsMsg}, nil
	}

	client := &viciClient{socketPath: "/test"}
	stats, err := client.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.Uptime != 3600 {
		t.Errorf("expected uptime 3600, got %f", stats.Uptime)
	}
}

func TestViciClientGetStatsDialError(t *testing.T) {
	origFn := newViciSessionFn
	defer func() { newViciSessionFn = origFn }()

	newViciSessionFn = func(socketPath string) (viciSession, error) {
		return nil, fmt.Errorf("connection refused")
	}

	client := &viciClient{socketPath: "/test"}
	_, err := client.GetStats()
	if err == nil {
		t.Error("expected error")
	}
}

func TestViciClientGetStatsCommandError(t *testing.T) {
	origFn := newViciSessionFn
	defer func() { newViciSessionFn = origFn }()

	newViciSessionFn = func(socketPath string) (viciSession, error) {
		return &mockSession{commandErr: fmt.Errorf("command error")}, nil
	}

	client := &viciClient{socketPath: "/test"}
	_, err := client.GetStats()
	if err == nil {
		t.Error("expected error")
	}
}

// Ensure fmt import is used (used in fmt.Sprintf and fmt.Errorf above).
