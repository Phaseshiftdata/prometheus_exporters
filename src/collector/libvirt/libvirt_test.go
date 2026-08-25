package libvirt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
)

// Compile-time interface checks.
var (
	_ LibvirtClient       = (*mockLibvirtClient)(nil)
	_ collector.Collector = (*libvirtCollector)(nil)
)

// mockLibvirtClient implements LibvirtClient for testing.
type mockLibvirtClient struct {
	available      bool
	hostCPUs       uint
	hostCPUsErr    error
	hostMem        uint64
	hostMemErr     error
	hostFreeMem    uint64
	hostFreeMemErr error
	domains        []DomainInfo
	domainsErr     error
	memStats       map[string][]DomainMemoryStat
	memStatsErr    map[string]error
	blockStats     map[string][]DomainBlockStats
	blockStatsErr  map[string]error
	ifaceStats     map[string][]DomainInterfaceStats
	ifaceStatsErr  map[string]error
}

func (m *mockLibvirtClient) IsAvailable() bool                       { return m.available }
func (m *mockLibvirtClient) GetHostCPUCount() (uint, error)          { return m.hostCPUs, m.hostCPUsErr }
func (m *mockLibvirtClient) GetHostMemoryBytes() (uint64, error)     { return m.hostMem, m.hostMemErr }
func (m *mockLibvirtClient) GetHostFreeMemoryBytes() (uint64, error) { return m.hostFreeMem, m.hostFreeMemErr }
func (m *mockLibvirtClient) ListDomains() ([]DomainInfo, error)      { return m.domains, m.domainsErr }

func (m *mockLibvirtClient) GetDomainMemoryStats(uuid string) ([]DomainMemoryStat, error) {
	if m.memStatsErr != nil {
		if err, ok := m.memStatsErr[uuid]; ok {
			return nil, err
		}
	}
	if m.memStats != nil {
		return m.memStats[uuid], nil
	}
	return nil, nil
}

func (m *mockLibvirtClient) GetDomainBlockStats(uuid string) ([]DomainBlockStats, error) {
	if m.blockStatsErr != nil {
		if err, ok := m.blockStatsErr[uuid]; ok {
			return nil, err
		}
	}
	if m.blockStats != nil {
		return m.blockStats[uuid], nil
	}
	return nil, nil
}

func (m *mockLibvirtClient) GetDomainInterfaceStats(uuid string) ([]DomainInterfaceStats, error) {
	if m.ifaceStatsErr != nil {
		if err, ok := m.ifaceStatsErr[uuid]; ok {
			return nil, err
		}
	}
	if m.ifaceStats != nil {
		return m.ifaceStats[uuid], nil
	}
	return nil, nil
}

func TestName(t *testing.T) {
	c := NewWithClient(&mockLibvirtClient{})
	if c.Name() != "libvirt" {
		t.Errorf("expected name 'libvirt', got %q", c.Name())
	}
}

func TestDescribe(t *testing.T) {
	c := NewWithClient(&mockLibvirtClient{})
	ch := make(chan *prometheus.Desc, 30)
	c.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	if len(descs) != 21 {
		t.Fatalf("expected 21 descriptors, got %d", len(descs))
	}
}

func TestLibvirtUnavailable(t *testing.T) {
	client := &mockLibvirtClient{available: false}
	c := NewWithClient(client)

	expected := `
# HELP libvirt_up Whether libvirtd is reachable (1 = up, 0 = down).
# TYPE libvirt_up gauge
libvirt_up 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_up"); err != nil {
		t.Error(err)
	}

	// No other metrics should be emitted.
	count := testutil.CollectAndCount(c,
		"libvirt_domains_total",
		"libvirt_domain_info_state",
		"libvirt_host_cpu_count",
	)
	if count != 0 {
		t.Errorf("expected 0 metrics when unavailable, got %d", count)
	}
}

func TestEmptyDomainList(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    8,
		hostMem:     34359738368, // 32 GiB
		hostFreeMem: 17179869184, // 16 GiB
		domains:     []DomainInfo{},
	}

	c := NewWithClient(client)

	expected := `
# HELP libvirt_up Whether libvirtd is reachable (1 = up, 0 = down).
# TYPE libvirt_up gauge
libvirt_up 1
# HELP libvirt_domains_total Total number of defined domains.
# TYPE libvirt_domains_total gauge
libvirt_domains_total 0
# HELP libvirt_host_cpu_count Number of host CPUs.
# TYPE libvirt_host_cpu_count gauge
libvirt_host_cpu_count 8
# HELP libvirt_host_memory_bytes Total host memory in bytes.
# TYPE libvirt_host_memory_bytes gauge
libvirt_host_memory_bytes 3.4359738368e+10
# HELP libvirt_host_free_memory_bytes Free host memory in bytes.
# TYPE libvirt_host_free_memory_bytes gauge
libvirt_host_free_memory_bytes 1.7179869184e+10
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"libvirt_up", "libvirt_domains_total",
		"libvirt_host_cpu_count", "libvirt_host_memory_bytes", "libvirt_host_free_memory_bytes",
	); err != nil {
		t.Error(err)
	}
}

