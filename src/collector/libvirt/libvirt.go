// Package libvirt implements a collector that reports hypervisor and
// virtual machine metrics obtained from the libvirt API.
package libvirt

import (
	"encoding/xml"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	lv "libvirt.org/go/libvirt"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
)

// DomainInfo holds basic information about a libvirt domain.
type DomainInfo struct {
	Name      string
	UUID      string
	State     uint8  // libvirt domain state
	MaxMemory uint64 // bytes
	Memory    uint64 // bytes
	NrVirtCPU uint
	CPUTime   uint64 // nanoseconds
}

// DomainMemoryStat holds a single memory stat entry.
type DomainMemoryStat struct {
	Tag int32
	Val uint64
}

// DomainBlockStats holds block device I/O stats.
type DomainBlockStats struct {
	Device    string
	RdBytes   int64
	WrBytes   int64
	RdReq     int64
	WrReq     int64
}

// DomainInterfaceStats holds network interface stats.
type DomainInterfaceStats struct {
	Name      string
	RxBytes   int64
	TxBytes   int64
	RxPackets int64
	TxPackets int64
	RxErrs    int64
	TxErrs    int64
}

// xmlDomain is used to parse domain XML for device discovery.
type xmlDomain struct {
	Devices xmlDevices `xml:"devices"`
}

type xmlDevices struct {
	Disks      []xmlDisk      `xml:"disk"`
	Interfaces []xmlInterface `xml:"interface"`
}

type xmlDisk struct {
	Target xmlDiskTarget `xml:"target"`
}

type xmlDiskTarget struct {
	Dev string `xml:"dev,attr"`
}

type xmlInterface struct {
	Target xmlInterfaceTarget `xml:"target"`
}

type xmlInterfaceTarget struct {
	Dev string `xml:"dev,attr"`
}

// memoryStatTagNames maps libvirt memory stat tags to human-readable names.
var memoryStatTagNames = map[int32]string{
	0:  "swap_in",
	1:  "swap_out",
	2:  "major_fault",
	3:  "minor_fault",
	4:  "unused",
	5:  "available",
	6:  "actual",
	7:  "rss",
	8:  "usable",
	9:  "last_update",
	10: "disk_caches",
}

// LibvirtClient abstracts the libvirt connection interface so the collector
// can be tested without a real libvirtd daemon.
type LibvirtClient interface {
	IsAvailable() bool
	GetHostCPUCount() (uint, error)
	GetHostMemoryBytes() (uint64, error)
	GetHostFreeMemoryBytes() (uint64, error)
	ListDomains() ([]DomainInfo, error)
	GetDomainMemoryStats(name string) ([]DomainMemoryStat, error)
	GetDomainBlockStats(name string) ([]DomainBlockStats, error)
	GetDomainInterfaceStats(name string) ([]DomainInterfaceStats, error)
}

// libvirtCollector implements collector.Collector for libvirt metrics.
type libvirtCollector struct {
	client LibvirtClient

	up             *prometheus.Desc
	domainsTotal   *prometheus.Desc
	hostCPUCount   *prometheus.Desc
	hostMemory     *prometheus.Desc
	hostFreeMemory *prometheus.Desc

	domainState     *prometheus.Desc
	domainMaxMemory *prometheus.Desc
	domainMemory    *prometheus.Desc
	domainVCPUs     *prometheus.Desc
	domainCPUTime   *prometheus.Desc

	domainMemoryStats *prometheus.Desc

	domainBlockReadBytes   *prometheus.Desc
	domainBlockWriteBytes  *prometheus.Desc
	domainBlockReadReqs    *prometheus.Desc
	domainBlockWriteReqs   *prometheus.Desc

	domainNetRxBytes   *prometheus.Desc
	domainNetTxBytes   *prometheus.Desc
	domainNetRxPackets *prometheus.Desc
	domainNetTxPackets *prometheus.Desc
	domainNetRxErrors  *prometheus.Desc
	domainNetTxErrors  *prometheus.Desc
}

// Compile-time interface checks.
var (
	_ collector.Collector = (*libvirtCollector)(nil)
	_ LibvirtClient       = (*libvirtClient)(nil)
)

