// Package ipsec implements a collector that reports IPsec SA metrics
// obtained from the strongSwan VICI interface.
package ipsec

import (
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/strongswan/govici/vici"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
)

// IKE SA state name → numeric value mapping.
var ikeStateMap = map[string]int{
	"CREATED":     0,
	"CONNECTING":  1,
	"ESTABLISHED": 2,
	"PASSIVE":     3,
	"REKEYING":    4,
	"REKEYED":     5,
	"DELETING":    6,
	"DESTROYING":  7,
}

// Child SA state name → numeric value mapping.
var childStateMap = map[string]int{
	"CREATED":    0,
	"ROUTED":     1,
	"INSTALLING": 2,
	"INSTALLED":  3,
	"UPDATING":   4,
	"REKEYING":   5,
	"REKEYED":    6,
	"RETRYING":   7,
	"DELETING":   8,
	"DELETED":    9,
	"DESTROYING": 10,
}

// IKESAInfo holds information about a single IKE SA.
type IKESAInfo struct {
	Name            string
	UID             string
	RemoteHost      string
	State           int // 0-7
	EstablishedSecs float64
	ChildSAs        []ChildSAInfo
}

// ChildSAInfo holds information about a single child SA.
type ChildSAInfo struct {
	Name          string
	UID           string
	State         int // 0-10
	LocalTS       string
	RemoteTS      string
	BytesIn       uint64
	BytesOut      uint64
	PacketsIn     uint64
	PacketsOut    uint64
	InstalledSecs float64
}

// CharonStats holds charon daemon statistics.
type CharonStats struct {
	Uptime        float64
	Workers       int
	IdleWorkers   int
	ActiveWorkers int
	Queues        map[string]int // priority → count
	HalfOpenIKE   int
}

// VICIClient abstracts the VICI socket interface so the collector can be
// tested without a real strongSwan daemon.
type VICIClient interface {
	ListSAs() ([]IKESAInfo, error)
	GetStats() (CharonStats, error)
	IsAvailable() bool
}

// ipsecCollector implements collector.Collector for IPsec SA metrics.
type ipsecCollector struct {
	client VICIClient

	up                *prometheus.Desc
	ikeSAs            *prometheus.Desc
	halfOpenIKESAs    *prometheus.Desc
	ikeSAState        *prometheus.Desc
	ikeSAEstablished  *prometheus.Desc
	childSAState      *prometheus.Desc
	childSABytesIn    *prometheus.Desc
	childSABytesOut   *prometheus.Desc
	childSAPacketsIn  *prometheus.Desc
	childSAPacketsOut *prometheus.Desc
	childSAInstalled  *prometheus.Desc
	uptime            *prometheus.Desc
	workersTotal      *prometheus.Desc
	idleWorkers       *prometheus.Desc
	activeWorkers     *prometheus.Desc
	queues            *prometheus.Desc
}

// Compile-time interface checks.
var (
	_ collector.Collector = (*ipsecCollector)(nil)
	_ VICIClient          = (*viciClient)(nil)
)

// New returns an IPsec collector that dials the VICI socket on each collect.
func New(socketPath string) collector.Collector {
	return NewWithClient(&viciClient{socketPath: socketPath})
}