func TestDomainBasicMetrics(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    4,
		hostMem:     8589934592,
		hostFreeMem: 4294967296,
		domains: []DomainInfo{
			{
				Name:      "web-server",
				UUID:      "550e8400-e29b-41d4-a716-446655440000",
				State:     1, // running
				MaxMemory: 4294967296,
				Memory:    2147483648,
				NrVirtCPU: 2,
				CPUTime:   123456789000, // nanoseconds
			},
		},
	}

	c := NewWithClient(client)

	// Check domain state.
	expected := `
# HELP libvirt_domain_info_state Domain state (1=running, 2=blocked, 3=paused, 4=shutdown, 5=shutoff, 6=crashed, 7=pmsuspended).
# TYPE libvirt_domain_info_state gauge
libvirt_domain_info_state{domain="web-server",uuid="550e8400-e29b-41d4-a716-446655440000"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_domain_info_state"); err != nil {
		t.Error(err)
	}

	// Check domain max memory.
	expected = `
# HELP libvirt_domain_info_max_memory_bytes Configured maximum memory for the domain in bytes.
# TYPE libvirt_domain_info_max_memory_bytes gauge
libvirt_domain_info_max_memory_bytes{domain="web-server",uuid="550e8400-e29b-41d4-a716-446655440000"} 4.294967296e+09
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_domain_info_max_memory_bytes"); err != nil {
		t.Error(err)
	}

	// Check domain vCPUs.
	expected = `
# HELP libvirt_domain_info_vcpus Number of virtual CPUs for the domain.
# TYPE libvirt_domain_info_vcpus gauge
libvirt_domain_info_vcpus{domain="web-server",uuid="550e8400-e29b-41d4-a716-446655440000"} 2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_domain_info_vcpus"); err != nil {
		t.Error(err)
	}

	// Check CPU time (nanoseconds to seconds: 123456789000 / 1e9 = 123.456789).
	expected = `
# HELP libvirt_domain_cpu_time_seconds_total Total CPU time consumed by the domain in seconds.
# TYPE libvirt_domain_cpu_time_seconds_total counter
libvirt_domain_cpu_time_seconds_total{domain="web-server",uuid="550e8400-e29b-41d4-a716-446655440000"} 123.456789
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_domain_cpu_time_seconds_total"); err != nil {
		t.Error(err)
	}
}

func TestDomainMemoryStats(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    4,
		hostMem:     8589934592,
		hostFreeMem: 4294967296,
		domains: []DomainInfo{
			{
				Name:      "db-server",
				UUID:      "550e8400-e29b-41d4-a716-446655440001",
				State:     1,
				MaxMemory: 4294967296,
				Memory:    4294967296,
				NrVirtCPU: 4,
				CPUTime:   0,
			},
		},
		memStats: map[string][]DomainMemoryStat{
			"550e8400-e29b-41d4-a716-446655440001": {
				{Tag: 6, Val: 4194304},  // actual (4 GiB in KiB)
				{Tag: 4, Val: 1048576},  // unused (1 GiB in KiB)
				{Tag: 5, Val: 3932160},  // available
				{Tag: 7, Val: 4300800},  // rss
				{Tag: 8, Val: 2097152},  // usable
			},
		},
	}

	c := NewWithClient(client)

	// Verify the correct number of memory stat metrics are emitted.
	count := testutil.CollectAndCount(c, "libvirt_domain_memory_stats_bytes")
	if count != 5 {
		t.Errorf("expected 5 memory stat metrics, got %d", count)
	}
}

