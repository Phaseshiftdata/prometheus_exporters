package firewall

import (
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/mdlayher/netlink"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sys/unix"
)

// This file tests the netlink reader two ways, because neither alone is
// enough and neither can be replaced by running the real thing.
//
// The first way is a fake nftConn: it covers the walk over chains, the error
// paths, and the label arithmetic without any encoding involved. The second
// way goes through the real *nftables.Conn with a stubbed socket, feeding it
// netlink messages laid out the way the kernel lays them out, so that the
// decode -- attribute numbers, nesting, big-endian counters, the libnftnl TLV
// a rule comment hides in -- is exercised rather than assumed. Two of those
// payloads are verbatim captures from a real kernel (see the citations below);
// the rest are assembled here from the nf_tables UAPI constants, deliberately
// without using google/nftables' own marshallers, so that a test passing does
// not merely prove the library agrees with itself.
//
// What none of this can prove is that monitor01's actual ruleset decodes to
// the labels its dashboards expect. That needs the host, and the host is out
// of reach from CI.

// ---------------------------------------------------------------------------
// Fake connection
// ---------------------------------------------------------------------------

// fakeConn implements nftConn without a socket.
type fakeConn struct {
	chains   []*nftables.Chain
	rules    map[string][]*nftables.Rule
	listErr  error
	rulesErr error
	closes   int
}

var _ nftConn = (*fakeConn)(nil)

func (f *fakeConn) ListChains() ([]*nftables.Chain, error) {
	return f.chains, f.listErr
}

func (f *fakeConn) GetRules(_ *nftables.Table, c *nftables.Chain) ([]*nftables.Rule, error) {
	if f.rulesErr != nil {
		return nil, f.rulesErr
	}
	return f.rules[c.Name], nil
}

func (f *fakeConn) CloseLasting() error {
	f.closes++
	return nil
}

// readerFor returns a reader wired to the given connection.
func readerFor(c nftConn) *netlinkReader {
	return &netlinkReader{dial: func() (nftConn, error) { return c, nil }}
}

// readerFailing returns a reader whose dial always fails.
func readerFailing(err error) *netlinkReader {
	return &netlinkReader{dial: func() (nftConn, error) { return nil, err }}
}

// testChain builds a decoded chain the way ListChains would hand one over.
func testChain(family nftables.TableFamily, table, name string, policy *nftables.ChainPolicy) *nftables.Chain {
	return &nftables.Chain{
		Name:   name,
		Table:  &nftables.Table{Name: table, Family: family},
		Policy: policy,
	}
}

// ---------------------------------------------------------------------------
// Pure conversion
// ---------------------------------------------------------------------------