// New returns a libvirt collector that connects to libvirtd on each collect.
func New(uri string) collector.Collector {
	return NewWithClient(&libvirtClient{uri: uri})
}

// NewWithClient returns a libvirt collector using the provided LibvirtClient,
// which is useful for injecting mocks in tests.
func NewWithClient(client LibvirtClient) collector.Collector {
	domainLabels := []string{"domain", "uuid"}
	domainDeviceLabels := []string{"domain", "uuid", "device"}
	domainIfaceLabels := []string{"domain", "uuid", "interface"}
	domainMemStatLabels := []string{"domain", "uuid", "stat"}

	return &libvirtCollector{
		client: client,
		up: prometheus.NewDesc(
			"libvirt_up",
			"Whether libvirtd is reachable (1 = up, 0 = down).",
			nil, nil,
		),
		domainsTotal: prometheus.NewDesc(
			"libvirt_domains_total",
			"Total number of defined domains.",
			nil, nil,
		),
		hostCPUCount: prometheus.NewDesc(
			"libvirt_host_cpu_count",
			"Number of host CPUs.",
			nil, nil,
		),
		hostMemory: prometheus.NewDesc(
			"libvirt_host_memory_bytes",
			"Total host memory in bytes.",
			nil, nil,
		),
		hostFreeMemory: prometheus.NewDesc(
			"libvirt_host_free_memory_bytes",
			"Free host memory in bytes.",
			nil, nil,
		),
		domainState: prometheus.NewDesc(
			"libvirt_domain_info_state",
			"Domain state (1=running, 2=blocked, 3=paused, 4=shutdown, 5=shutoff, 6=crashed, 7=pmsuspended).",
			domainLabels, nil,
		),
		domainMaxMemory: prometheus.NewDesc(
			"libvirt_domain_info_max_memory_bytes",
			"Configured maximum memory for the domain in bytes.",
			domainLabels, nil,
		),
		domainMemory: prometheus.NewDesc(
			"libvirt_domain_info_memory_bytes",
			"Current memory allocation for the domain in bytes.",
			domainLabels, nil,
		),
		domainVCPUs: prometheus.NewDesc(
			"libvirt_domain_info_vcpus",
			"Number of virtual CPUs for the domain.",
			domainLabels, nil,
		),
		domainCPUTime: prometheus.NewDesc(
			"libvirt_domain_cpu_time_seconds_total",
			"Total CPU time consumed by the domain in seconds.",
			domainLabels, nil,
		),
		domainMemoryStats: prometheus.NewDesc(
			"libvirt_domain_memory_stats_bytes",
			"Memory statistics for the domain in bytes.",
			domainMemStatLabels, nil,
		),
		domainBlockReadBytes: prometheus.NewDesc(
			"libvirt_domain_block_read_bytes_total",
			"Total bytes read from block device.",
			domainDeviceLabels, nil,
		),
		domainBlockWriteBytes: prometheus.NewDesc(
			"libvirt_domain_block_write_bytes_total",
			"Total bytes written to block device.",
			domainDeviceLabels, nil,
		),
		domainBlockReadReqs: prometheus.NewDesc(
			"libvirt_domain_block_read_requests_total",
			"Total read requests to block device.",
			domainDeviceLabels, nil,
		),
		domainBlockWriteReqs: prometheus.NewDesc(
			"libvirt_domain_block_write_requests_total",
			"Total write requests to block device.",
			domainDeviceLabels, nil,
		),
		domainNetRxBytes: prometheus.NewDesc(
			"libvirt_domain_net_receive_bytes_total",
			"Total bytes received on network interface.",
			domainIfaceLabels, nil,
		),
		domainNetTxBytes: prometheus.NewDesc(
			"libvirt_domain_net_transmit_bytes_total",
			"Total bytes transmitted on network interface.",
			domainIfaceLabels, nil,
		),
		domainNetRxPackets: prometheus.NewDesc(
			"libvirt_domain_net_receive_packets_total",
			"Total packets received on network interface.",
			domainIfaceLabels, nil,
		),
		domainNetTxPackets: prometheus.NewDesc(
			"libvirt_domain_net_transmit_packets_total",
			"Total packets transmitted on network interface.",
			domainIfaceLabels, nil,
		),
		domainNetRxErrors: prometheus.NewDesc(
			"libvirt_domain_net_receive_errors_total",
			"Total receive errors on network interface.",
			domainIfaceLabels, nil,
		),
		domainNetTxErrors: prometheus.NewDesc(
			"libvirt_domain_net_transmit_errors_total",
			"Total transmit errors on network interface.",
			domainIfaceLabels, nil,
		),
	}
}