func TestDomainBlockStats(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    4,
		hostMem:     8589934592,
		hostFreeMem: 4294967296,
		domains: []DomainInfo{
			{
				Name:      "web-server",
				UUID:      "550e8400-e29b-41d4-a716-446655440000",
				State:     1,
				MaxMemory: 4294967296,
				Memory:    2147483648,
				NrVirtCPU: 2,
				CPUTime:   0,
			},
		},
		blockStats: map[string][]DomainBlockStats{
			"550e8400-e29b-41d4-a716-446655440000": {
				{Device: "vda", RdBytes: 1048576, WrBytes: 2097152, RdReq: 1000, WrReq: 2000},
				{Device: "vdb", RdBytes: 524288, WrBytes: 1048576, RdReq: 500, WrReq: 1000},
			},
		},
	}

	c := NewWithClient(client)

	expected := `
# HELP libvirt_domain_block_read_bytes_total Total bytes read from block device.
# TYPE libvirt_domain_block_read_bytes_total counter
libvirt_domain_block_read_bytes_total{device="vda",domain="web-server",uuid="550e8400-e29b-41d4-a716-446655440000"} 1.048576e+06
libvirt_domain_block_read_bytes_total{device="vdb",domain="web-server",uuid="550e8400-e29b-41d4-a716-446655440000"} 524288
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_domain_block_read_bytes_total"); err != nil {
		t.Error(err)
	}

	expected = `
# HELP libvirt_domain_block_write_requests_total Total write requests to block device.
# TYPE libvirt_domain_block_write_requests_total counter
libvirt_domain_block_write_requests_total{device="vda",domain="web-server",uuid="550e8400-e29b-41d4-a716-446655440000"} 2000
libvirt_domain_block_write_requests_total{device="vdb",domain="web-server",uuid="550e8400-e29b-41d4-a716-446655440000"} 1000
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_domain_block_write_requests_total"); err != nil {
		t.Error(err)
	}
}

func TestDomainInterfaceStats(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    4,
		hostMem:     8589934592,
		hostFreeMem: 4294967296,
		domains: []DomainInfo{
			{
				Name:      "web-server",
				UUID:      "550e8400-e29b-41d4-a716-446655440000",
				State:     1,
				MaxMemory: 4294967296,
				Memory:    2147483648,
				NrVirtCPU: 2,
				CPUTime:   0,
			},
		},
		ifaceStats: map[string][]DomainInterfaceStats{
			"550e8400-e29b-41d4-a716-446655440000": {
				{
					Name:      "vnet0",
					RxBytes:   10485760,
					TxBytes:   5242880,
					RxPackets: 10000,
					TxPackets: 5000,
					RxErrs:    5,
					TxErrs:    2,
				},
			},
		},
	}

	c := NewWithClient(client)

	expected := `
# HELP libvirt_domain_net_receive_bytes_total Total bytes received on network interface.
# TYPE libvirt_domain_net_receive_bytes_total counter
libvirt_domain_net_receive_bytes_total{domain="web-server",interface="vnet0",uuid="550e8400-e29b-41d4-a716-446655440000"} 1.048576e+07
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_domain_net_receive_bytes_total"); err != nil {
		t.Error(err)
	}

	expected = `
# HELP libvirt_domain_net_receive_errors_total Total receive errors on network interface.
# TYPE libvirt_domain_net_receive_errors_total counter
libvirt_domain_net_receive_errors_total{domain="web-server",interface="vnet0",uuid="550e8400-e29b-41d4-a716-446655440000"} 5
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_domain_net_receive_errors_total"); err != nil {
		t.Error(err)
	}

	expected = `
# HELP libvirt_domain_net_transmit_errors_total Total transmit errors on network interface.
# TYPE libvirt_domain_net_transmit_errors_total counter
libvirt_domain_net_transmit_errors_total{domain="web-server",interface="vnet0",uuid="550e8400-e29b-41d4-a716-446655440000"} 2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_domain_net_transmit_errors_total"); err != nil {
		t.Error(err)
	}
}

func TestMultipleDomains(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    16,
		hostMem:     68719476736,
		hostFreeMem: 34359738368,
		domains: []DomainInfo{
			{
				Name:      "web-1",
				UUID:      "uuid-web-1",
				State:     1, // running
				MaxMemory: 4294967296,
				Memory:    2147483648,
				NrVirtCPU: 2,
				CPUTime:   1000000000,
			},
			{
				Name:      "db-1",
				UUID:      "uuid-db-1",
				State:     1, // running
				MaxMemory: 8589934592,
				Memory:    8589934592,
				NrVirtCPU: 4,
				CPUTime:   5000000000,
			},
			{
				Name:      "stopped-vm",
				UUID:      "uuid-stopped",
				State:     5, // shutoff
				MaxMemory: 2147483648,
				Memory:    0,
				NrVirtCPU: 1,
				CPUTime:   0,
			},
		},
	}

	c := NewWithClient(client)

	// Check domain count.
	expected := `
