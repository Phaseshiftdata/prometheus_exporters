package firewall

import (
	"errors"
	"fmt"
	"strconv"
	"syscall"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
)

// This file is the production NftablesReader. It reads the ruleset the same
// way nft(8) does -- an NETLINK_NETFILTER socket speaking the nf_tables
// subsystem -- rather than by running nft(8) itself.
//
// The reason is the runtime image. network-exporter ships distroless and
// non-root by mandate (requirement 6, and its row in docs/CIS-EXCEPTIONS.md,
// enforced by scripts/lint-compose-hardening.sh), so there is no nft binary in
// it and no shell to invoke one with: `docker exec network-exporter sh` fails
// with `"sh": executable file not found`. Shipping nft(8) into the image would
// break the mandate; bind-mounting the host's /usr/sbin/nft would not even
// run, because a distroless image has no dynamic loader and none of the shared
// libraries nft links against. The collector therefore reported
// network_firewall_collector_up 0 on monitor01 from the day it was deployed.
//
// Netlink is the way in that costs nothing: it is a kernel interface, not a
// userspace program, so it needs no binary in the image. It is also the same
// interface conntrack already uses successfully from this very container --
// src/collector/conntrack goes through NETLINK_NETFILTER too, for the
// CTNETLINK subsystem instead of the nf_tables one -- which is the practical
// evidence that the capability set in docker-compose.yml is already sufficient
// (nfnetlink_rcv gates the whole NETLINK_NETFILTER protocol on CAP_NET_ADMIN
// in the socket's network namespace, once, for every subsystem behind it).
//
// The library is github.com/google/nftables rather than the already-vendored
// github.com/vishvananda/netlink. That was not a preference: vishvananda has
// no nf_tables support at all. Its netfilter coverage stops at conntrack and
// ipset, and its filter.go/chain.go are tc traffic-control objects, not
// netfilter ones -- `grep -ril nftables` over the module returns nothing.

// nftConn is the subset of *nftables.Conn's API this collector uses. It exists
// so the two readers below can be exercised against a fake: CI has neither
// CAP_NET_ADMIN nor an nftables ruleset, so a test that needed a real socket
// would be a test that never ran.
type nftConn interface {
	ListChains() ([]*nftables.Chain, error)
	GetRules(t *nftables.Table, c *nftables.Chain) ([]*nftables.Rule, error)
	CloseLasting() error
}

// netlinkReader implements NftablesReader against the kernel.
type netlinkReader struct {
	// dial opens a connection. A field rather than a direct call so tests can
	// substitute a fake, and so probe and the two Get methods share one path.
	dial func() (nftConn, error)
}

// Compile-time interface checks.
var (
	_ NftablesReader = (*netlinkReader)(nil)
	_ nftConn        = (*nftables.Conn)(nil)
)

// dialNftables opens a lasting NETLINK_NETFILTER socket.
//
// Lasting, rather than the library's default of one transient socket per
// operation, because a scrape walks every chain in the ruleset and asks for
// each one's rules separately -- the kernel's nf_tables dump API has no
// "everything at once" call the way `nft list ruleset` appears to. On a host
// running firewalld that is dozens of round trips, and dozens of socket
// open/close pairs every 30 seconds is a cost with nothing to show for it.
// The caller closes it; see the deferred CloseLasting in each reader.
func dialNftables() (nftConn, error) {
	conn, err := nftNewConnFn()
	if err != nil {
		return nil, fmt.Errorf("opening NETLINK_NETFILTER socket: %w", err)
	}
	return conn, nil
}

// nftNewConnFn is the one call in this package that touches a real socket. It
// is a function variable for the same reason arp.go's neighListFn is: it is
// the seam tests use to feed captured kernel netlink payloads through the
// entire production decode path without needing a kernel.
var nftNewConnFn = func() (*nftables.Conn, error) {
	return nftables.New(nftables.AsLasting())
}

// newNetlinkReader returns the production reader.
func newNetlinkReader() *netlinkReader {
	return &netlinkReader{dial: dialNftables}
}

