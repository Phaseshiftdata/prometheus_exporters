// Package firewall implements a collector that reports nftables DROP and
// REJECT packet/byte counters as Prometheus metrics.
//
// The counters come straight off the kernel's nf_tables netlink subsystem.
// They used to come from parsing `nft --json list ruleset`, which meant the
// collector could only work where nft(8) was on PATH -- and the runtime image
// is distroless by mandate (requirement 6, and its row in
// docs/CIS-EXCEPTIONS.md), so it never was. See nftreader.go for why netlink
// is the only way in that does not require relaxing the image.
package firewall

import (
	"log/slog"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
)

// RuleInfo holds counter data for a single nftables rule with a DROP or
// REJECT verdict.
type RuleInfo struct {
	Family  string // "ip", "ip6", "inet"
	Table   string
	Chain   string
	Rule    string // rule comment if available, otherwise position index as string
	Verdict string // "drop" or "reject"
	Packets uint64
	Bytes   uint64
}

// ChainPolicy holds counter data for a chain's default policy.
type ChainPolicy struct {
	Family  string
	Table   string
	Chain   string
	Policy  string // "drop" or "accept"
	Packets uint64
	Bytes   uint64
}

// NftablesReader abstracts retrieval of nftables rule and policy data so
// the collector can be tested without real nftables access.
type NftablesReader interface {
	GetDropRejectRules() ([]RuleInfo, error)
	GetChainPolicies() ([]ChainPolicy, error)
}

// firewallCollector implements collector.Collector for nftables DROP/REJECT
// counters.
type firewallCollector struct {
	reader NftablesReader

	// unavailable is the reason this host can never serve firewall metrics,
	// empty when a reader is wired. It exists so the collector can answer
	// "down" instead of erroring, which is what used to poison the whole
	// scrape -- see the note on New.
	unavailable string
	logFailure  sync.Once

	up            *prometheus.Desc
	dropPackets   *prometheus.Desc
	dropBytes     *prometheus.Desc
	rejectPackets *prometheus.Desc
	rejectBytes   *prometheus.Desc
	policyPackets *prometheus.Desc
	policyBytes   *prometheus.Desc
}

// Compile-time interface check.
var _ collector.Collector = (*firewallCollector)(nil)

// probeReaderFn builds the production reader and classifies whether this
// process can ever read nftables. It is a function variable so tests can
// stand in for the kernel without a netlink socket.
//
// The netnsPath parameter, when non-empty, causes the reader to open a
// netlink socket in the specified network namespace (e.g. "/proc/1/ns/net")
// instead of the current one. This is the mechanism that lets a container
// with userns-remap read the host's nftables ruleset.
var probeReaderFn = func(netnsPath string) (NftablesReader, string) {
	if netnsPath != "" {
		r, err := newNetlinkReaderForNetNS(netnsPath)
		if err != nil {
			return nil, err.Error()
		}
		return r, r.probe()
	}
	r := newNetlinkReader()
	return r, r.probe()
}

// New returns a firewall collector backed by the kernel's nf_tables netlink
// subsystem, or a permanently-down collector when this process can never talk
// to it.
//
// The probe happens here, once, and only latches for conditions that are a
// property of the deployment rather than of the ruleset: no CAP_NET_ADMIN for
// NETLINK_NETFILTER, or a kernel with no nf_tables at all. Neither changes for
// the lifetime of the process, and retrying them every 30s only produced a
// scrape error every 30s -- which, until promhttp was told to continue on
// error, discarded every other collector's metrics too. That is the incident
// this branch of the code exists for; see cmd/network_exporter/main.go.
//
// Anything else -- a transient netlink error, an empty ruleset, a table that
// vanishes between the chain dump and the rule dump -- deliberately does NOT
// latch. Those are conditions the host can recover from on its own, and a
// collector that gave up on the first of them would stay dark long after the
// cause was gone.
func New() collector.Collector {
	return NewWithNetNS("")
}

// NewWithNetNS returns a firewall collector that reads nftables from the
// specified network namespace. When netnsPath is empty, the collector reads
// from the current namespace (identical to New). When set to a path like
// "/proc/1/ns/net", the collector opens that namespace file and passes its
// fd to nftables.WithNetNSFd on every dial, allowing a container with
// userns-remap to read the host's nftables ruleset.
func NewWithNetNS(netnsPath string) collector.Collector {
	reader, unavailable := probeReaderFn(netnsPath)
	if unavailable != "" {
		slog.Warn("firewall metrics disabled: nftables cannot be read over netlink",
			"collector", "firewall", "reason", unavailable)
		return newCollector(nil, unavailable)
	}
	return NewWithReader(reader)
}