// NewWithClient returns an IPsec collector using the provided VICIClient,
// which is useful for injecting mocks in tests.
func NewWithClient(client VICIClient) collector.Collector {
	ikeLabels := []string{"name", "uid", "remote_host"}
	childLabels := []string{"ike_sa_name", "name", "uid", "remote_host", "local_ts", "remote_ts"}

	return &ipsecCollector{
		client: client,
		up: prometheus.NewDesc(
			"ipsec_up",
			"Whether the VICI socket is reachable (1 = up, 0 = down).",
			nil, nil,
		),
		ikeSAs: prometheus.NewDesc(
			"ipsec_ike_sas",
			"Total number of IKE SAs.",
			nil, nil,
		),
		halfOpenIKESAs: prometheus.NewDesc(
			"ipsec_half_open_ike_sas",
			"Number of half-open IKE SAs.",
			nil, nil,
		),
		ikeSAState: prometheus.NewDesc(
			"ipsec_ike_sa_state",
			"Numeric IKE SA state (0=CREATED..7=DESTROYING).",
			ikeLabels, nil,
		),
		ikeSAEstablished: prometheus.NewDesc(
			"ipsec_ike_sa_established_seconds",
			"Seconds since the IKE SA was established.",
			ikeLabels, nil,
		),
		childSAState: prometheus.NewDesc(
			"ipsec_child_sa_state",
			"Numeric child SA state (0=CREATED..10=DESTROYING).",
			childLabels, nil,
		),
		childSABytesIn: prometheus.NewDesc(
			"ipsec_child_sa_bytes_in",
			"Bytes received on this child SA.",
			childLabels, nil,
		),
		childSABytesOut: prometheus.NewDesc(
			"ipsec_child_sa_bytes_out",
			"Bytes sent on this child SA.",
			childLabels, nil,
		),
		childSAPacketsIn: prometheus.NewDesc(
			"ipsec_child_sa_packets_in",
			"Packets received on this child SA.",
			childLabels, nil,
		),
		childSAPacketsOut: prometheus.NewDesc(
			"ipsec_child_sa_packets_out",
			"Packets sent on this child SA.",
			childLabels, nil,
		),
		childSAInstalled: prometheus.NewDesc(
			"ipsec_child_sa_installed_seconds",
			"Seconds since the child SA was installed.",
			childLabels, nil,
		),
		uptime: prometheus.NewDesc(
			"ipsec_uptime_seconds",
			"Charon daemon uptime in seconds.",
			nil, nil,
		),
		workersTotal: prometheus.NewDesc(
			"ipsec_workers_total",
			"Total number of charon worker threads.",
			nil, nil,
		),
		idleWorkers: prometheus.NewDesc(
			"ipsec_idle_workers",
			"Number of idle charon worker threads.",
			nil, nil,
		),
		activeWorkers: prometheus.NewDesc(
			"ipsec_active_workers",
			"Number of active charon worker threads.",
			nil, nil,
		),
		queues: prometheus.NewDesc(
			"ipsec_queues",
			"Number of queued jobs by priority.",
			[]string{"priority"}, nil,
		),
	}
}

// Name returns the short identifier for this collector.
func (c *ipsecCollector) Name() string { return "ipsec" }

// Describe sends all metric descriptors to the channel.
func (c *ipsecCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.ikeSAs
	ch <- c.halfOpenIKESAs
	ch <- c.ikeSAState
	ch <- c.ikeSAEstablished
	ch <- c.childSAState
	ch <- c.childSABytesIn
	ch <- c.childSABytesOut
	ch <- c.childSAPacketsIn
	ch <- c.childSAPacketsOut
	ch <- c.childSAInstalled
	ch <- c.uptime
	ch <- c.workersTotal
	ch <- c.idleWorkers
	ch <- c.activeWorkers
	ch <- c.queues
}

// Collect queries the VICI socket and emits IPsec metrics.
func (c *ipsecCollector) Collect(ch chan<- prometheus.Metric) {
	if !c.client.IsAvailable() {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)

	// Collect SAs.
	sas, err := c.client.ListSAs()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.ikeSAs, err)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.ikeSAs, prometheus.GaugeValue, float64(len(sas)))

	for _, ike := range sas {
		ikeLabels := []string{ike.Name, ike.UID, ike.RemoteHost}

		ch <- prometheus.MustNewConstMetric(
			c.ikeSAState, prometheus.GaugeValue, float64(ike.State),
			ikeLabels...,
		)
		ch <- prometheus.MustNewConstMetric(
			c.ikeSAEstablished, prometheus.GaugeValue, ike.EstablishedSecs,
			ikeLabels...,
		)

		for _, child := range ike.ChildSAs {
			childLabels := []string{
				ike.Name, child.Name, child.UID,
				ike.RemoteHost, child.LocalTS, child.RemoteTS,
			}

			ch <- prometheus.MustNewConstMetric(
				c.childSAState, prometheus.GaugeValue, float64(child.State),
				childLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.childSABytesIn, prometheus.GaugeValue, float64(child.BytesIn),
				childLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.childSABytesOut, prometheus.GaugeValue, float64(child.BytesOut),
				childLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.childSAPacketsIn, prometheus.GaugeValue, float64(child.PacketsIn),
				childLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.childSAPacketsOut, prometheus.GaugeValue, float64(child.PacketsOut),
				childLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.childSAInstalled, prometheus.GaugeValue, child.InstalledSecs,
				childLabels...,
			)
		}
	}

	// Collect charon stats.
	stats, err := c.client.GetStats()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.uptime, err)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.halfOpenIKESAs, prometheus.GaugeValue, float64(stats.HalfOpenIKE))
	ch <- prometheus.MustNewConstMetric(c.uptime, prometheus.GaugeValue, stats.Uptime)
	ch <- prometheus.MustNewConstMetric(c.workersTotal, prometheus.GaugeValue, float64(stats.Workers))
	ch <- prometheus.MustNewConstMetric(c.idleWorkers, prometheus.GaugeValue, float64(stats.IdleWorkers))
	ch <- prometheus.MustNewConstMetric(c.activeWorkers, prometheus.GaugeValue, float64(stats.ActiveWorkers))

	for priority, count := range stats.Queues {
		ch <- prometheus.MustNewConstMetric(
			c.queues, prometheus.GaugeValue, float64(count),
			priority,
		)
	}
}

