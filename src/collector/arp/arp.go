// Package arp implements a collector that reports the full IPv4 ARP
// neighbor table as Prometheus metrics.
package arp

import (
	"fmt"
	"net"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vishvananda/netlink"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
)

// Neighbor represents a single ARP table entry.
type Neighbor struct {
	IP     net.IP
	MAC    net.HardwareAddr
	Device string
	State  int
}

// NeighborLister abstracts retrieval of the system ARP table so the
// collector can be tested without real netlink calls.
type NeighborLister interface {
	ListNeighbors() ([]Neighbor, error)
}

// netlinkLister is the production NeighborLister backed by netlink.
type netlinkLister struct{}

// Compile-time interface check.
var _ NeighborLister = (*netlinkLister)(nil)

// neighListFn and linkByIndexFn are function variables for the netlink calls,
// allowing tests to inject stubs.
var (
	neighListFn    = netlink.NeighList
	linkByIndexFn  = netlink.LinkByIndex
)

func (l *netlinkLister) ListNeighbors() ([]Neighbor, error) {
	raw, err := neighListFn(0, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("netlink NeighList: %w", err)
	}
	out := make([]Neighbor, 0, len(raw))
	for _, n := range raw {
		link, err := linkByIndexFn(n.LinkIndex)
		if err != nil {
			continue
		}
		out = append(out, Neighbor{
			IP:     n.IP,
			MAC:    n.HardwareAddr,
			Device: link.Attrs().Name,
			State:  n.State,
		})
	}
	return out, nil
}

// nudStateString maps a kernel NUD state constant to a human-readable string.
func nudStateString(state int) string {
	switch state {
	case 0x01: // NUD_INCOMPLETE
		return "incomplete"
	case 0x02: // NUD_REACHABLE
		return "reachable"
	case 0x04: // NUD_STALE
		return "stale"
	case 0x08: // NUD_DELAY
		return "delay"
	case 0x10: // NUD_PROBE
		return "probe"
	case 0x20: // NUD_FAILED
		return "failed"
	case 0x40: // NUD_NOARP
		return "noarp"
	case 0x80: // NUD_PERMANENT
		return "permanent"
	default:
		return fmt.Sprintf("unknown(%d)", state)
	}
}

// arpCollector implements collector.Collector for the ARP table.
type arpCollector struct {
	lister NeighborLister
	desc   *prometheus.Desc
}

// Compile-time interface check.
var _ collector.Collector = (*arpCollector)(nil)

// New returns an ARP collector backed by the real netlink implementation.
func New() collector.Collector {
	return NewWithLister(&netlinkLister{})
}

// NewWithLister returns an ARP collector using the provided NeighborLister,
// which is useful for injecting mocks in tests.
func NewWithLister(lister NeighborLister) collector.Collector {
	return &arpCollector{
		lister: lister,
		desc: prometheus.NewDesc(
			"network_arp_entry",
			"ARP table entry; value is always 1.",
			[]string{"ip", "mac", "device", "state"},
			nil,
		),
	}
}

// Name returns the short identifier for this collector.
func (c *arpCollector) Name() string { return "arp" }

// Describe sends the metric descriptor to the channel.
func (c *arpCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

// Collect queries the ARP table and sends one gauge per IPv4 entry.
func (c *arpCollector) Collect(ch chan<- prometheus.Metric) {
	neighbors, err := c.lister.ListNeighbors()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.desc, err)
		return
	}
	for _, n := range neighbors {
		// Skip entries with nil IP or IPv6 addresses.
		if n.IP == nil || n.IP.To4() == nil {
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			c.desc,
			prometheus.GaugeValue,
			1,
			n.IP.String(),
			n.MAC.String(),
			n.Device,
			nudStateString(n.State),
		)
	}
}