// NewWithReader returns a firewall collector using the provided
// NftablesReader, which is useful for injecting mocks in tests.
func NewWithReader(reader NftablesReader) collector.Collector {
	return newCollector(reader, "")
}

// newCollector builds the collector with its descriptors. A non-empty
// unavailable reason means no reader is wired and every scrape reports down.
func newCollector(reader NftablesReader, unavailable string) collector.Collector {
	ruleLabels := []string{"family", "table", "chain", "rule"}
	chainLabels := []string{"family", "table", "chain"}

	return &firewallCollector{
		reader:      reader,
		unavailable: unavailable,
		up: prometheus.NewDesc(
			"network_firewall_collector_up",
			"Whether nftables counters could be read (1 = collecting, 0 = unavailable).",
			nil, nil,
		),
		dropPackets: prometheus.NewDesc(
			"network_firewall_drop_packets_total",
			"Total packets dropped by nftables DROP rules.",
			ruleLabels, nil,
		),
		dropBytes: prometheus.NewDesc(
			"network_firewall_drop_bytes_total",
			"Total bytes dropped by nftables DROP rules.",
			ruleLabels, nil,
		),
		rejectPackets: prometheus.NewDesc(
			"network_firewall_reject_packets_total",
			"Total packets rejected by nftables REJECT rules.",
			ruleLabels, nil,
		),
		rejectBytes: prometheus.NewDesc(
			"network_firewall_reject_bytes_total",
			"Total bytes rejected by nftables REJECT rules.",
			ruleLabels, nil,
		),
		policyPackets: prometheus.NewDesc(
			"network_firewall_policy_drop_packets_total",
			"Total packets dropped by chain default DROP policy.",
			chainLabels, nil,
		),
		policyBytes: prometheus.NewDesc(
			"network_firewall_policy_drop_bytes_total",
			"Total bytes dropped by chain default DROP policy.",
			chainLabels, nil,
		),
	}
}

// Name returns the short identifier for this collector.
func (c *firewallCollector) Name() string { return "firewall" }

// Describe sends all metric descriptors to the channel.
func (c *firewallCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.dropPackets
	ch <- c.dropBytes
	ch <- c.rejectPackets
	ch <- c.rejectBytes
	ch <- c.policyPackets
	ch <- c.policyBytes
}

// reportDown emits the collector-up gauge at 0 and logs the reason at most
// once. Once, not per scrape: a firewall source that fails at all fails every
// scrape interval forever, and the gauge is what a dashboard reads anyway.
func (c *firewallCollector) reportDown(ch chan<- prometheus.Metric, reason string) {
	c.logFailure.Do(func() {
		slog.Warn("firewall collection failed; reporting collector down",
			"collector", "firewall", "reason", reason)
	})
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
}

// Collect queries nftables and sends metrics for DROP/REJECT rules and
// chain default DROP policies.
//
// A failure here reports network_firewall_collector_up 0 rather than an
// invalid metric. An invalid metric fails the whole gather, which is how an
// unreadable nftables came to hide four healthy collectors behind an HTTP 500.
func (c *firewallCollector) Collect(ch chan<- prometheus.Metric) {
	if c.unavailable != "" {
		// Already logged at startup, so no logging here -- just the gauge, so
		// the gap is visible on a dashboard instead of looking like silence.
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}

	rules, err := c.reader.GetDropRejectRules()
	if err != nil {
		c.reportDown(ch, err.Error())
		return
	}

	for _, r := range rules {
		switch r.Verdict {
		case "drop":
			ch <- prometheus.MustNewConstMetric(
				c.dropPackets, prometheus.CounterValue, float64(r.Packets),
				r.Family, r.Table, r.Chain, r.Rule,
			)
			ch <- prometheus.MustNewConstMetric(
				c.dropBytes, prometheus.CounterValue, float64(r.Bytes),
				r.Family, r.Table, r.Chain, r.Rule,
			)
		case "reject":
			ch <- prometheus.MustNewConstMetric(
				c.rejectPackets, prometheus.CounterValue, float64(r.Packets),
				r.Family, r.Table, r.Chain, r.Rule,
			)
			ch <- prometheus.MustNewConstMetric(
				c.rejectBytes, prometheus.CounterValue, float64(r.Bytes),
				r.Family, r.Table, r.Chain, r.Rule,
			)
		}
	}

	policies, err := c.reader.GetChainPolicies()
	if err != nil {
		c.reportDown(ch, err.Error())
		return
	}

	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)

	for _, p := range policies {
		if p.Policy != "drop" {
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			c.policyPackets, prometheus.CounterValue, float64(p.Packets),
			p.Family, p.Table, p.Chain,
		)
		ch <- prometheus.MustNewConstMetric(
			c.policyBytes, prometheus.CounterValue, float64(p.Bytes),
			p.Family, p.Table, p.Chain,
		)
	}
}
