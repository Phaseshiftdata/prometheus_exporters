package firewall

import (
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
)

// Compile-time interface checks.
var (
	_ NftablesReader      = (*mockReader)(nil)
	_ collector.Collector = (*firewallCollector)(nil)
)

// mockReader implements NftablesReader for testing.
type mockReader struct {
	rules     []RuleInfo
	policies  []ChainPolicy
	rulesErr  error
	policyErr error
}

func (m *mockReader) GetDropRejectRules() ([]RuleInfo, error) {
	return m.rules, m.rulesErr
}

func (m *mockReader) GetChainPolicies() ([]ChainPolicy, error) {
	return m.policies, m.policyErr
}

func TestName(t *testing.T) {
	c := NewWithReader(&mockReader{})
	if c.Name() != "firewall" {
		t.Errorf("expected name 'firewall', got %q", c.Name())
	}
}

func TestDescribe(t *testing.T) {
	c := NewWithReader(&mockReader{})
	ch := make(chan *prometheus.Desc, 10)
	c.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	if len(descs) != 7 {
		t.Fatalf("expected 7 descriptors, got %d", len(descs))
	}
}

func TestDropRules(t *testing.T) {
	reader := &mockReader{
		rules: []RuleInfo{
			{
				Family:  "ip",
				Table:   "filter",
				Chain:   "input",
				Rule:    "block-ssh",
				Verdict: "drop",
				Packets: 100,
				Bytes:   5000,
			},
		},
	}

	c := NewWithReader(reader)
	expected := `
# HELP network_firewall_drop_packets_total Total packets dropped by nftables DROP rules.
# TYPE network_firewall_drop_packets_total counter
network_firewall_drop_packets_total{chain="input",family="ip",rule="block-ssh",table="filter"} 100
# HELP network_firewall_drop_bytes_total Total bytes dropped by nftables DROP rules.
# TYPE network_firewall_drop_bytes_total counter
network_firewall_drop_bytes_total{chain="input",family="ip",rule="block-ssh",table="filter"} 5000
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"network_firewall_drop_packets_total",
		"network_firewall_drop_bytes_total",
	); err != nil {
		t.Error(err)
	}
}

func TestRejectRules(t *testing.T) {
	reader := &mockReader{
		rules: []RuleInfo{
			{
				Family:  "ip6",
				Table:   "filter",
				Chain:   "input",
				Rule:    "reject-telnet",
				Verdict: "reject",
				Packets: 42,
				Bytes:   2100,
			},
		},
	}

	c := NewWithReader(reader)
	expected := `
# HELP network_firewall_reject_packets_total Total packets rejected by nftables REJECT rules.
# TYPE network_firewall_reject_packets_total counter
network_firewall_reject_packets_total{chain="input",family="ip6",rule="reject-telnet",table="filter"} 42
# HELP network_firewall_reject_bytes_total Total bytes rejected by nftables REJECT rules.
# TYPE network_firewall_reject_bytes_total counter
network_firewall_reject_bytes_total{chain="input",family="ip6",rule="reject-telnet",table="filter"} 2100
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"network_firewall_reject_packets_total",
		"network_firewall_reject_bytes_total",
	); err != nil {
		t.Error(err)
	}
}