# HELP libvirt_domains_total Total number of defined domains.
# TYPE libvirt_domains_total gauge
libvirt_domains_total 3
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "libvirt_domains_total"); err != nil {
		t.Error(err)
	}

	// Check all domain states are present.
	count := testutil.CollectAndCount(c, "libvirt_domain_info_state")
	if count != 3 {
		t.Errorf("expected 3 domain state metrics, got %d", count)
	}
}

func TestDomainListError(t *testing.T) {
	client := &mockLibvirtClient{
		available:  true,
		hostCPUs:   4,
		hostMem:    8589934592,
		domainsErr: fmt.Errorf("libvirt connection reset"),
	}

	c := NewWithClient(client)
	metrics := make(chan prometheus.Metric, 20)
	c.Collect(metrics)
	close(metrics)

	// Should have libvirt_up=1, host metrics, and an invalid metric for domain list error.
	count := 0
	for range metrics {
		count++
	}
	if count < 2 {
		t.Errorf("expected at least 2 metrics on domain list error, got %d", count)
	}
}

func TestHostMetricErrors(t *testing.T) {
	client := &mockLibvirtClient{
		available:      true,
		hostCPUsErr:    fmt.Errorf("cpu error"),
		hostMemErr:     fmt.Errorf("mem error"),
		hostFreeMemErr: fmt.Errorf("free mem error"),
		domains:        []DomainInfo{},
	}

	c := NewWithClient(client)

	// Should still emit libvirt_up=1 and domains_total=0 despite host metric errors.
	expected := `
# HELP libvirt_up Whether libvirtd is reachable (1 = up, 0 = down).
# TYPE libvirt_up gauge
libvirt_up 1
# HELP libvirt_domains_total Total number of defined domains.
# TYPE libvirt_domains_total gauge
libvirt_domains_total 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"libvirt_up", "libvirt_domains_total",
	); err != nil {
		t.Error(err)
	}

	// Host metrics should not be present.
	count := testutil.CollectAndCount(c, "libvirt_host_cpu_count", "libvirt_host_memory_bytes", "libvirt_host_free_memory_bytes")
	if count != 0 {
		t.Errorf("expected 0 host metrics when all fail, got %d", count)
	}
}

func TestMemoryStatsError(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    4,
		hostMem:     8589934592,
		hostFreeMem: 4294967296,
		domains: []DomainInfo{
			{
				Name:      "test-vm",
				UUID:      "uuid-test",
				State:     1,
				MaxMemory: 4294967296,
				Memory:    2147483648,
				NrVirtCPU: 2,
				CPUTime:   0,
			},
		},
		memStatsErr: map[string]error{
			"uuid-test": fmt.Errorf("domain not running"),
		},
	}

	c := NewWithClient(client)

	// Memory stats should be absent, but domain basic metrics should be present.
	count := testutil.CollectAndCount(c, "libvirt_domain_memory_stats_bytes")
	if count != 0 {
		t.Errorf("expected 0 memory stat metrics when error, got %d", count)
	}

	count = testutil.CollectAndCount(c, "libvirt_domain_info_state")
	if count != 1 {
		t.Errorf("expected 1 domain state metric, got %d", count)
	}
}

func TestBlockStatsError(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    4,
		hostMem:     8589934592,
		hostFreeMem: 4294967296,
		domains: []DomainInfo{
			{
				Name:      "test-vm",
				UUID:      "uuid-test",
				State:     1,
				MaxMemory: 4294967296,
				Memory:    2147483648,
				NrVirtCPU: 2,
				CPUTime:   0,
			},
		},
		blockStatsErr: map[string]error{
			"uuid-test": fmt.Errorf("block stats unavailable"),
		},
	}

	c := NewWithClient(client)

	count := testutil.CollectAndCount(c,
		"libvirt_domain_block_read_bytes_total",
		"libvirt_domain_block_write_bytes_total",
	)
	if count != 0 {
		t.Errorf("expected 0 block stat metrics when error, got %d", count)
	}
}

func TestInterfaceStatsError(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    4,
		hostMem:     8589934592,
		hostFreeMem: 4294967296,
		domains: []DomainInfo{
			{
				Name:      "test-vm",
				UUID:      "uuid-test",
				State:     1,
				MaxMemory: 4294967296,
				Memory:    2147483648,
				NrVirtCPU: 2,
				CPUTime:   0,
			},
		},
		ifaceStatsErr: map[string]error{
			"uuid-test": fmt.Errorf("interface stats unavailable"),
		},
	}

	c := NewWithClient(client)

	count := testutil.CollectAndCount(c,
		"libvirt_domain_net_receive_bytes_total",
		"libvirt_domain_net_transmit_bytes_total",
	)
	if count != 0 {
		t.Errorf("expected 0 interface stat metrics when error, got %d", count)
	}
}

func TestNewConstructor(t *testing.T) {
	c := New("qemu:///system")
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.Name() != "libvirt" {
		t.Errorf("expected name 'libvirt', got %q", c.Name())
	}
}

func TestMemoryStatTagNames(t *testing.T) {
	tests := []struct {
		tag  int32
		name string
	}{
		{0, "swap_in"},
		{1, "swap_out"},
		{2, "major_fault"},
		{3, "minor_fault"},
		{4, "unused"},
		{5, "available"},
		{6, "actual"},
		{7, "rss"},
		{8, "usable"},
		{9, "last_update"},
		{10, "disk_caches"},
	}

	for _, tt := range tests {
		got, ok := memoryStatTagNames[tt.tag]
		if !ok {
			t.Errorf("tag %d not found in memoryStatTagNames", tt.tag)
			continue
		}
		if got != tt.name {
			t.Errorf("memoryStatTagNames[%d] = %q, want %q", tt.tag, got, tt.name)
		}
	}
}

func TestUnknownMemoryStatTag(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    4,
		hostMem:     8589934592,
		hostFreeMem: 4294967296,
		domains: []DomainInfo{
			{
				Name:      "test-vm",
				UUID:      "uuid-test",
				State:     1,
				MaxMemory: 4294967296,
				Memory:    2147483648,
				NrVirtCPU: 2,
				CPUTime:   0,
			},
		},
		memStats: map[string][]DomainMemoryStat{
			"uuid-test": {
				{Tag: 99, Val: 12345},
			},
		},
	}

	c := NewWithClient(client)

	// Should produce a metric with stat="unknown_99".
	count := testutil.CollectAndCount(c, "libvirt_domain_memory_stats_bytes")
	if count != 1 {
		t.Errorf("expected 1 memory stat metric for unknown tag, got %d", count)
	}
}

func TestFullCollection(t *testing.T) {
	client := &mockLibvirtClient{
		available:   true,
		hostCPUs:    8,
		hostMem:     34359738368,
		hostFreeMem: 17179869184,
		domains: []DomainInfo{
			{
				Name:      "prod-web",
				UUID:      "uuid-prod-web",
				State:     1,
				MaxMemory: 8589934592,
				Memory:    4294967296,
				NrVirtCPU: 4,
				CPUTime:   999000000000,
			},
		},
		memStats: map[string][]DomainMemoryStat{
			"uuid-prod-web": {
				{Tag: 6, Val: 4194304},
				{Tag: 7, Val: 4300800},
			},
		},
		blockStats: map[string][]DomainBlockStats{
			"uuid-prod-web": {
				{Device: "vda", RdBytes: 10485760, WrBytes: 20971520, RdReq: 10000, WrReq: 20000},
			},
		},
		ifaceStats: map[string][]DomainInterfaceStats{
			"uuid-prod-web": {
				{Name: "vnet0", RxBytes: 104857600, TxBytes: 52428800, RxPackets: 100000, TxPackets: 50000, RxErrs: 0, TxErrs: 0},
			},
		},
	}

	c := NewWithClient(client)

	// Verify total metric count: 1 (up) + 3 (host) + 1 (domains_total)
	// + 5 (domain basic) + 2 (mem stats) + 4 (block stats) + 6 (iface stats) = 22
	count := testutil.CollectAndCount(c)
	if count != 22 {
		t.Errorf("expected 22 total metrics, got %d", count)
	}
}
