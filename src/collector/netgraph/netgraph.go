// Package netgraph implements a collector that discovers network topology
// edges by examining active TCP and UDP connections relative to local
// listening ports.
package netgraph

import (
	"fmt"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
	"github.com/phaseshiftdata/prometheus_exporters/src/procnet"
)

// Connection represents a single network connection.
type Connection struct {
	LocalIP    string
	LocalPort  uint16
	RemoteIP   string
	RemotePort uint16
	State      string
	Protocol   string
}

// ConnectionSource abstracts retrieval of active connections so the
// collector can be tested without real procfs access.
type ConnectionSource interface {
	ListConnections() ([]Connection, error)
}

// procfsSource is the production ConnectionSource backed by procnet.
type procfsSource struct {
	procPath string
}

// Compile-time interface check.
var _ ConnectionSource = (*procfsSource)(nil)

func (s *procfsSource) ListConnections() ([]Connection, error) {
	tcp, err := procnet.ParseTCP(s.procPath)
	if err != nil {
		return nil, fmt.Errorf("parsing TCP: %w", err)
	}
	udp, err := procnet.ParseUDP(s.procPath)
	if err != nil {
		return nil, fmt.Errorf("parsing UDP: %w", err)
	}

	conns := make([]Connection, 0, len(tcp)+len(udp))
	for _, e := range tcp {
		conns = append(conns, entryToConnection(e))
	}
	for _, e := range udp {
		conns = append(conns, entryToConnection(e))
	}
	return conns, nil
}

func entryToConnection(e procnet.Entry) Connection {
	return Connection{
		LocalIP:    e.LocalIP,
		LocalPort:  e.LocalPort,
		RemoteIP:   e.RemoteIP,
		RemotePort: e.RemotePort,
		State:      e.State,
		Protocol:   e.Protocol,
	}
}

// edgeKey uniquely identifies a network graph edge.
type edgeKey struct {
	remoteHost string
	localPort  string
	direction  string
}

// netgraphCollector implements collector.Collector for network graph edges.
type netgraphCollector struct {
	source ConnectionSource
	desc   *prometheus.Desc
}

// Compile-time interface check.
var _ collector.Collector = (*netgraphCollector)(nil)

// New returns a network graph collector backed by real procfs at procPath.
func New(procPath string) collector.Collector {
	return NewWithSource(&procfsSource{procPath: procPath})
}

// NewWithSource returns a network graph collector using the provided
// ConnectionSource, which is useful for injecting mocks in tests.
func NewWithSource(source ConnectionSource) collector.Collector {
	return &netgraphCollector{
		source: source,
		desc: prometheus.NewDesc(
			"network_graph_edge",
			"Presence indicator for a network topology edge; value is always 1.",
			[]string{"remote_host", "local_port", "direction"},
			nil,
		),
	}
}

// Name returns the short identifier for this collector.
func (c *netgraphCollector) Name() string { return "netgraph" }

// Describe sends the metric descriptor to the channel.
func (c *netgraphCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

// isLoopback returns true if the IP should be excluded from graph edges.
func isLoopback(ip string) bool {
	return ip == "0.0.0.0" || ip == "127.0.0.1"
}

// Collect queries connections and emits deduplicated network graph edges.
func (c *netgraphCollector) Collect(ch chan<- prometheus.Metric) {
	conns, err := c.source.ListConnections()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.desc, err)
		return
	}

	// First pass: collect all local ports that have a LISTEN entry (services).
	// For UDP, a "listening" socket has state CLOSE and remote 0.0.0.0.
	type portProto struct {
		port     uint16
		protocol string
	}
	listening := make(map[portProto]bool)
	for _, conn := range conns {
		if conn.Protocol == "tcp" && conn.State == "LISTEN" {
			listening[portProto{conn.LocalPort, conn.Protocol}] = true
		}
		if conn.Protocol == "udp" && conn.State == "CLOSE" && conn.RemoteIP == "0.0.0.0" {
			listening[portProto{conn.LocalPort, conn.Protocol}] = true
		}
	}

	// Second pass: build deduplicated edges.
	edges := make(map[edgeKey]bool)
	for _, conn := range conns {
		// Skip LISTEN entries themselves — they are not connections.
		if conn.Protocol == "tcp" && conn.State == "LISTEN" {
			continue
		}
		// Skip UDP listen entries.
		if conn.Protocol == "udp" && conn.State == "CLOSE" && conn.RemoteIP == "0.0.0.0" {
			continue
		}

		// Skip loopback addresses.
		if isLoopback(conn.RemoteIP) || isLoopback(conn.LocalIP) {
			continue
		}

		if listening[portProto{conn.LocalPort, conn.Protocol}] {
			// Inbound: remote connects to our listening port.
			edges[edgeKey{
				remoteHost: conn.RemoteIP,
				localPort:  strconv.FormatUint(uint64(conn.LocalPort), 10),
				direction:  "inbound",
			}] = true
		} else {
			// Outbound: we connect to remote service port.
			edges[edgeKey{
				remoteHost: conn.RemoteIP,
				localPort:  strconv.FormatUint(uint64(conn.RemotePort), 10),
				direction:  "outbound",
			}] = true
		}
	}

	for e := range edges {
		ch <- prometheus.MustNewConstMetric(
			c.desc,
			prometheus.GaugeValue,
			1,
			e.remoteHost,
			e.localPort,
			e.direction,
		)
	}
}