func TestChainDefaultDropPolicy(t *testing.T) {
	reader := &mockReader{
		policies: []ChainPolicy{
			{
				Family:  "inet",
				Table:   "filter",
				Chain:   "input",
				Policy:  "drop",
				Packets: 999,
				Bytes:   88888,
			},
		},
	}

	c := NewWithReader(reader)
	expected := `
# HELP network_firewall_policy_drop_packets_total Total packets dropped by chain default DROP policy.
# TYPE network_firewall_policy_drop_packets_total counter
network_firewall_policy_drop_packets_total{chain="input",family="inet",table="filter"} 999
# HELP network_firewall_policy_drop_bytes_total Total bytes dropped by chain default DROP policy.
# TYPE network_firewall_policy_drop_bytes_total counter
network_firewall_policy_drop_bytes_total{chain="input",family="inet",table="filter"} 88888
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"network_firewall_policy_drop_packets_total",
		"network_firewall_policy_drop_bytes_total",
	); err != nil {
		t.Error(err)
	}
}

func TestAcceptPolicyIsIgnored(t *testing.T) {
	reader := &mockReader{
		policies: []ChainPolicy{
			{
				Family:  "ip",
				Table:   "filter",
				Chain:   "output",
				Policy:  "accept",
				Packets: 500,
				Bytes:   25000,
			},
		},
	}

	c := NewWithReader(reader)
	// No policy metrics should be emitted for accept policy.
	count := testutil.CollectAndCount(c,
		"network_firewall_policy_drop_packets_total",
		"network_firewall_policy_drop_bytes_total",
	)
	if count != 0 {
		t.Errorf("expected 0 policy metrics for accept policy, got %d", count)
	}
}

func TestEmptyRuleset(t *testing.T) {
	reader := &mockReader{}
	c := NewWithReader(reader)

	count := testutil.CollectAndCount(c,
		"network_firewall_drop_packets_total",
		"network_firewall_drop_bytes_total",
		"network_firewall_reject_packets_total",
		"network_firewall_reject_bytes_total",
		"network_firewall_policy_drop_packets_total",
		"network_firewall_policy_drop_bytes_total",
	)
	if count != 0 {
		t.Errorf("expected 0 metrics for empty ruleset, got %d", count)
	}
}

func TestRulesWithCommentVsPositionIndex(t *testing.T) {
	reader := &mockReader{
		rules: []RuleInfo{
			{
				Family:  "ip",
				Table:   "filter",
				Chain:   "forward",
				Rule:    "block-forward-traffic",
				Verdict: "drop",
				Packets: 10,
				Bytes:   500,
			},
			{
				Family:  "ip",
				Table:   "filter",
				Chain:   "forward",
				Rule:    "1",
				Verdict: "drop",
				Packets: 20,
				Bytes:   1000,
			},
		},
	}

	c := NewWithReader(reader)
	expected := `
# HELP network_firewall_drop_packets_total Total packets dropped by nftables DROP rules.
# TYPE network_firewall_drop_packets_total counter
network_firewall_drop_packets_total{chain="forward",family="ip",rule="block-forward-traffic",table="filter"} 10
network_firewall_drop_packets_total{chain="forward",family="ip",rule="1",table="filter"} 20
# HELP network_firewall_drop_bytes_total Total bytes dropped by nftables DROP rules.
# TYPE network_firewall_drop_bytes_total counter
network_firewall_drop_bytes_total{chain="forward",family="ip",rule="block-forward-traffic",table="filter"} 500
network_firewall_drop_bytes_total{chain="forward",family="ip",rule="1",table="filter"} 1000
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"network_firewall_drop_packets_total",
		"network_firewall_drop_bytes_total",
	); err != nil {
		t.Error(err)
	}
}