// Name returns the short identifier for this collector.
func (c *libvirtCollector) Name() string { return "libvirt" }

// Describe sends all metric descriptors to the channel.
func (c *libvirtCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.domainsTotal
	ch <- c.hostCPUCount
	ch <- c.hostMemory
	ch <- c.hostFreeMemory
	ch <- c.domainState
	ch <- c.domainMaxMemory
	ch <- c.domainMemory
	ch <- c.domainVCPUs
	ch <- c.domainCPUTime
	ch <- c.domainMemoryStats
	ch <- c.domainBlockReadBytes
	ch <- c.domainBlockWriteBytes
	ch <- c.domainBlockReadReqs
	ch <- c.domainBlockWriteReqs
	ch <- c.domainNetRxBytes
	ch <- c.domainNetTxBytes
	ch <- c.domainNetRxPackets
	ch <- c.domainNetTxPackets
	ch <- c.domainNetRxErrors
	ch <- c.domainNetTxErrors
}

// Collect queries libvirtd and emits metrics.
func (c *libvirtCollector) Collect(ch chan<- prometheus.Metric) {
	if !c.client.IsAvailable() {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)

	// Host metrics.
	c.collectHostMetrics(ch)

	// Domain metrics.
	domains, err := c.client.ListDomains()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.domainsTotal, err)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.domainsTotal, prometheus.GaugeValue, float64(len(domains)))

	for _, d := range domains {
		labels := []string{d.Name, d.UUID}

		ch <- prometheus.MustNewConstMetric(c.domainState, prometheus.GaugeValue, float64(d.State), labels...)
		ch <- prometheus.MustNewConstMetric(c.domainMaxMemory, prometheus.GaugeValue, float64(d.MaxMemory), labels...)
		ch <- prometheus.MustNewConstMetric(c.domainMemory, prometheus.GaugeValue, float64(d.Memory), labels...)
		ch <- prometheus.MustNewConstMetric(c.domainVCPUs, prometheus.GaugeValue, float64(d.NrVirtCPU), labels...)
		ch <- prometheus.MustNewConstMetric(c.domainCPUTime, prometheus.CounterValue, float64(d.CPUTime)/1e9, labels...)

		c.collectDomainMemoryStats(ch, d)
		c.collectDomainBlockStats(ch, d)
		c.collectDomainInterfaceStats(ch, d)
	}
}

func (c *libvirtCollector) collectHostMetrics(ch chan<- prometheus.Metric) {
	cpuCount, err := c.client.GetHostCPUCount()
	if err != nil {
		slog.Warn("failed to get host CPU count", "error", err)
	} else {
		ch <- prometheus.MustNewConstMetric(c.hostCPUCount, prometheus.GaugeValue, float64(cpuCount))
	}

	mem, err := c.client.GetHostMemoryBytes()
	if err != nil {
		slog.Warn("failed to get host memory", "error", err)
	} else {
		ch <- prometheus.MustNewConstMetric(c.hostMemory, prometheus.GaugeValue, float64(mem))
	}

	freeMem, err := c.client.GetHostFreeMemoryBytes()
	if err != nil {
		slog.Warn("failed to get host free memory", "error", err)
	} else {
		ch <- prometheus.MustNewConstMetric(c.hostFreeMemory, prometheus.GaugeValue, float64(freeMem))
	}
}