// IKEStateValue converts an IKE state string to its numeric value.
// Returns -1 if the state is unknown.
func IKEStateValue(state string) int {
	if v, ok := ikeStateMap[strings.ToUpper(state)]; ok {
		return v
	}
	return -1
}

// ChildStateValue converts a child SA state string to its numeric value.
// Returns -1 if the state is unknown.
func ChildStateValue(state string) int {
	if v, ok := childStateMap[strings.ToUpper(state)]; ok {
		return v
	}
	return -1
}

// ---------------------------------------------------------------------------
// Message abstraction for testability
// ---------------------------------------------------------------------------

// MessageGetter abstracts the key-value access pattern used by vici.Message
// so parsing logic can be tested without a real VICI connection.
type MessageGetter interface {
	Get(key string) any
	Keys() []string
}

// viciMsg wraps *vici.Message to implement MessageGetter.
type viciMsg struct {
	m *vici.Message
}

func (v *viciMsg) Get(key string) any    { return v.m.Get(key) }
func (v *viciMsg) Keys() []string        { return v.m.Keys() }

func wrapMsg(m *vici.Message) MessageGetter {
	if m == nil {
		return nil
	}
	return &viciMsg{m: m}
}

func wrapMsgs(msgs []*vici.Message) []MessageGetter {
	wrapped := make([]MessageGetter, len(msgs))
	for i, m := range msgs {
		wrapped[i] = wrapMsg(m)
	}
	return wrapped
}

// msgStr retrieves a string value from a MessageGetter.
func msgStr(msg MessageGetter, key string) string {
	if msg == nil {
		return ""
	}
	v := msg.Get(key)
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// msgSection retrieves a sub-section from a MessageGetter.
// It handles both *vici.Message (production) and MessageGetter (testing).
func msgSection(msg MessageGetter, key string) MessageGetter {
	if msg == nil {
		return nil
	}
	v := msg.Get(key)
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case *vici.Message:
		return wrapMsg(s)
	case MessageGetter:
		return s
	default:
		return nil
	}
}

// parseIKESAs extracts IKE SA info from a list of VICI response messages.
func parseIKESAs(msgs []MessageGetter) []IKESAInfo {
	var sas []IKESAInfo
	for _, msg := range msgs {
		for _, ikeName := range msg.Keys() {
			ikeSection := msgSection(msg, ikeName)
			if ikeSection == nil {
				continue
			}
			sa := IKESAInfo{
				Name:       ikeName,
				UID:        msgStr(ikeSection, "uniqueid"),
				RemoteHost: msgStr(ikeSection, "remote-host"),
				State:      IKEStateValue(msgStr(ikeSection, "state")),
			}

			estab := msgStr(ikeSection, "established")
			if estab != "" {
				fmt.Sscanf(estab, "%f", &sa.EstablishedSecs)
			}

			// Parse child SAs.
			childSection := msgSection(ikeSection, "child-sas")
			if childSection != nil {
				for _, childName := range childSection.Keys() {
					cSection := msgSection(childSection, childName)
					if cSection == nil {
						continue
					}
					child := ChildSAInfo{
						Name:     childName,
						UID:      msgStr(cSection, "uniqueid"),
						State:    ChildStateValue(msgStr(cSection, "state")),
						LocalTS:  msgStr(cSection, "local-ts"),
						RemoteTS: msgStr(cSection, "remote-ts"),
					}
					fmt.Sscanf(msgStr(cSection, "bytes-in"), "%d", &child.BytesIn)
					fmt.Sscanf(msgStr(cSection, "bytes-out"), "%d", &child.BytesOut)
					fmt.Sscanf(msgStr(cSection, "packets-in"), "%d", &child.PacketsIn)
					fmt.Sscanf(msgStr(cSection, "packets-out"), "%d", &child.PacketsOut)
					fmt.Sscanf(msgStr(cSection, "install-time"), "%f", &child.InstalledSecs)
					sa.ChildSAs = append(sa.ChildSAs, child)
				}
			}

			sas = append(sas, sa)
		}
	}
	return sas
}