// probe reports why this process can never read nftables, or "" if it looks
// like it can.
//
// Only two classes of failure latch, and both are properties of how the
// exporter was deployed rather than of what is in the ruleset:
//
//   - EPERM/EACCES -- nfnetlink_rcv rejects every message on the socket
//     without CAP_NET_ADMIN in the network namespace the socket belongs to. If
//     the container's cap_add ever loses NET_ADMIN, this is what it looks like,
//     and it will look like it until the container is recreated.
//   - EPROTONOSUPPORT/EAFNOSUPPORT -- the kernel has no NETLINK_NETFILTER
//     socket to open, i.e. nf_tables is not built or not loadable. Also not
//     something a running process recovers from.
//
// Everything else is left to Collect, which reports the collector down for
// that scrape and recovers by itself on the next one. An empty ruleset in
// particular is a success, not a failure: a host with no firewall rules is a
// host with no firewall metrics, and that is honest.
func (r *netlinkReader) probe() string {
	conn, err := r.dial()
	if err == nil {
		defer func() { _ = conn.CloseLasting() }()
		_, err = conn.ListChains()
	}
	if err == nil {
		return ""
	}

	switch {
	case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EACCES):
		return fmt.Sprintf("no CAP_NET_ADMIN for NETLINK_NETFILTER in this network namespace: %v", err)
	case errors.Is(err, syscall.EPROTONOSUPPORT), errors.Is(err, syscall.EAFNOSUPPORT):
		return fmt.Sprintf("kernel has no NETLINK_NETFILTER/nf_tables support: %v", err)
	}
	return ""
}

// GetDropRejectRules returns every rule in the ruleset carrying a drop or
// reject verdict, with the counters attached to it.
//
// A rule with no counter statement reports zero. That is not a bug and it is
// not worth working around: nftables only accounts a rule that was written
// with `counter`, so a zero here means the rule was written without one, and
// inventing a value would be worse than reporting the truth.
func (r *netlinkReader) GetDropRejectRules() ([]RuleInfo, error) {
	conn, err := r.dial()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.CloseLasting() }()

	chains, err := conn.ListChains()
	if err != nil {
		return nil, fmt.Errorf("listing nftables chains: %w", err)
	}

	var rules []RuleInfo
	for _, chain := range chains {
		if chain == nil || chain.Table == nil {
			continue
		}
		chainRules, err := conn.GetRules(chain.Table, chain)
		if err != nil {
			return nil, fmt.Errorf("listing rules in %s %s %s: %w",
				familyName(chain.Table.Family), chain.Table.Name, chain.Name, err)
		}
		rules = append(rules, dropRejectRules(chain, chainRules)...)
	}
	return rules, nil
}

// GetChainPolicies returns the default policy of every base chain.
//
// Base chains only: a regular (jump target) chain has no policy, the kernel
// sends no NFTA_CHAIN_POLICY for it, and reporting one would be a fabrication.
// The Packets and Bytes fields stay zero, exactly as they did when this came
// from `nft --json list ruleset` -- see chainPolicies for why.
func (r *netlinkReader) GetChainPolicies() ([]ChainPolicy, error) {
	conn, err := r.dial()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.CloseLasting() }()

	chains, err := conn.ListChains()
	if err != nil {
		return nil, fmt.Errorf("listing nftables chains: %w", err)
	}
	return chainPolicies(chains), nil
}

// dropRejectRules converts one chain's decoded rules into RuleInfo, keeping
// only those that drop or reject.
//
// The index counts every rule in the chain, including the ones filtered out
// here, because the `rule` label for an uncommented rule is its position in
// the chain -- that is what the label meant when it came from nft(8) and
// dashboards were built against it. Counting only drop rules would silently
// renumber every series the first time an accept rule was inserted above one.
func dropRejectRules(chain *nftables.Chain, rules []*nftables.Rule) []RuleInfo {
	out := make([]RuleInfo, 0, len(rules))
	for idx, rule := range rules {
		if rule == nil {
			continue
		}
		verdict, packets, bytes := classifyRule(rule)
		if verdict == "" {
			continue
		}
		out = append(out, RuleInfo{
			Family:  familyName(chain.Table.Family),
			Table:   chain.Table.Name,
			Chain:   chain.Name,
			Rule:    ruleLabel(rule, idx),
			Verdict: verdict,
			Packets: packets,
			Bytes:   bytes,
		})
	}
	return out
}

