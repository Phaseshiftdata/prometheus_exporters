// Package iface implements a collector that reports network interface
// classification, bond membership, and bridge membership as Prometheus metrics.
package iface

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
)

// LinkInfo describes a single network interface and its classification.
type LinkInfo struct {
	Name       string
	Type       string // physical, bond, bridge, vti, veth, loopback, other
	Driver     string
	MasterName string
	MasterType string // bond, bridge, or empty
}

// LinkLister abstracts retrieval of network interface information so the
// collector can be tested without real sysfs access.
type LinkLister interface {
	ListLinks() ([]LinkInfo, error)
}

// sysfsLister is the production LinkLister backed by sysfs.
type sysfsLister struct {
	sysPath string
}

// Compile-time interface check.
var _ LinkLister = (*sysfsLister)(nil)

func (s *sysfsLister) ListLinks() ([]LinkInfo, error) {
	netDir := filepath.Join(s.sysPath, "class", "net")
	entries, err := os.ReadDir(netDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", netDir, err)
	}

	// First pass: classify each interface.
	infos := make(map[string]*LinkInfo, len(entries))
	order := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		li := &LinkInfo{Name: name}

		devDir := filepath.Join(netDir, name)

		// Detect driver from device/driver symlink.
		driverLink, err := os.Readlink(filepath.Join(devDir, "device", "driver"))
		if err == nil {
			li.Driver = filepath.Base(driverLink)
		}

		// Classify type.
		if _, err := os.Stat(filepath.Join(devDir, "bonding")); err == nil {
			li.Type = "bond"
		} else if _, err := os.Stat(filepath.Join(devDir, "bridge")); err == nil {
			li.Type = "bridge"
		} else {
			li.Type = s.classifyByTypeFile(devDir, name)
		}

		infos[name] = li
		order = append(order, name)
	}

	// Second pass: resolve master relationships.
	for _, name := range order {
		li := infos[name]
		devDir := filepath.Join(netDir, name)

		masterLink, err := os.Readlink(filepath.Join(devDir, "master"))
		if err != nil {
			continue
		}
		masterName := filepath.Base(masterLink)
		li.MasterName = masterName
		if master, ok := infos[masterName]; ok {
			li.MasterType = master.Type
		}
	}

	result := make([]LinkInfo, 0, len(order))
	for _, name := range order {
		result = append(result, *infos[name])
	}
	return result, nil
}

// classifyByTypeFile determines the interface type from the sysfs type file
// and the interface name.
func (s *sysfsLister) classifyByTypeFile(devDir, name string) string {
	data, err := os.ReadFile(filepath.Join(devDir, "type"))
	if err != nil {
		return "other"
	}
	typeStr := strings.TrimSpace(string(data))
	switch typeStr {
	case "0": // ethernet
		if strings.HasPrefix(name, "veth") {
			return "veth"
		}
		return "physical"
	case "768": // tunnel / vti
		return "vti"
	case "772": // loopback
		return "loopback"
	default:
		return "other"
	}
}

// ifaceCollector implements collector.Collector for interface classification.
type ifaceCollector struct {
	lister     LinkLister
	typeDesc   *prometheus.Desc
	bondDesc   *prometheus.Desc
	bridgeDesc *prometheus.Desc
}

// Compile-time interface check.
var _ collector.Collector = (*ifaceCollector)(nil)

// New returns an interface collector backed by sysfs at the given path
// (typically "/sys").
func New(sysPath string) collector.Collector {
	return NewWithLister(&sysfsLister{sysPath: sysPath})
}

// NewWithLister returns an interface collector using the provided LinkLister,
// which is useful for injecting mocks in tests.
func NewWithLister(lister LinkLister) collector.Collector {
	return &ifaceCollector{
		lister: lister,
		typeDesc: prometheus.NewDesc(
			"network_interface_type",
			"Interface type classification; value is always 1.",
			[]string{"device", "type", "driver"},
			nil,
		),
		bondDesc: prometheus.NewDesc(
			"network_bond_member",
			"Bond membership; value is always 1.",
			[]string{"bond", "member"},
			nil,
		),
		bridgeDesc: prometheus.NewDesc(
			"network_bridge_member",
			"Bridge membership; value is always 1.",
			[]string{"bridge", "member"},
			nil,
		),
	}
}

// Name returns the short identifier for this collector.
func (c *ifaceCollector) Name() string { return "iface" }

// Describe sends all metric descriptors to the channel.
func (c *ifaceCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.typeDesc
	ch <- c.bondDesc
	ch <- c.bridgeDesc
}

// Collect queries the interface list and emits classification and membership
// metrics.
func (c *ifaceCollector) Collect(ch chan<- prometheus.Metric) {
	links, err := c.lister.ListLinks()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.typeDesc, err)
		return
	}
	for _, li := range links {
		ch <- prometheus.MustNewConstMetric(
			c.typeDesc,
			prometheus.GaugeValue,
			1,
			li.Name,
			li.Type,
			li.Driver,
		)

		if li.MasterName != "" && li.MasterType == "bond" {
			ch <- prometheus.MustNewConstMetric(
				c.bondDesc,
				prometheus.GaugeValue,
				1,
				li.MasterName,
				li.Name,
			)
		}
		if li.MasterName != "" && li.MasterType == "bridge" {
			ch <- prometheus.MustNewConstMetric(
				c.bridgeDesc,
				prometheus.GaugeValue,
				1,
				li.MasterName,
				li.Name,
			)
		}
	}
}
