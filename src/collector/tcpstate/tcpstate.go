// Package tcpstate implements a collector that reports per-TCP-connection
// state as Prometheus metrics, labeled by local address:port and peer
// address:port.
package tcpstate

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
	"github.com/phaseshiftdata/prometheus_exporters/src/procnet"
)

// ConnectionSource abstracts retrieval of TCP connections so the
// collector can be tested without real procfs access.
type ConnectionSource interface {
	ListConnections() ([]Connection, error)
}

// Connection represents a single TCP connection.
type Connection struct {
	LocalIP    string
	LocalPort  uint16
	RemoteIP   string
	RemotePort uint16
	State      string
}

// procfsSource is the production ConnectionSource backed by procnet.
type procfsSource struct {
	procPath string
}

// Compile-time interface check.
var _ ConnectionSource = (*procfsSource)(nil)

func (s *procfsSource) ListConnections() ([]Connection, error) {
	entries, err := procnet.ParseTCP(s.procPath)
	if err != nil {
		return nil, fmt.Errorf("parsing TCP: %w", err)
	}
	conns := make([]Connection, 0, len(entries))
	for _, e := range entries {
		conns = append(conns, Connection{
			LocalIP:    e.LocalIP,
			LocalPort:  e.LocalPort,
			RemoteIP:   e.RemoteIP,
			RemotePort: e.RemotePort,
			State:      e.State,
		})
	}
	return conns, nil
}

// DefaultMaxConnections is the default maximum number of TCP connections
// to export. This prevents metric cardinality explosion on busy hosts.
const DefaultMaxConnections = 10000

// tcpStateCollector implements collector.Collector for per-connection TCP state.
type tcpStateCollector struct {
	source   ConnectionSource
	maxConns int
	states   map[string]bool // allowed states filter (nil = all)
	desc     *prometheus.Desc
	descTrunc *prometheus.Desc
}

// Compile-time interface check.
var _ collector.Collector = (*tcpStateCollector)(nil)

// New returns a TCP state collector backed by real procfs at procPath
// with the default maximum connection limit.
func New(procPath string) collector.Collector {
	return NewWithOptions(&procfsSource{procPath: procPath}, DefaultMaxConnections, nil)
}

// NewWithMax returns a TCP state collector with a custom maximum connection limit
// and optional state filter.
func NewWithMax(procPath string, maxConns int, states []string) collector.Collector {
	return NewWithOptions(&procfsSource{procPath: procPath}, maxConns, states)
}

// NewWithSource returns a TCP state collector using the provided
// ConnectionSource, which is useful for injecting mocks in tests.
func NewWithSource(source ConnectionSource) collector.Collector {
	return NewWithOptions(source, DefaultMaxConnections, nil)
}

// NewWithOptions returns a TCP state collector with a custom source,
// max connections, and optional state filter.
func NewWithOptions(source ConnectionSource, maxConns int, states []string) collector.Collector {
	if maxConns <= 0 {
		maxConns = DefaultMaxConnections
	}
	var stateFilter map[string]bool
	if len(states) > 0 {
		stateFilter = make(map[string]bool, len(states))
		for _, s := range states {
			stateFilter[strings.ToUpper(strings.TrimSpace(s))] = true
		}
	}
	return &tcpStateCollector{
		source:   source,
		maxConns: maxConns,
		states:   stateFilter,
		desc: prometheus.NewDesc(
			"network_tcp_connection",
			"Per-TCP-connection state indicator; value is always 1.",
			[]string{"local_addr", "local_port", "peer_addr", "peer_port", "state"},
			nil,
		),
		descTrunc: prometheus.NewDesc(
			"network_tcp_connections_truncated",
			"Set to 1 when the TCP connection count exceeds the maximum limit and output is truncated.",
			nil, nil,
		),
	}
}

// Name returns the short identifier for this collector.
func (c *tcpStateCollector) Name() string { return "tcpstate" }

// Describe sends the metric descriptors to the channel.
func (c *tcpStateCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
	ch <- c.descTrunc
}

// isLoopback returns true if the IP should be excluded from TCP state metrics.
func isLoopback(ip string) bool {
	return ip == "0.0.0.0" || ip == "127.0.0.1" || ip == "::1" || ip == "::"
}

// Collect queries TCP connections and emits one gauge per connection.
func (c *tcpStateCollector) Collect(ch chan<- prometheus.Metric) {
	conns, err := c.source.ListConnections()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.desc, err)
		return
	}

	emitted := 0
	for _, conn := range conns {
		// Skip loopback connections (both sides).
		if isLoopback(conn.LocalIP) && isLoopback(conn.RemoteIP) {
			continue
		}

		// Filter by allowed states if configured.
		if c.states != nil && !c.states[conn.State] {
			continue
		}

		if emitted >= c.maxConns {
			ch <- prometheus.MustNewConstMetric(c.descTrunc, prometheus.GaugeValue, 1)
			return
		}

		ch <- prometheus.MustNewConstMetric(
			c.desc,
			prometheus.GaugeValue,
			1,
			conn.LocalIP,
			strconv.FormatUint(uint64(conn.LocalPort), 10),
			conn.RemoteIP,
			strconv.FormatUint(uint64(conn.RemotePort), 10),
			conn.State,
		)
		emitted++
	}
	ch <- prometheus.MustNewConstMetric(c.descTrunc, prometheus.GaugeValue, 0)
}