func (c *libvirtCollector) collectDomainMemoryStats(ch chan<- prometheus.Metric, d DomainInfo) {
	stats, err := c.client.GetDomainMemoryStats(d.Name)
	if err != nil {
		slog.Debug("failed to get memory stats", "domain", d.Name, "error", err)
		return
	}

	for _, s := range stats {
		tagName, ok := memoryStatTagNames[s.Tag]
		if !ok {
			tagName = fmt.Sprintf("unknown_%d", s.Tag)
		}
		// Memory stats from libvirt are in KiB; convert to bytes.
		ch <- prometheus.MustNewConstMetric(c.domainMemoryStats, prometheus.GaugeValue, float64(s.Val)*1024,
			d.Name, d.UUID, tagName,
		)
	}
}

func (c *libvirtCollector) collectDomainBlockStats(ch chan<- prometheus.Metric, d DomainInfo) {
	blockStats, err := c.client.GetDomainBlockStats(d.Name)
	if err != nil {
		slog.Debug("failed to get block stats", "domain", d.Name, "error", err)
		return
	}

	for _, bs := range blockStats {
		deviceLabels := []string{d.Name, d.UUID, bs.Device}
		ch <- prometheus.MustNewConstMetric(c.domainBlockReadBytes, prometheus.CounterValue, float64(bs.RdBytes), deviceLabels...)
		ch <- prometheus.MustNewConstMetric(c.domainBlockWriteBytes, prometheus.CounterValue, float64(bs.WrBytes), deviceLabels...)
		ch <- prometheus.MustNewConstMetric(c.domainBlockReadReqs, prometheus.CounterValue, float64(bs.RdReq), deviceLabels...)
		ch <- prometheus.MustNewConstMetric(c.domainBlockWriteReqs, prometheus.CounterValue, float64(bs.WrReq), deviceLabels...)
	}
}

func (c *libvirtCollector) collectDomainInterfaceStats(ch chan<- prometheus.Metric, d DomainInfo) {
	ifaceStats, err := c.client.GetDomainInterfaceStats(d.Name)
	if err != nil {
		slog.Debug("failed to get interface stats", "domain", d.Name, "error", err)
		return
	}

	for _, is := range ifaceStats {
		ifaceLabels := []string{d.Name, d.UUID, is.Name}
		ch <- prometheus.MustNewConstMetric(c.domainNetRxBytes, prometheus.CounterValue, float64(is.RxBytes), ifaceLabels...)
		ch <- prometheus.MustNewConstMetric(c.domainNetTxBytes, prometheus.CounterValue, float64(is.TxBytes), ifaceLabels...)
		ch <- prometheus.MustNewConstMetric(c.domainNetRxPackets, prometheus.CounterValue, float64(is.RxPackets), ifaceLabels...)
		ch <- prometheus.MustNewConstMetric(c.domainNetTxPackets, prometheus.CounterValue, float64(is.TxPackets), ifaceLabels...)
		ch <- prometheus.MustNewConstMetric(c.domainNetRxErrors, prometheus.CounterValue, float64(is.RxErrs), ifaceLabels...)
		ch <- prometheus.MustNewConstMetric(c.domainNetTxErrors, prometheus.CounterValue, float64(is.TxErrs), ifaceLabels...)
	}
}

// ---------------------------------------------------------------------------
// Real libvirt client implementation
// ---------------------------------------------------------------------------

// libvirtClient implements LibvirtClient by connecting to libvirtd.
type libvirtClient struct {
	uri string
}

func (c *libvirtClient) connect() (*lv.Connect, error) {
	conn, err := lv.NewConnect(c.uri)
	if err != nil {
		return nil, fmt.Errorf("libvirt connect %s: %w", c.uri, err)
	}
	return conn, nil
}