// classifyRule walks a rule's expression list for the verdict and the counter.
//
// A drop verdict arrives as an `immediate` expression writing into the verdict
// register, which google/nftables re-parses into expr.Verdict; a reject
// arrives as its own expr.Reject. Both are terminal, so a rule has at most one
// and the last-one-wins ordering here never actually arbitrates anything --
// it only mirrors what the JSON parser this replaced did, so that a ruleset
// which somehow carried both would still be labeled the same way.
func classifyRule(rule *nftables.Rule) (verdict string, packets, bytes uint64) {
	for _, e := range rule.Exprs {
		switch ex := e.(type) {
		case *expr.Counter:
			packets, bytes = ex.Packets, ex.Bytes
		case *expr.Verdict:
			if ex.Kind == expr.VerdictDrop {
				verdict = "drop"
			}
		case *expr.Reject:
			verdict = "reject"
		}
	}
	return verdict, packets, bytes
}

// ruleLabel returns the rule's comment, or its position in the chain when it
// has none.
//
// nft(8) stores a rule comment in NFTA_RULE_USERDATA as a libnftnl TLV rather
// than a netlink attribute of its own, which is why this goes through the
// userdata decoder instead of reading a field off the rule.
func ruleLabel(rule *nftables.Rule, idx int) string {
	if comment, ok := userdata.GetString(rule.UserData, userdata.TypeComment); ok && comment != "" {
		return comment
	}
	return strconv.Itoa(idx)
}

// chainPolicies converts decoded chains into ChainPolicy, skipping the regular
// chains that have no policy at all.
//
// Packets and Bytes are left at zero. The kernel does keep per-base-chain
// counters, but it only sends NFTA_CHAIN_COUNTERS for a chain that was created
// with them enabled, and google/nftables does not decode that attribute today.
// `nft --json list ruleset` did not report them either, so this is not a
// regression -- network_firewall_policy_drop_{packets,bytes}_total has always
// been a presence signal for a drop-policy chain rather than a traffic
// measurement. Populating it properly needs an upstream change; until then,
// reporting zero is what the previous implementation reported.
func chainPolicies(chains []*nftables.Chain) []ChainPolicy {
	var out []ChainPolicy
	for _, chain := range chains {
		if chain == nil || chain.Table == nil || chain.Policy == nil {
			continue
		}
		out = append(out, ChainPolicy{
			Family: familyName(chain.Table.Family),
			Table:  chain.Table.Name,
			Chain:  chain.Name,
			Policy: policyName(*chain.Policy),
		})
	}
	return out
}

// familyName renders an nftables table family as the label value the metrics
// have always used -- the same spelling nft(8) prints, not the kernel's
// NFPROTO_ constant name.
func familyName(f nftables.TableFamily) string {
	switch f {
	case nftables.TableFamilyIPv4:
		return "ip"
	case nftables.TableFamilyIPv6:
		return "ip6"
	case nftables.TableFamilyINet:
		return "inet"
	case nftables.TableFamilyBridge:
		return "bridge"
	case nftables.TableFamilyARP:
		return "arp"
	case nftables.TableFamilyNetdev:
		return "netdev"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(f))
	}
}

// policyName renders a chain policy as the lowercase word nft(8) prints.
func policyName(p nftables.ChainPolicy) string {
	switch p {
	case nftables.ChainPolicyDrop:
		return "drop"
	case nftables.ChainPolicyAccept:
		return "accept"
	default:
		return fmt.Sprintf("unknown(%d)", uint32(p))
	}
}
