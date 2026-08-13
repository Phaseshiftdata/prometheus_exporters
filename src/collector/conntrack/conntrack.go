// Package conntrack implements a collector that reports per-port connection
// counts by state, per-port byte counters from conntrack, and listening
// port presence.
package conntrack

import (
	"fmt"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vishvananda/netlink"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
	"github.com/phaseshiftdata/prometheus_exporters/src/procnet"
)

// SocketEntry represents a single network socket.
type SocketEntry struct {
	LocalIP    string
	LocalPort  uint16
	RemoteIP   string
	RemotePort uint16
	State      string
	Protocol   string
}

// SocketSource abstracts retrieval of active sockets.
type SocketSource interface {
	ListSockets() ([]SocketEntry, error)
}

// ConntrackFlow represents a single conntrack flow entry.
type ConntrackFlow struct {
	Protocol string
	SrcPort  uint16
	DstPort  uint16
	BytesIn  uint64
	BytesOut uint64
}

// ConntrackSource abstracts retrieval of conntrack flow entries.
type ConntrackSource interface {
	ListFlows() ([]ConntrackFlow, error)
}

// procfsSocketSource is the production SocketSource backed by procnet.
type procfsSocketSource struct {
	procPath string
}

// Compile-time interface check.
var _ SocketSource = (*procfsSocketSource)(nil)

func (s *procfsSocketSource) ListSockets() ([]SocketEntry, error) {
	tcp, err := procnet.ParseTCP(s.procPath)
	if err != nil {
		return nil, fmt.Errorf("parsing TCP: %w", err)
	}
	udp, err := procnet.ParseUDP(s.procPath)
	if err != nil {
		return nil, fmt.Errorf("parsing UDP: %w", err)
	}

	entries := make([]SocketEntry, 0, len(tcp)+len(udp))
	for _, e := range tcp {
		entries = append(entries, entryToSocket(e))
	}
	for _, e := range udp {
		entries = append(entries, entryToSocket(e))
	}
	return entries, nil
}

func entryToSocket(e procnet.Entry) SocketEntry {
	return SocketEntry{
		LocalIP:    e.LocalIP,
		LocalPort:  e.LocalPort,
		RemoteIP:   e.RemoteIP,
		RemotePort: e.RemotePort,
		State:      e.State,
		Protocol:   e.Protocol,
	}
}

// netlinkConntrackSource is the production ConntrackSource backed by netlink.
type netlinkConntrackSource struct{}

// Compile-time interface check.
var _ ConntrackSource = (*netlinkConntrackSource)(nil)

// conntrackTableListFn is a function variable for the netlink call,
// allowing tests to inject stubs.
var conntrackTableListFn = netlink.ConntrackTableList

// conntrackFlowEntry is a minimal representation of a netlink conntrack flow
// used to decouple parsing from the netlink library type.
type conntrackFlowEntry struct {
	Protocol uint8
	SrcPort  uint16
	DstPort  uint16
	FwdBytes uint64
	RevBytes uint64
}

func (s *netlinkConntrackSource) ListFlows() ([]ConntrackFlow, error) {
	flows, err := conntrackTableListFn(netlink.ConntrackTable, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("conntrack table list: %w", err)
	}
	entries := make([]conntrackFlowEntry, 0, len(flows))
	for _, f := range flows {
		entries = append(entries, conntrackFlowEntry{
			Protocol: f.Forward.Protocol,
			SrcPort:  f.Forward.SrcPort,
			DstPort:  f.Forward.DstPort,
			FwdBytes: f.Forward.Bytes,
			RevBytes: f.Reverse.Bytes,
		})
	}
	return convertFlowEntries(entries), nil
}

// convertFlowEntries converts raw conntrack entries to ConntrackFlow,
// filtering to TCP and UDP protocols only.
func convertFlowEntries(entries []conntrackFlowEntry) []ConntrackFlow {
	result := make([]ConntrackFlow, 0, len(entries))
	for _, e := range entries {
		if e.Protocol == 0 {
			continue
		}
		var proto string
		switch e.Protocol {
		case 6:
			proto = "tcp"
		case 17:
			proto = "udp"
		default:
			continue
		}
		result = append(result, ConntrackFlow{
			Protocol: proto,
			SrcPort:  e.SrcPort,
			DstPort:  e.DstPort,
			BytesIn:  e.RevBytes,
			BytesOut: e.FwdBytes,
		})
	}
	return result
}

// portProtoKey identifies a (port, protocol) pair.
type portProtoKey struct {
	port     uint16
	protocol string
}

// conntrackCollector implements collector.Collector for per-port connection
// visibility metrics.
type conntrackCollector struct {
	sockets SocketSource
	flows   ConntrackSource

	descConnections        *prometheus.Desc
	descBytesIn            *prometheus.Desc
	descBytesOut           *prometheus.Desc
	descListen             *prometheus.Desc
	descAccountingEnabled  *prometheus.Desc
}

// Compile-time interface check.
var _ collector.Collector = (*conntrackCollector)(nil)

// New returns a conntrack collector backed by real procfs and netlink.
func New(procPath string) collector.Collector {
	return NewWithSources(
		&procfsSocketSource{procPath: procPath},
		&netlinkConntrackSource{},
	)
}