// parseCharonStats extracts charon stats from a VICI response message.
func parseCharonStats(msg MessageGetter) CharonStats {
	var stats CharonStats

	// Parse uptime.
	if uptimeSection := msgSection(msg, "uptime"); uptimeSection != nil {
		fmt.Sscanf(msgStr(uptimeSection, "running"), "%f", &stats.Uptime)
	}

	// Parse workers.
	if workersSection := msgSection(msg, "workers"); workersSection != nil {
		fmt.Sscanf(msgStr(workersSection, "total"), "%d", &stats.Workers)
		fmt.Sscanf(msgStr(workersSection, "idle"), "%d", &stats.IdleWorkers)
		fmt.Sscanf(msgStr(workersSection, "active"), "%d", &stats.ActiveWorkers)
	}

	// Parse queues.
	stats.Queues = make(map[string]int)
	if queuesSection := msgSection(msg, "queues"); queuesSection != nil {
		for _, p := range []string{"critical", "high", "medium", "low"} {
			var count int
			fmt.Sscanf(msgStr(queuesSection, p), "%d", &count)
			stats.Queues[p] = count
		}
	}

	// Parse half-open IKE SAs.
	if ikesasSection := msgSection(msg, "ikesas"); ikesasSection != nil {
		fmt.Sscanf(msgStr(ikesasSection, "half-open"), "%d", &stats.HalfOpenIKE)
	}

	return stats
}

// ---------------------------------------------------------------------------
// Real VICI client implementation
// ---------------------------------------------------------------------------

// viciSession abstracts the vici session operations for testability.
type viciSession interface {
	Close() error
	StreamedCommandRequest(cmd, event string, msg *vici.Message) ([]*vici.Message, error)
	CommandRequest(cmd string, msg *vici.Message) (*vici.Message, error)
}

// newViciSessionFn is a function variable for creating a VICI session,
// allowing tests to inject stubs.
var newViciSessionFn = func(socketPath string) (viciSession, error) {
	return vici.NewSession(vici.WithSocketPath(socketPath))
}

// viciClient implements VICIClient by dialing the VICI socket.
type viciClient struct {
	socketPath string
}

func (c *viciClient) IsAvailable() bool {
	session, err := newViciSessionFn(c.socketPath)
	if err != nil {
		return false
	}
	session.Close()
	return true
}

func (c *viciClient) ListSAs() ([]IKESAInfo, error) {
	session, err := newViciSessionFn(c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("vici dial: %w", err)
	}
	defer session.Close()

	msgs, err := session.StreamedCommandRequest("list-sas", "list-sa", nil)
	if err != nil {
		return nil, fmt.Errorf("vici list-sas: %w", err)
	}
	return parseIKESAs(wrapMsgs(msgs)), nil
}

func (c *viciClient) GetStats() (CharonStats, error) {
	session, err := newViciSessionFn(c.socketPath)
	if err != nil {
		return CharonStats{}, fmt.Errorf("vici dial: %w", err)
	}
	defer session.Close()

	msg, err := session.CommandRequest("stats", nil)
	if err != nil {
		return CharonStats{}, fmt.Errorf("vici stats: %w", err)
	}

	return parseCharonStats(wrapMsg(msg)), nil
}