func (c *libvirtClient) IsAvailable() bool {
	conn, err := c.connect()
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (c *libvirtClient) GetHostCPUCount() (uint, error) {
	conn, err := c.connect()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	info, err := conn.GetNodeInfo()
	if err != nil {
		return 0, fmt.Errorf("get node info: %w", err)
	}
	return info.Cpus, nil
}

func (c *libvirtClient) GetHostMemoryBytes() (uint64, error) {
	conn, err := c.connect()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	info, err := conn.GetNodeInfo()
	if err != nil {
		return 0, fmt.Errorf("get node info: %w", err)
	}
	// NodeInfo.Memory is in KiB.
	return info.Memory * 1024, nil
}

func (c *libvirtClient) GetHostFreeMemoryBytes() (uint64, error) {
	conn, err := c.connect()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	free, err := conn.GetFreeMemory()
	if err != nil {
		return 0, fmt.Errorf("get free memory: %w", err)
	}
	return free, nil
}

func (c *libvirtClient) ListDomains() ([]DomainInfo, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	domains, err := conn.ListAllDomains(lv.CONNECT_LIST_DOMAINS_ACTIVE | lv.CONNECT_LIST_DOMAINS_INACTIVE)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}

	var result []DomainInfo
	for _, dom := range domains {
		name, err := dom.GetName()
		if err != nil {
			dom.Free()
			continue
		}
		uuid, err := dom.GetUUIDString()
		if err != nil {
			dom.Free()
			continue
		}
		info, err := dom.GetInfo()
		if err != nil {
			dom.Free()
			continue
		}

		result = append(result, DomainInfo{
			Name:      name,
			UUID:      uuid,
			State:     uint8(info.State),
			MaxMemory: info.MaxMem * 1024, // KiB to bytes
			Memory:    info.Memory * 1024,  // KiB to bytes
			NrVirtCPU: info.NrVirtCpu,
			CPUTime:   info.CpuTime,
		})
		dom.Free()
	}

	return result, nil
}

func (c *libvirtClient) GetDomainMemoryStats(name string) ([]DomainMemoryStat, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return nil, fmt.Errorf("lookup domain %s: %w", name, err)
	}
	defer dom.Free()

	stats, err := dom.MemoryStats(uint32(lv.DOMAIN_MEMORY_STAT_NR), 0)
	if err != nil {
		return nil, fmt.Errorf("memory stats %s: %w", name, err)
	}

	var result []DomainMemoryStat
	for _, s := range stats {
		result = append(result, DomainMemoryStat{
			Tag: int32(s.Tag),
			Val: s.Val,
		})
	}
	return result, nil
}

func (c *libvirtClient) GetDomainBlockStats(name string) ([]DomainBlockStats, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return nil, fmt.Errorf("lookup domain %s: %w", name, err)
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return nil, fmt.Errorf("get xml desc %s: %w", name, err)
	}

	var domXML xmlDomain
	if err := xml.Unmarshal([]byte(xmlDesc), &domXML); err != nil {
		return nil, fmt.Errorf("parse xml %s: %w", name, err)
	}

	var result []DomainBlockStats
	for _, disk := range domXML.Devices.Disks {
		dev := disk.Target.Dev
		if dev == "" {
			continue
		}
		bs, err := dom.BlockStats(dev)
		if err != nil {
			slog.Debug("failed to get block stats for device", "domain", name, "device", dev, "error", err)
			continue
		}
		result = append(result, DomainBlockStats{
			Device:  dev,
			RdBytes: bs.RdBytes,
			WrBytes: bs.WrBytes,
			RdReq:   bs.RdReq,
			WrReq:   bs.WrReq,
		})
	}

	return result, nil
}

func (c *libvirtClient) GetDomainInterfaceStats(name string) ([]DomainInterfaceStats, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return nil, fmt.Errorf("lookup domain %s: %w", name, err)
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return nil, fmt.Errorf("get xml desc %s: %w", name, err)
	}

	var domXML xmlDomain
	if err := xml.Unmarshal([]byte(xmlDesc), &domXML); err != nil {
		return nil, fmt.Errorf("parse xml %s: %w", name, err)
	}

	var result []DomainInterfaceStats
	for _, iface := range domXML.Devices.Interfaces {
		dev := iface.Target.Dev
		if dev == "" {
			continue
		}
		is, err := dom.InterfaceStats(dev)
		if err != nil {
			slog.Debug("failed to get interface stats for device", "domain", name, "interface", dev, "error", err)
			continue
		}
		result = append(result, DomainInterfaceStats{
			Name:      dev,
			RxBytes:   is.RxBytes,
			TxBytes:   is.TxBytes,
			RxPackets: is.RxPackets,
			TxPackets: is.TxPackets,
			RxErrs:    is.RxErrs,
			TxErrs:    is.TxErrs,
		})
	}

	return result, nil
}