func TestMixedFamilies(t *testing.T) {
	reader := &mockReader{
		rules: []RuleInfo{
			{
				Family:  "ip",
				Table:   "filter",
				Chain:   "input",
				Rule:    "0",
				Verdict: "drop",
				Packets: 50,
				Bytes:   2500,
			},
			{
				Family:  "inet",
				Table:   "filter",
				Chain:   "input",
				Rule:    "rate-limit",
				Verdict: "reject",
				Packets: 30,
				Bytes:   1500,
			},
		},
		policies: []ChainPolicy{
			{
				Family:  "ip",
				Table:   "filter",
				Chain:   "input",
				Policy:  "drop",
				Packets: 200,
				Bytes:   10000,
			},
			{
				Family:  "inet",
				Table:   "filter",
				Chain:   "input",
				Policy:  "drop",
				Packets: 300,
				Bytes:   15000,
			},
		},
	}

	c := NewWithReader(reader)
	expected := `
# HELP network_firewall_drop_packets_total Total packets dropped by nftables DROP rules.
# TYPE network_firewall_drop_packets_total counter
network_firewall_drop_packets_total{chain="input",family="ip",rule="0",table="filter"} 50
# HELP network_firewall_drop_bytes_total Total bytes dropped by nftables DROP rules.
# TYPE network_firewall_drop_bytes_total counter
network_firewall_drop_bytes_total{chain="input",family="ip",rule="0",table="filter"} 2500
# HELP network_firewall_reject_packets_total Total packets rejected by nftables REJECT rules.
# TYPE network_firewall_reject_packets_total counter
network_firewall_reject_packets_total{chain="input",family="inet",rule="rate-limit",table="filter"} 30
# HELP network_firewall_reject_bytes_total Total bytes rejected by nftables REJECT rules.
# TYPE network_firewall_reject_bytes_total counter
network_firewall_reject_bytes_total{chain="input",family="inet",rule="rate-limit",table="filter"} 1500
# HELP network_firewall_policy_drop_packets_total Total packets dropped by chain default DROP policy.
# TYPE network_firewall_policy_drop_packets_total counter
network_firewall_policy_drop_packets_total{chain="input",family="ip",table="filter"} 200
network_firewall_policy_drop_packets_total{chain="input",family="inet",table="filter"} 300
# HELP network_firewall_policy_drop_bytes_total Total bytes dropped by chain default DROP policy.
# TYPE network_firewall_policy_drop_bytes_total counter
network_firewall_policy_drop_bytes_total{chain="input",family="ip",table="filter"} 10000
network_firewall_policy_drop_bytes_total{chain="input",family="inet",table="filter"} 15000
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"network_firewall_drop_packets_total",
		"network_firewall_drop_bytes_total",
		"network_firewall_reject_packets_total",
		"network_firewall_reject_bytes_total",
		"network_firewall_policy_drop_packets_total",
		"network_firewall_policy_drop_bytes_total",
	); err != nil {
		t.Error(err)
	}
}

// collectorUpDown is the expected exposition when the collector reports itself
// unable to read nftables.
const collectorUpDown = `
# HELP network_firewall_collector_up Whether nftables counters could be read (1 = collecting, 0 = unavailable).
# TYPE network_firewall_collector_up gauge
network_firewall_collector_up 0
`

// collectorUpOK is the expected exposition when nftables was read successfully.
const collectorUpOK = `
# HELP network_firewall_collector_up Whether nftables counters could be read (1 = collecting, 0 = unavailable).
# TYPE network_firewall_collector_up gauge
network_firewall_collector_up 1
`

func TestRulesError(t *testing.T) {
	reader := &mockReader{
		rulesErr: errors.New("nft not found"),
	}

	// A read failure must degrade to up=0, not to an invalid metric: an invalid
	// metric fails the entire gather and takes every other collector with it.
	c := NewWithReader(reader)
	if err := testutil.CollectAndCompare(c, strings.NewReader(collectorUpDown),
		"network_firewall_collector_up",
	); err != nil {
		t.Error(err)
	}
}

func TestPolicyError(t *testing.T) {
	reader := &mockReader{
		rules:     []RuleInfo{}, // no rules error
		policyErr: errors.New("permission denied"),
	}

	c := NewWithReader(reader)
	if err := testutil.CollectAndCompare(c, strings.NewReader(collectorUpDown),
		"network_firewall_collector_up",
	); err != nil {
		t.Error(err)
	}
}

func TestCollectorUpOnSuccess(t *testing.T) {
	reader := &mockReader{
		rules: []RuleInfo{
			{Family: "ip", Table: "filter", Chain: "input", Rule: "block-ssh", Verdict: "drop", Packets: 1, Bytes: 2},
		},
	}

	c := NewWithReader(reader)
	if err := testutil.CollectAndCompare(c, strings.NewReader(collectorUpOK),
		"network_firewall_collector_up",
	); err != nil {
		t.Error(err)
	}
}

func TestNewWhenNetlinkIsUnreachableIsPermanentlyDown(t *testing.T) {
	origFn := probeReaderFn
	defer func() { probeReaderFn = origFn }()
	probeReaderFn = func() (NftablesReader, string) {
		return nil, "no CAP_NET_ADMIN for NETLINK_NETFILTER in this network namespace: operation not permitted"
	}

	// This is a container that lost NET_ADMIN: the socket will keep refusing
	// every message for as long as the process lives.
	c := New()
	if err := testutil.CollectAndCompare(c, strings.NewReader(collectorUpDown),
		"network_firewall_collector_up",
	); err != nil {
		t.Error(err)
	}

	// Nothing else may be emitted, and repeated scrapes must stay quiet and
	// error-free rather than failing the gather every 30 seconds.
	for i := 0; i < 3; i++ {
		if n := testutil.CollectAndCount(c); n != 1 {
			t.Errorf("scrape %d: expected exactly the up gauge, got %d metrics", i, n)
		}
	}
}

func TestNewWithReachableNetlinkUsesTheProbedReader(t *testing.T) {
	origFn := probeReaderFn
	defer func() { probeReaderFn = origFn }()
	probeReaderFn = func() (NftablesReader, string) {
		return &mockReader{
			policies: []ChainPolicy{
				{Family: "ip", Table: "filter", Chain: "input", Policy: "drop"},
			},
		}, ""
	}

	c := New()
	if err := testutil.CollectAndCompare(c, strings.NewReader(collectorUpOK),
		"network_firewall_collector_up",
	); err != nil {
		t.Error(err)
	}
}

func TestMultipleDropRulesInSameChain(t *testing.T) {
	reader := &mockReader{
		rules: []RuleInfo{
			{
				Family:  "ip",
				Table:   "filter",
				Chain:   "input",
				Rule:    "block-ssh",
				Verdict: "drop",
				Packets: 100,
				Bytes:   5000,
			},
			{
				Family:  "ip",
				Table:   "filter",
				Chain:   "input",
				Rule:    "block-telnet",
				Verdict: "drop",
				Packets: 200,
				Bytes:   10000,
			},
		},
	}

	c := NewWithReader(reader)
	count := testutil.CollectAndCount(c,
		"network_firewall_drop_packets_total",
	)
	if count != 2 {
		t.Errorf("expected 2 drop packet metrics, got %d", count)
	}
}

// TestNew exercises the unstubbed constructor, which really does open a
// NETLINK_NETFILTER socket and really does ask the kernel for its chains.
//
// It asserts nothing about the outcome on purpose. The CI runner has no
// CAP_NET_ADMIN, so the probe there fails with EPERM and returns a
// permanently-down collector; a developer's Linux box may well have both the
// capability and a ruleset, and get a live one. Both are correct. What is
// being checked is that neither path panics, returns nil, or blocks -- which
// is exactly the class of thing a nil netlink connection or an unclosed
// lasting socket would do.
func TestNew(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.Name() != "firewall" {
		t.Errorf("expected name 'firewall', got %q", c.Name())
	}
	// A scrape must always produce the up gauge, whichever way the probe went.
	if n := testutil.CollectAndCount(c, "network_firewall_collector_up"); n != 1 {
		t.Errorf("expected exactly one up gauge, got %d", n)
	}
}

func TestDropAndRejectMixed(t *testing.T) {
	reader := &mockReader{
		rules: []RuleInfo{
			{
				Family:  "ip",
				Table:   "filter",
				Chain:   "input",
				Rule:    "0",
				Verdict: "drop",
				Packets: 10,
				Bytes:   500,
			},
			{
				Family:  "ip",
				Table:   "filter",
				Chain:   "input",
				Rule:    "1",
				Verdict: "reject",
				Packets: 20,
				Bytes:   1000,
			},
		},
	}

	c := NewWithReader(reader)
	dropCount := testutil.CollectAndCount(c,
		"network_firewall_drop_packets_total",
		"network_firewall_drop_bytes_total",
	)
	if dropCount != 2 {
		t.Errorf("expected 2 drop metrics, got %d", dropCount)
	}
}