// NewWithSources returns a conntrack collector using the provided sources,
// which is useful for injecting mocks in tests.
func NewWithSources(sockets SocketSource, flows ConntrackSource) collector.Collector {
	return &conntrackCollector{
		sockets: sockets,
		flows:   flows,
		descConnections: prometheus.NewDesc(
			"network_port_connections",
			"Number of connections per port, protocol, and state.",
			[]string{"port", "protocol", "state"},
			nil,
		),
		descBytesIn: prometheus.NewDesc(
			"network_port_bytes_in",
			"Total inbound bytes per port from conntrack.",
			[]string{"port", "protocol"},
			nil,
		),
		descBytesOut: prometheus.NewDesc(
			"network_port_bytes_out",
			"Total outbound bytes per port from conntrack.",
			[]string{"port", "protocol"},
			nil,
		),
		descListen: prometheus.NewDesc(
			"network_port_listen",
			"Presence of a listening port; value is always 1.",
			[]string{"port", "protocol", "bind_address"},
			nil,
		),
		descAccountingEnabled: prometheus.NewDesc(
			"network_conntrack_accounting_enabled",
			"Whether conntrack accounting is available (1) or not (0).",
			nil,
			nil,
		),
	}
}

// Name returns the short identifier for this collector.
func (c *conntrackCollector) Name() string { return "conntrack" }

// Describe sends all metric descriptors to the channel.
func (c *conntrackCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.descConnections
	ch <- c.descBytesIn
	ch <- c.descBytesOut
	ch <- c.descListen
	ch <- c.descAccountingEnabled
}

// isListening returns true if the socket represents a listening port.
func isListening(s SocketEntry) bool {
	if s.Protocol == "tcp" && s.State == "LISTEN" {
		return true
	}
	if s.Protocol == "udp" && s.State == "CLOSE" && s.RemoteIP == "0.0.0.0" {
		return true
	}
	return false
}

// Collect queries sockets and conntrack flows and emits per-port metrics.
func (c *conntrackCollector) Collect(ch chan<- prometheus.Metric) {
	sockets, err := c.sockets.ListSockets()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.descConnections, err)
		return
	}

	// Find listening ports and emit listen metrics.
	// Use a map to deduplicate listen entries by (port, protocol, bind_address).
	type listenKey struct {
		port        uint16
		protocol    string
		bindAddress string
	}
	listenSeen := make(map[listenKey]bool)
	listeningPorts := make(map[portProtoKey]bool)

	for _, s := range sockets {
		if !isListening(s) {
			continue
		}
		listeningPorts[portProtoKey{s.LocalPort, s.Protocol}] = true

		lk := listenKey{s.LocalPort, s.Protocol, s.LocalIP}
		if !listenSeen[lk] {
			listenSeen[lk] = true
			ch <- prometheus.MustNewConstMetric(
				c.descListen,
				prometheus.GaugeValue,
				1,
				strconv.FormatUint(uint64(s.LocalPort), 10),
				s.Protocol,
				s.LocalIP,
			)
		}
	}

	// Count connections per (port, protocol, state) for non-LISTEN entries
	// where the local port matches a listening port.
	type connKey struct {
		port     uint16
		protocol string
		state    string
	}
	counts := make(map[connKey]float64)

	for _, s := range sockets {
		if isListening(s) {
			continue
		}
		key := portProtoKey{s.LocalPort, s.Protocol}
		if !listeningPorts[key] {
			continue
		}
		counts[connKey{s.LocalPort, s.Protocol, s.State}]++
	}

	for k, v := range counts {
		ch <- prometheus.MustNewConstMetric(
			c.descConnections,
			prometheus.GaugeValue,
			v,
			strconv.FormatUint(uint64(k.port), 10),
			k.protocol,
			k.state,
		)
	}

	// Conntrack byte counts.
	if c.flows == nil {
		ch <- prometheus.MustNewConstMetric(
			c.descAccountingEnabled,
			prometheus.GaugeValue,
			0,
		)
		return
	}

	flows, err := c.flows.ListFlows()
	if err != nil {
		ch <- prometheus.MustNewConstMetric(
			c.descAccountingEnabled,
			prometheus.GaugeValue,
			0,
		)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		c.descAccountingEnabled,
		prometheus.GaugeValue,
		1,
	)

	// Aggregate byte counts per destination port that matches a listening port.
	type byteKey struct {
		port     uint16
		protocol string
	}
	bytesIn := make(map[byteKey]uint64)
	bytesOut := make(map[byteKey]uint64)

	for _, f := range flows {
		key := portProtoKey{f.DstPort, f.Protocol}
		if !listeningPorts[key] {
			continue
		}
		bk := byteKey{f.DstPort, f.Protocol}
		bytesIn[bk] += f.BytesIn
		bytesOut[bk] += f.BytesOut
	}

	for bk, v := range bytesIn {
		portStr := strconv.FormatUint(uint64(bk.port), 10)
		ch <- prometheus.MustNewConstMetric(
			c.descBytesIn,
			prometheus.GaugeValue,
			float64(v),
			portStr,
			bk.protocol,
		)
	}
	for bk, v := range bytesOut {
		portStr := strconv.FormatUint(uint64(bk.port), 10)
		ch <- prometheus.MustNewConstMetric(
			c.descBytesOut,
			prometheus.GaugeValue,
			float64(v),
			portStr,
			bk.protocol,
		)
	}
}