func TestFamilyName(t *testing.T) {
	tests := []struct {
		input nftables.TableFamily
		want  string
	}{
		{nftables.TableFamilyIPv4, "ip"},
		{nftables.TableFamilyIPv6, "ip6"},
		{nftables.TableFamilyINet, "inet"},
		{nftables.TableFamilyBridge, "bridge"},
		{nftables.TableFamilyARP, "arp"},
		{nftables.TableFamilyNetdev, "netdev"},
		{nftables.TableFamily(200), "unknown(200)"},
	}
	for _, tc := range tests {
		if got := familyName(tc.input); got != tc.want {
			t.Errorf("familyName(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestPolicyName(t *testing.T) {
	tests := []struct {
		input nftables.ChainPolicy
		want  string
	}{
		{nftables.ChainPolicyDrop, "drop"},
		{nftables.ChainPolicyAccept, "accept"},
		{nftables.ChainPolicy(42), "unknown(42)"},
	}
	for _, tc := range tests {
		if got := policyName(tc.input); got != tc.want {
			t.Errorf("policyName(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRuleLabelPrefersComment(t *testing.T) {
	tests := []struct {
		name     string
		userData []byte
		idx      int
		want     string
	}{
		{"comment wins", commentUserData("block-ssh"), 3, "block-ssh"},
		{"no userdata falls back to position", nil, 3, "3"},
		{"empty comment falls back to position", commentUserData(""), 7, "7"},
		{"unrelated tlv falls back to position", []byte{0x01, 0x02, 'x', 0x00}, 0, "0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ruleLabel(&nftables.Rule{UserData: tc.userData}, tc.idx)
			if got != tc.want {
				t.Errorf("ruleLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyRule(t *testing.T) {
	tests := []struct {
		name        string
		exprs       []expr.Any
		wantVerdict string
		wantPackets uint64
		wantBytes   uint64
	}{
		{
			name:        "counter then drop",
			exprs:       []expr.Any{&expr.Counter{Packets: 10, Bytes: 500}, &expr.Verdict{Kind: expr.VerdictDrop}},
			wantVerdict: "drop",
			wantPackets: 10,
			wantBytes:   500,
		},
		{
			name:        "counter then reject",
			exprs:       []expr.Any{&expr.Counter{Packets: 4, Bytes: 40}, &expr.Reject{}},
			wantVerdict: "reject",
			wantPackets: 4,
			wantBytes:   40,
		},
		{
			// The counters still come back; the caller discards them along with
			// the rule, because there is no accept metric to put them in.
			name:        "accept verdict is not ours",
			exprs:       []expr.Any{&expr.Counter{Packets: 9, Bytes: 90}, &expr.Verdict{Kind: expr.VerdictAccept}},
			wantVerdict: "",
			wantPackets: 9,
			wantBytes:   90,
		},
		{
			name:        "jump verdict is not ours",
			exprs:       []expr.Any{&expr.Verdict{Kind: expr.VerdictJump, Chain: "somewhere"}},
			wantVerdict: "",
		},
		{
			name:        "drop with no counter reports zero",
			exprs:       []expr.Any{&expr.Verdict{Kind: expr.VerdictDrop}},
			wantVerdict: "drop",
		},
		{
			name:        "unrelated expressions are ignored",
			exprs:       []expr.Any{&expr.Meta{}, &expr.Cmp{}},
			wantVerdict: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			verdict, packets, bytes := classifyRule(&nftables.Rule{Exprs: tc.exprs})
			if verdict != tc.wantVerdict {
				t.Errorf("verdict = %q, want %q", verdict, tc.wantVerdict)
			}
			if packets != tc.wantPackets || bytes != tc.wantBytes {
				t.Errorf("counters = %d/%d, want %d/%d", packets, bytes, tc.wantPackets, tc.wantBytes)
			}
		})
	}
}

// TestDropRejectRulesNumbersByChainPosition pins the label semantics that
// dashboards were built against: an uncommented rule is labeled with its
// position among ALL rules in its chain, not among the drop rules.
func TestDropRejectRulesNumbersByChainPosition(t *testing.T) {
	chain := testChain(nftables.TableFamilyIPv4, "filter", "input", nil)
	rules := []*nftables.Rule{
		{Exprs: []expr.Any{&expr.Verdict{Kind: expr.VerdictAccept}}}, // 0, filtered out
		nil, // 1, defensive
		{Exprs: []expr.Any{&expr.Counter{Packets: 7, Bytes: 70}, &expr.Verdict{Kind: expr.VerdictDrop}}}, // 2
		{
			UserData: commentUserData("named"),
			Exprs:    []expr.Any{&expr.Counter{Packets: 8, Bytes: 80}, &expr.Reject{}},
		}, // 3
	}

	got := dropRejectRules(chain, rules)
	if len(got) != 2 {
		t.Fatalf("expected 2 rules, got %d: %+v", len(got), got)
	}
	if got[0].Rule != "2" || got[0].Verdict != "drop" || got[0].Packets != 7 || got[0].Family != "ip" {
		t.Errorf("unexpected first rule: %+v", got[0])
	}
	if got[1].Rule != "named" || got[1].Verdict != "reject" || got[1].Bytes != 80 {
		t.Errorf("unexpected second rule: %+v", got[1])
	}
}

func TestChainPoliciesSkipsChainsWithoutOne(t *testing.T) {
	drop := nftables.ChainPolicyDrop
	accept := nftables.ChainPolicyAccept
	chains := []*nftables.Chain{
		testChain(nftables.TableFamilyINet, "filter", "input", &drop),
		testChain(nftables.TableFamilyINet, "filter", "output", &accept),
		testChain(nftables.TableFamilyINet, "filter", "jump-target", nil), // regular chain
		nil,
		{Name: "no-table", Policy: &drop}, // never happens, but must not panic
	}

	got := chainPolicies(chains)
	if len(got) != 2 {
		t.Fatalf("expected 2 policies, got %d: %+v", len(got), got)
	}
	if got[0].Chain != "input" || got[0].Policy != "drop" || got[0].Family != "inet" {
		t.Errorf("unexpected first policy: %+v", got[0])
	}
	if got[1].Chain != "output" || got[1].Policy != "accept" {
		t.Errorf("unexpected second policy: %+v", got[1])
	}
	// Chain policy counters are not available over this interface; they were
	// not available from nft(8) either. Zero, not invented.
	if got[0].Packets != 0 || got[0].Bytes != 0 {
		t.Errorf("expected zero policy counters, got %d/%d", got[0].Packets, got[0].Bytes)
	}
}

// ---------------------------------------------------------------------------
// Reader behavior against a fake connection
// ---------------------------------------------------------------------------

func TestGetDropRejectRulesWalksEveryChain(t *testing.T) {
	conn := &fakeConn{
		chains: []*nftables.Chain{
			testChain(nftables.TableFamilyIPv4, "filter", "input", nil),
			testChain(nftables.TableFamilyIPv6, "filter", "forward", nil),
			nil,                             // must not panic
			{Name: "tableless", Table: nil}, // must be skipped, not dereferenced
		},
		rules: map[string][]*nftables.Rule{
			"input": {{
				UserData: commentUserData("block-ssh"),
				Exprs:    []expr.Any{&expr.Counter{Packets: 100, Bytes: 5000}, &expr.Verdict{Kind: expr.VerdictDrop}},
			}},
			"forward": {{
				Exprs: []expr.Any{&expr.Counter{Packets: 42, Bytes: 2100}, &expr.Reject{}},
			}},
		},
	}

	got, err := readerFor(conn).GetDropRejectRules()
	if err != nil {
		t.Fatalf("GetDropRejectRules: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rules, got %d: %+v", len(got), got)
	}
	if got[0].Family != "ip" || got[0].Chain != "input" || got[0].Rule != "block-ssh" {
		t.Errorf("unexpected first rule: %+v", got[0])
	}
	if got[1].Family != "ip6" || got[1].Chain != "forward" || got[1].Rule != "0" {
		t.Errorf("unexpected second rule: %+v", got[1])
	}
	// The lasting socket must not leak: one dial, one close.
	if conn.closes != 1 {
		t.Errorf("expected the connection to be closed once, got %d", conn.closes)
	}
}

func TestGetDropRejectRulesDialError(t *testing.T) {
	if _, err := readerFailing(errors.New("boom")).GetDropRejectRules(); err == nil {
		t.Error("expected a dial error to surface")
	}
}

func TestGetDropRejectRulesListChainsError(t *testing.T) {
	conn := &fakeConn{listErr: errors.New("nope")}
	_, err := readerFor(conn).GetDropRejectRules()
	if err == nil {
		t.Fatal("expected an error")
	}
	if conn.closes != 1 {
		t.Errorf("expected the connection to be closed once, got %d", conn.closes)
	}
}

func TestGetDropRejectRulesGetRulesError(t *testing.T) {
	conn := &fakeConn{
		chains:   []*nftables.Chain{testChain(nftables.TableFamilyIPv4, "filter", "input", nil)},
		rulesErr: errors.New("table vanished"),
	}
	_, err := readerFor(conn).GetDropRejectRules()
	if err == nil {
		t.Fatal("expected an error")
	}
	// The message has to name the chain: "listing rules failed" on a host with
	// forty chains is not an actionable log line.
	if want := "listing rules in ip filter input"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not identify the chain (%q)", err, want)
	}
}

func TestGetChainPolicies(t *testing.T) {
	drop := nftables.ChainPolicyDrop
	conn := &fakeConn{
		chains: []*nftables.Chain{testChain(nftables.TableFamilyIPv4, "filter", "input", &drop)},
	}
	got, err := readerFor(conn).GetChainPolicies()
	if err != nil {
		t.Fatalf("GetChainPolicies: %v", err)
	}
	if len(got) != 1 || got[0].Policy != "drop" {
		t.Errorf("unexpected policies: %+v", got)
	}
	if conn.closes != 1 {
		t.Errorf("expected the connection to be closed once, got %d", conn.closes)
	}
}

func TestGetChainPoliciesDialError(t *testing.T) {
	if _, err := readerFailing(errors.New("boom")).GetChainPolicies(); err == nil {
		t.Error("expected a dial error to surface")
	}
}

func TestGetChainPoliciesListChainsError(t *testing.T) {
	conn := &fakeConn{listErr: errors.New("nope")}
	if _, err := readerFor(conn).GetChainPolicies(); err == nil {
		t.Error("expected an error")
	}
}

// ---------------------------------------------------------------------------
// Probe classification
// ---------------------------------------------------------------------------

func TestProbeClassification(t *testing.T) {
	tests := []struct {
		name       string
		dialErr    error
		listErr    error
		wantLatch  bool
		wantSubstr string
	}{
		{name: "healthy"},
		{
			name:       "no NET_ADMIN",
			listErr:    syscall.EPERM,
			wantLatch:  true,
			wantSubstr: "CAP_NET_ADMIN",
		},
		{
			name:       "access denied",
			listErr:    syscall.EACCES,
			wantLatch:  true,
			wantSubstr: "CAP_NET_ADMIN",
		},
		{
			name:       "no NETLINK_NETFILTER at dial time",
			dialErr:    syscall.EPROTONOSUPPORT,
			wantLatch:  true,
			wantSubstr: "nf_tables support",
		},
		{
			name:       "no address family",
			dialErr:    syscall.EAFNOSUPPORT,
			wantLatch:  true,
			wantSubstr: "nf_tables support",
		},
		{
			// A table being replaced under us, a truncated dump, ENOBUFS on a
			// busy socket: all recoverable, none may permanently disable the
			// collector.
			name:      "transient errors do not latch",
			listErr:   syscall.ENOBUFS,
			wantLatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r *netlinkReader
			if tc.dialErr != nil {
				r = readerFailing(tc.dialErr)
			} else {
				r = readerFor(&fakeConn{listErr: tc.listErr})
			}
			got := r.probe()
			if tc.wantLatch && got == "" {
				t.Fatalf("expected a permanent-unavailability reason, got none")
			}
			if !tc.wantLatch && got != "" {
				t.Fatalf("expected no latch, got %q", got)
			}
			if tc.wantSubstr != "" && !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("reason %q does not mention %q", got, tc.wantSubstr)
			}
		})
	}
}

// TestProbeWrappedErrorsStillClassify guards the classification against the
// several layers that sit between syscall and caller: mdlayher/netlink wraps
// a netlink error code in *netlink.OpError, and this package wraps that again
// with %w. errors.Is has to see through all of it or every EPERM would read
// as a transient fault and the collector would retry it forever.
func TestProbeWrappedErrorsStillClassify(t *testing.T) {
	wrapped := &netlink.OpError{Op: "receive", Err: syscall.EPERM}
	r := readerFor(&fakeConn{listErr: wrapped})
	if got := r.probe(); !strings.Contains(got, "CAP_NET_ADMIN") {
		t.Errorf("wrapped EPERM was not classified: %q", got)
	}
}

// TestDialNftablesWrapsConstructorFailure covers the one line in dialNftables
// that a real socket never reaches.
func TestDialNftablesWrapsConstructorFailure(t *testing.T) {
	orig := nftNewConnFn
	defer func() { nftNewConnFn = orig }()
	nftNewConnFn = func(_ int) (*nftables.Conn, error) { return nil, syscall.EPROTONOSUPPORT }

	_, err := dialNftables(-1)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, syscall.EPROTONOSUPPORT) {
		t.Errorf("wrapped error lost its cause: %v", err)
	}
	if !strings.Contains(err.Error(), "NETLINK_NETFILTER") {
		t.Errorf("error %q does not say which socket failed", err)
	}
}

// ---------------------------------------------------------------------------
// Network namespace support
// ---------------------------------------------------------------------------

func TestNewNetlinkReaderForNetNSOpensFile(t *testing.T) {
	// Create a temp file to stand in for /proc/1/ns/net.
	f, err := os.CreateTemp(t.TempDir(), "fake-netns")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	f.Close()

	origOpen := openNetNSFn
	defer func() { openNetNSFn = origOpen }()

	var openedPath string
	openNetNSFn = func(path string) (*os.File, error) {
		openedPath = path
		return os.Open(path) //nolint:gosec
	}

	// Also stub nftNewConnFn so we don't need a real netlink socket.
	origConn := nftNewConnFn
	defer func() { nftNewConnFn = origConn }()
	nftNewConnFn = func(netnsFd int) (*nftables.Conn, error) {
		if netnsFd < 0 {
			t.Error("expected a non-negative fd for namespace dial")
		}
		return nftables.New(
			nftables.AsLasting(),
			nftables.WithTestDial(func(reqs []netlink.Message) ([]netlink.Message, error) {
				return dumpReply(reqs[0], nil), nil
			}),
		)
	}

	r, err := newNetlinkReaderForNetNS(f.Name())
	if err != nil {
		t.Fatalf("newNetlinkReaderForNetNS: %v", err)
	}
	defer r.Close()

	if openedPath != f.Name() {
		t.Errorf("opened %q, want %q", openedPath, f.Name())
	}
	if r.netnsFile == nil {
		t.Error("expected netnsFile to be set")
	}
}

func TestNewNetlinkReaderForNetNSInvalidPath(t *testing.T) {
	_, err := newNetlinkReaderForNetNS("/nonexistent/ns/net")
	if err == nil {
		t.Fatal("expected an error for a nonexistent path")
	}
	if !strings.Contains(err.Error(), "opening host network namespace") {
		t.Errorf("error %q does not mention the namespace", err)
	}
}

func TestNetlinkReaderCloseWithoutNetNS(t *testing.T) {
	r := newNetlinkReader()
	if err := r.Close(); err != nil {
		t.Errorf("Close on reader without netns file: %v", err)
	}
}

func TestNewWithNetNSEmptyPathUsesCurrentNamespace(t *testing.T) {
	origFn := probeReaderFn
	defer func() { probeReaderFn = origFn }()

	var receivedPath string
	probeReaderFn = func(netnsPath string) (NftablesReader, string) {
		receivedPath = netnsPath
		return &mockReader{}, ""
	}

	c := NewWithNetNS("")
	if c == nil {
		t.Fatal("NewWithNetNS returned nil")
	}
	if receivedPath != "" {
		t.Errorf("expected empty path, got %q", receivedPath)
	}
}

func TestNewWithNetNSPassesPathToProbe(t *testing.T) {
	origFn := probeReaderFn
	defer func() { probeReaderFn = origFn }()

	var receivedPath string
	probeReaderFn = func(netnsPath string) (NftablesReader, string) {
		receivedPath = netnsPath
		return &mockReader{}, ""
	}

	c := NewWithNetNS("/proc/1/ns/net")
	if c == nil {
		t.Fatal("NewWithNetNS returned nil")
	}
	if receivedPath != "/proc/1/ns/net" {
		t.Errorf("expected /proc/1/ns/net, got %q", receivedPath)
	}
}

func TestNewWithNetNSInvalidPathReportsDown(t *testing.T) {
	// Use the real probeReaderFn -- it will fail to open the path.
	c := NewWithNetNS("/nonexistent/ns/net")
	if c == nil {
		t.Fatal("NewWithNetNS returned nil")
	}
	if c.Name() != "firewall" {
		t.Errorf("expected name 'firewall', got %q", c.Name())
	}
	// The collector must report down, not panic.
	ch := make(chan prometheus.Metric, 10)
	c.Collect(ch)
	close(ch)
	var found bool
	for m := range ch {
		desc := m.Desc().String()
		if strings.Contains(desc, "network_firewall_collector_up") {
			found = true
		}
	}
	if !found {
		t.Error("expected network_firewall_collector_up metric")
	}
}

func TestDialNftablesPassesFd(t *testing.T) {
	orig := nftNewConnFn
	defer func() { nftNewConnFn = orig }()

	var receivedFd int
	nftNewConnFn = func(netnsFd int) (*nftables.Conn, error) {
		receivedFd = netnsFd
		return nil, syscall.EPROTONOSUPPORT // Don't need a real conn for this test.
	}

	_, _ = dialNftables(42)
	if receivedFd != 42 {
		t.Errorf("expected fd 42, got %d", receivedFd)
	}

	_, _ = dialNftables(-1)
	if receivedFd != -1 {
		t.Errorf("expected fd -1, got %d", receivedFd)
	}
}

// ---------------------------------------------------------------------------
// Decode, through the real *nftables.Conn against synthesized kernel traffic
// ---------------------------------------------------------------------------

// capturedChainDump is a verbatim NFT_MSG_GETCHAIN dump reply from a real
// kernel, taken from google/nftables' own TestListChains (Apache-2.0). It is
// the `inet filter` table -- its nfgenmsg family byte is NFPROTO_INET, which
// is where the "inet" family label below comes from -- holding the three
// standard base chains (input accept, forward drop, output accept) plus a
// regular chain with no policy at all.
//
// Real bytes rather than bytes this test assembled: they are the only thing
// here that proves the attribute layout being decoded is the layout a kernel
// actually emits, rather than the one this file happens to believe in.
var capturedChainDump = [][]byte{
	// chain input { type filter hook input priority filter; policy accept; }
	[]byte("\x70\x00\x00\x00\x03\x0a\x02\x00\x00\x00\x00\x00\xb8\x76\x02\x00\x01\x00\x00\xc3\x0b\x00\x01\x00\x66\x69\x6c\x74\x65\x72\x00\x00\x0c\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x01\x0a\x00\x03\x00\x69\x6e\x70\x75\x74\x00\x00\x00\x14\x00\x04\x00\x08\x00\x01\x00\x00\x00\x00\x01\x08\x00\x02\x00\x00\x00\x00\x00\x08\x00\x05\x00\x00\x00\x00\x01\x0b\x00\x07\x00\x66\x69\x6c\x74\x65\x72\x00\x00\x08\x00\x0a\x00\x00\x00\x00\x01\x08\x00\x06\x00\x00\x00\x00\x00"),
	// chain forward { type filter hook forward priority filter; policy drop; }
	[]byte("\x70\x00\x00\x00\x03\x0a\x02\x00\x00\x00\x00\x01\xb8\x76\x02\x00\x01\x00\x00\xc3\x0b\x00\x01\x00\x66\x69\x6c\x74\x65\x72\x00\x00\x0c\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x02\x0c\x00\x03\x00\x66\x6f\x72\x77\x61\x72\x64\x00\x14\x00\x04\x00\x08\x00\x01\x00\x00\x00\x00\x02\x08\x00\x02\x00\x00\x00\x00\x00\x08\x00\x05\x00\x00\x00\x00\x00\x0b\x00\x07\x00\x66\x69\x6c\x74\x65\x72\x00\x00\x08\x00\x0a\x00\x00\x00\x00\x01\x08\x00\x06\x00\x00\x00\x00\x00"),
	// chain output { type filter hook output priority filter; policy accept; }
	[]byte("\x70\x00\x00\x00\x03\x0a\x02\x00\x00\x00\x00\x02\xb8\x76\x02\x00\x01\x00\x00\xc3\x0b\x00\x01\x00\x66\x69\x6c\x74\x65\x72\x00\x00\x0c\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x03\x0b\x00\x03\x00\x6f\x75\x74\x70\x75\x74\x00\x00\x14\x00\x04\x00\x08\x00\x01\x00\x00\x00\x00\x03\x08\x00\x02\x00\x00\x00\x00\x00\x08\x00\x05\x00\x00\x00\x00\x01\x0b\x00\x07\x00\x66\x69\x6c\x74\x65\x72\x00\x00\x08\x00\x0a\x00\x00\x00\x00\x01\x08\x00\x06\x00\x00\x00\x00\x00"),
	// chain undef { counter packets 56235 bytes 175436495 return } -- no policy
	[]byte("\x40\x00\x00\x00\x03\x0a\x02\x00\x00\x00\x00\x03\xb8\x76\x02\x00\x01\x00\x00\xc3\x0b\x00\x01\x00\x66\x69\x6c\x74\x65\x72\x00\x00\x0c\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x04\x0a\x00\x03\x00\x75\x6e\x64\x65\x66\x00\x00\x00\x08\x00\x06\x00\x00\x00\x00\x01"),
}

// capturedCounterOnlyRule is a verbatim NFT_MSG_GETRULE dump reply from a real
// kernel, taken from google/nftables' own TestGetRules (Apache-2.0), captured
// with `strace -eraw=sendto nft list chain ip filter forward`. It is a rule
// carrying nothing but `counter packets 673497 bytes 1838301216` -- no
// verdict, so this collector must ignore it entirely while still counting it
// for the positional rule label.
var capturedCounterOnlyRule = netlink.Message{
	Header: netlink.Header{Length: 0x68, Type: 0xa06, Flags: 0x802},
	Data: []uint8{
		0x2, 0x0, 0x0, 0xc, 0xb, 0x0, 0x1, 0x0, 0x66, 0x69, 0x6c, 0x74, 0x65, 0x72, 0x0, 0x0,
		0xc, 0x0, 0x2, 0x0, 0x66, 0x6f, 0x72, 0x77, 0x61, 0x72, 0x64, 0x0, 0xc, 0x0, 0x3, 0x0,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2, 0x30, 0x0, 0x4, 0x0, 0x2c, 0x0, 0x1, 0x0,
		0xc, 0x0, 0x1, 0x0, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x65, 0x72, 0x0, 0x1c, 0x0, 0x2, 0x0,
		0xc, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x6d, 0x92, 0x20, 0x20, 0xc, 0x0, 0x2, 0x0,
		0x0, 0x0, 0x0, 0x0, 0x0, 0xa, 0x48, 0xd9,
	},
}

// TestDecodeCapturedChainDump runs a real kernel chain dump through the real
// decoder and asserts on the ChainPolicy values the collector would label
// metrics with.
func TestDecodeCapturedChainDump(t *testing.T) {
	r := newNetlinkReaderWithDial(t, func(req netlink.Message) []netlink.Message {
		if headerKind(req) != unix.NFT_MSG_GETCHAIN {
			t.Errorf("unexpected request type %v", req.Header.Type)
		}
		return dumpReply(req, unmarshalAll(t, capturedChainDump))
	})

	got, err := r.GetChainPolicies()
	if err != nil {
		t.Fatalf("GetChainPolicies: %v", err)
	}

	want := []ChainPolicy{
		{Family: "inet", Table: "filter", Chain: "input", Policy: "accept"},
		{Family: "inet", Table: "filter", Chain: "forward", Policy: "drop"},
		{Family: "inet", Table: "filter", Chain: "output", Policy: "accept"},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d policies, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("policy %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestDecodeRuleDump is the end-to-end decode: a chain dump followed by a rule
// dump per chain, all in kernel wire format, coming out as the RuleInfo the
// collector turns into metrics.
func TestDecodeRuleDump(t *testing.T) {
	inputRules := []netlink.Message{
		// 0: accept, ignored but still counted for the position label.
		rawRuleMessage(t, nftables.TableFamilyIPv4, "filter", "input", 1, "",
			rawCounterExpr(t, 1, 2), rawVerdictExpr(t, expr.VerdictAccept)),
		// 1: a commented drop -- the comment becomes the `rule` label.
		rawRuleMessage(t, nftables.TableFamilyIPv4, "filter", "input", 2, "block-ssh",
			rawCounterExpr(t, 673497, 1838301216), rawVerdictExpr(t, expr.VerdictDrop)),
		// 2: an uncommented reject -- the position becomes the `rule` label.
		rawRuleMessage(t, nftables.TableFamilyIPv4, "filter", "input", 3, "",
			rawCounterExpr(t, 42, 2100), rawRejectExpr(t)),
	}

	r := newNetlinkReaderWithDial(t, func(req netlink.Message) []netlink.Message {
		switch headerKind(req) {
		case unix.NFT_MSG_GETCHAIN:
			return dumpReply(req, unmarshalAll(t, capturedChainDump))
		case unix.NFT_MSG_GETRULE:
			switch requestedChain(t, req) {
			case "input":
				return dumpReply(req, inputRules)
			case "forward":
				return dumpReply(req, []netlink.Message{capturedCounterOnlyRule})
			default:
				return dumpReply(req, nil)
			}
		}
		t.Fatalf("unexpected request type %v", req.Header.Type)
		return nil
	})

	got, err := r.GetDropRejectRules()
	if err != nil {
		t.Fatalf("GetDropRejectRules: %v", err)
	}

	// inet rather than ip: the family label comes from the table the CHAIN dump
	// said the chain belongs to, not from the rule dump's own nfgenmsg. That is
	// deliberate -- a rule's family is a property of its table, and taking it
	// from the chain is the only way a rule with no counters and no comment
	// still gets labeled consistently with its neighbors.
	want := []RuleInfo{
		{Family: "inet", Table: "filter", Chain: "input", Rule: "block-ssh", Verdict: "drop", Packets: 673497, Bytes: 1838301216},
		{Family: "inet", Table: "filter", Chain: "input", Rule: "2", Verdict: "reject", Packets: 42, Bytes: 2100},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d rules, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestDecodeRuleDumpSurfacesNetlinkErrors proves the graceful-degradation
// contract survives all the way down: an EPERM from the kernel on the rule
// dump has to come back as an error the collector can turn into
// network_firewall_collector_up 0, not as a partial result or a panic.
func TestDecodeRuleDumpSurfacesNetlinkErrors(t *testing.T) {
	r := newNetlinkReaderWithDial(t, func(req netlink.Message) []netlink.Message {
		if headerKind(req) == unix.NFT_MSG_GETCHAIN {
			return dumpReply(req, unmarshalAll(t, capturedChainDump))
		}
		return []netlink.Message{errorReply(req, unix.EPERM)}
	})

	if _, err := r.GetDropRejectRules(); err == nil {
		t.Fatal("expected the netlink error to surface")
	}
}

// ---------------------------------------------------------------------------
// Kernel-wire-format helpers
//
// These build nf_tables netlink messages from the UAPI constants directly.
// They deliberately do not call google/nftables' marshallers: this test is
// meant to check that the decoder reads what the KERNEL writes, and a
// round-trip through the library's own encoder would only check that the
// library is self-consistent.
// ---------------------------------------------------------------------------

// newNetlinkReaderWithDial builds the production reader with its socket
// replaced by respond, exercising dialNftables and the real *nftables.Conn.
func newNetlinkReaderWithDial(t *testing.T, respond func(req netlink.Message) []netlink.Message) *netlinkReader {
	t.Helper()
	orig := nftNewConnFn
	t.Cleanup(func() { nftNewConnFn = orig })
	nftNewConnFn = func(_ int) (*nftables.Conn, error) {
		return nftables.New(
			nftables.AsLasting(),
			nftables.WithTestDial(func(reqs []netlink.Message) ([]netlink.Message, error) {
				if len(reqs) != 1 {
					t.Fatalf("expected a single request message, got %d", len(reqs))
				}
				return respond(reqs[0]), nil
			}),
		)
	}
	return newNetlinkReader()
}

// headerKind returns the nf_tables message type from a netlink header,
// dropping the NFNL_SUBSYS_NFTABLES byte above it.
func headerKind(m netlink.Message) int {
	return int(m.Header.Type & 0xff)
}

// requestedChain pulls NFTA_RULE_CHAIN out of a GETRULE request.
func requestedChain(t *testing.T, m netlink.Message) string {
	t.Helper()
	ad, err := netlink.NewAttributeDecoder(m.Data[4:])
	if err != nil {
		t.Fatalf("decoding request: %v", err)
	}
	var name string
	for ad.Next() {
		if ad.Type() == unix.NFTA_RULE_CHAIN {
			name = ad.String()
		}
	}
	return name
}

// dumpReply wraps messages as a netlink dump: every entry flagged multi-part,
// terminated by an NLMSG_DONE carrying a zero error code, which is exactly how
// the kernel ends a dump.
func dumpReply(req netlink.Message, msgs []netlink.Message) []netlink.Message {
	out := make([]netlink.Message, 0, len(msgs)+1)
	for _, m := range msgs {
		m.Header.Flags |= netlink.Multi
		m.Header.Sequence = req.Header.Sequence
		m.Header.PID = req.Header.PID
		out = append(out, m)
	}
	return append(out, netlink.Message{
		Header: netlink.Header{
			Type:     netlink.Done,
			Flags:    netlink.Multi,
			Sequence: req.Header.Sequence,
			PID:      req.Header.PID,
		},
		Data: []byte{0, 0, 0, 0},
	})
}

// errorReply builds the NLMSG_ERROR the kernel sends when it refuses a
// request; nfnetlink_rcv answers every message with EPERM this way when the
// socket lacks CAP_NET_ADMIN.
func errorReply(req netlink.Message, errno syscall.Errno) netlink.Message {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(-int32(errno)))
	return netlink.Message{
		Header: netlink.Header{
			Type:     netlink.Error,
			Sequence: req.Header.Sequence,
			PID:      req.Header.PID,
		},
		Data: data,
	}
}

// unmarshalAll turns captured wire bytes back into netlink messages.
func unmarshalAll(t *testing.T, raw [][]byte) []netlink.Message {
	t.Helper()
	out := make([]netlink.Message, 0, len(raw))
	for _, b := range raw {
		var m netlink.Message
		if err := m.UnmarshalBinary(b); err != nil {
			t.Fatalf("unmarshalling captured message: %v", err)
		}
		out = append(out, m)
	}
	return out
}

// rawRuleMessage assembles an NFT_MSG_NEWRULE dump entry.
func rawRuleMessage(t *testing.T, family nftables.TableFamily, table, chain string, handle uint64, comment string, exprs ...[]byte) netlink.Message {
	t.Helper()

	attrs := []netlink.Attribute{
		{Type: unix.NFTA_RULE_TABLE, Data: nullTerm(table)},
		{Type: unix.NFTA_RULE_CHAIN, Data: nullTerm(chain)},
		{Type: unix.NFTA_RULE_HANDLE, Data: be64(handle)},
	}
	if len(exprs) > 0 {
		elems := make([]netlink.Attribute, 0, len(exprs))
		for _, e := range exprs {
			elems = append(elems, netlink.Attribute{Type: unix.NLA_F_NESTED | unix.NFTA_LIST_ELEM, Data: e})
		}
		attrs = append(attrs, netlink.Attribute{
			Type: unix.NLA_F_NESTED | unix.NFTA_RULE_EXPRESSIONS,
			Data: marshalAttrs(t, elems),
		})
	}
	if comment != "" {
		attrs = append(attrs, netlink.Attribute{Type: unix.NFTA_RULE_USERDATA, Data: commentUserData(comment)})
	}

	return netlink.Message{
		Header: netlink.Header{Type: netlink.HeaderType(unix.NFNL_SUBSYS_NFTABLES<<8 | unix.NFT_MSG_NEWRULE)},
		Data:   append(nfgenmsg(family), marshalAttrs(t, attrs)...),
	}
}

// rawCounterExpr builds the `counter` expression: NFTA_EXPR_NAME plus a nested
// NFTA_EXPR_DATA holding the two big-endian 64-bit totals.
func rawCounterExpr(t *testing.T, packets, bytes uint64) []byte {
	t.Helper()
	data := marshalAttrs(t, []netlink.Attribute{
		{Type: unix.NFTA_COUNTER_BYTES, Data: be64(bytes)},
		{Type: unix.NFTA_COUNTER_PACKETS, Data: be64(packets)},
	})
	return marshalAttrs(t, []netlink.Attribute{
		{Type: unix.NFTA_EXPR_NAME, Data: nullTerm("counter")},
		{Type: unix.NLA_F_NESTED | unix.NFTA_EXPR_DATA, Data: data},
	})
}

// rawVerdictExpr builds a verdict. There is no "verdict" expression on the
// wire: nftables encodes one as an `immediate` writing a verdict code into the
// verdict register, which is why this nests three deep and why the decoder has
// to notice an immediate with an empty data field and re-read it as a verdict.
func rawVerdictExpr(t *testing.T, kind expr.VerdictKind) []byte {
	t.Helper()
	code := marshalAttrs(t, []netlink.Attribute{
		{Type: unix.NFTA_VERDICT_CODE, Data: be32(uint32(kind))},
	})
	immData := marshalAttrs(t, []netlink.Attribute{
		{Type: unix.NLA_F_NESTED | unix.NFTA_DATA_VERDICT, Data: code},
	})
	data := marshalAttrs(t, []netlink.Attribute{
		{Type: unix.NFTA_IMMEDIATE_DREG, Data: be32(unix.NFT_REG_VERDICT)},
		{Type: unix.NLA_F_NESTED | unix.NFTA_IMMEDIATE_DATA, Data: immData},
	})
	return marshalAttrs(t, []netlink.Attribute{
		{Type: unix.NFTA_EXPR_NAME, Data: nullTerm("immediate")},
		{Type: unix.NLA_F_NESTED | unix.NFTA_EXPR_DATA, Data: data},
	})
}

// rawRejectExpr builds `reject with icmpx admin-prohibited`.
func rawRejectExpr(t *testing.T) []byte {
	t.Helper()
	data := marshalAttrs(t, []netlink.Attribute{
		{Type: unix.NFTA_REJECT_TYPE, Data: be32(unix.NFT_REJECT_ICMPX_UNREACH)},
		{Type: unix.NFTA_REJECT_ICMP_CODE, Data: []byte{unix.NFT_REJECT_ICMPX_ADMIN_PROHIBITED}},
	})
	return marshalAttrs(t, []netlink.Attribute{
		{Type: unix.NFTA_EXPR_NAME, Data: nullTerm("reject")},
		{Type: unix.NLA_F_NESTED | unix.NFTA_EXPR_DATA, Data: data},
	})
}

// nfgenmsg is the four-byte struct nfgenmsg every nf_tables message starts
// with: family, netlink version, then a big-endian resource id.
func nfgenmsg(family nftables.TableFamily) []byte {
	return []byte{byte(family), unix.NFNETLINK_V0, 0x00, 0x00}
}

// commentUserData encodes a rule comment the way nft(8) does: a libnftnl TLV
// of type NFTNL_UDATA_RULE_COMMENT (0) whose length counts the terminating
// NUL. This is hand-rolled rather than taken from the library so that the
// decoder is being checked against the format, not against its own writer.
func commentUserData(comment string) []byte {
	out := []byte{0x00, byte(len(comment) + 1)}
	out = append(out, comment...)
	return append(out, 0x00)
}

// marshalAttrs is netlink.MarshalAttributes with the error folded into the
// test, since none of these fixtures can legitimately fail to encode.
func marshalAttrs(t *testing.T, attrs []netlink.Attribute) []byte {
	t.Helper()
	b, err := netlink.MarshalAttributes(attrs)
	if err != nil {
		t.Fatalf("marshalling attributes: %v", err)
	}
	return b
}

func nullTerm(s string) []byte { return append([]byte(s), 0x00) }

func be32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func be64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}
