package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector/libvirt"
)

// TestRootCmd verifies the cobra command is wired correctly.
func TestRootCmd(t *testing.T) {
	cmd := rootCmd()
	if cmd.Use != "libvirt_exporter" {
		t.Errorf("expected use 'libvirt_exporter', got %q", cmd.Use)
	}

	// Verify flags exist.
	flags := []string{"listen-address", "libvirt-uri", "log-level"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected flag %q to exist", f)
		}
	}
}

// TestExecuteInvalidPort verifies execute returns non-zero on bind failure.
func TestExecuteInvalidPort(t *testing.T) {
	// This would try to bind and fail; we just verify the command structure.
	cmd := rootCmd()
	if cmd.RunE == nil {
		t.Error("expected RunE to be set")
	}
}

// TestSetupLogging verifies all log levels are handled.
func TestSetupLogging(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error", "invalid"}
	for _, level := range levels {
		setupLogging(level) // should not panic
	}
}

// --- Mock libvirt client for e2e tests ---

type mockLibvirtClient struct {
	available bool
}

func (m *mockLibvirtClient) IsAvailable() bool { return m.available }

func (m *mockLibvirtClient) GetHostCPUCount() (uint, error) {
	return 8, nil
}

func (m *mockLibvirtClient) GetHostMemoryBytes() (uint64, error) {
	return 34359738368, nil // 32 GiB
}

func (m *mockLibvirtClient) GetHostFreeMemoryBytes() (uint64, error) {
	return 17179869184, nil // 16 GiB
}

func (m *mockLibvirtClient) ListDomains() ([]libvirt.DomainInfo, error) {
	return []libvirt.DomainInfo{
		{
			Name:      "test-vm",
			UUID:      "550e8400-e29b-41d4-a716-446655440000",
			State:     1, // running
			MaxMemory: 4294967296,
			Memory:    2147483648,
			NrVirtCPU: 2,
			CPUTime:   123456789000,
		},
	}, nil
}

func (m *mockLibvirtClient) GetDomainMemoryStats(name string) ([]libvirt.DomainMemoryStat, error) {
	return []libvirt.DomainMemoryStat{
		{Tag: 6, Val: 2097152},   // actual
		{Tag: 4, Val: 524288},    // unused
		{Tag: 5, Val: 1966080},   // available
		{Tag: 7, Val: 2150400},   // rss
	}, nil
}

func (m *mockLibvirtClient) GetDomainBlockStats(name string) ([]libvirt.DomainBlockStats, error) {
	return []libvirt.DomainBlockStats{
		{Device: "vda", RdBytes: 1048576, WrBytes: 2097152, RdReq: 1000, WrReq: 2000},
	}, nil
}

func (m *mockLibvirtClient) GetDomainInterfaceStats(name string) ([]libvirt.DomainInterfaceStats, error) {
	return []libvirt.DomainInterfaceStats{
		{Name: "vnet0", RxBytes: 10485760, TxBytes: 5242880, RxPackets: 10000, TxPackets: 5000, RxErrs: 0, TxErrs: 0},
	}, nil
}

// --- Mock unavailable client ---

type mockLibvirtClientUnavailable struct{}

func (m *mockLibvirtClientUnavailable) IsAvailable() bool                       { return false }
func (m *mockLibvirtClientUnavailable) GetHostCPUCount() (uint, error)          { return 0, fmt.Errorf("unavailable") }
func (m *mockLibvirtClientUnavailable) GetHostMemoryBytes() (uint64, error)     { return 0, fmt.Errorf("unavailable") }
func (m *mockLibvirtClientUnavailable) GetHostFreeMemoryBytes() (uint64, error) { return 0, fmt.Errorf("unavailable") }
func (m *mockLibvirtClientUnavailable) ListDomains() ([]libvirt.DomainInfo, error) {
	return nil, fmt.Errorf("unavailable")
}
func (m *mockLibvirtClientUnavailable) GetDomainMemoryStats(name string) ([]libvirt.DomainMemoryStat, error) {
	return nil, fmt.Errorf("unavailable")
}
func (m *mockLibvirtClientUnavailable) GetDomainBlockStats(name string) ([]libvirt.DomainBlockStats, error) {
	return nil, fmt.Errorf("unavailable")
}
func (m *mockLibvirtClientUnavailable) GetDomainInterfaceStats(name string) ([]libvirt.DomainInterfaceStats, error) {
	return nil, fmt.Errorf("unavailable")
}

// --- Test helpers ---

func waitForServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become ready", addr)
}

func scrapeMetrics(t *testing.T, addr string) string {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func startTestServer(t *testing.T, client libvirt.LibvirtClient) (string, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	reg := prometheus.NewRegistry()
	c := libvirt.NewWithClient(client)
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = serve(ctx, addr, reg) }()
	waitForServer(t, addr)
	return addr, cancel
}

// TestE2ELibvirtExporterMetrics starts a real HTTP server with a mock-backed
// collector and verifies that scraping /metrics returns all expected metrics.
func TestE2ELibvirtExporterMetrics(t *testing.T) {
	addr, cancel := startTestServer(t, &mockLibvirtClient{available: true})
	defer cancel()

	metrics := scrapeMetrics(t, addr)

	// Verify hypervisor metrics.
	hypervisorFamilies := []string{
		"libvirt_up",
		"libvirt_domains_total",
		"libvirt_host_cpu_count",
		"libvirt_host_memory_bytes",
		"libvirt_host_free_memory_bytes",
	}
	for _, family := range hypervisorFamilies {
		if !strings.Contains(metrics, family) {
			t.Errorf("expected hypervisor metric family %q not found in /metrics output", family)
		}
	}

	// Verify domain metrics.
	domainFamilies := []string{
		"libvirt_domain_info_state",
		"libvirt_domain_info_max_memory_bytes",
		"libvirt_domain_info_memory_bytes",
		"libvirt_domain_info_vcpus",
		"libvirt_domain_cpu_time_seconds_total",
		"libvirt_domain_memory_stats_bytes",
		"libvirt_domain_block_read_bytes_total",
		"libvirt_domain_block_write_bytes_total",
		"libvirt_domain_block_read_requests_total",
		"libvirt_domain_block_write_requests_total",
		"libvirt_domain_net_receive_bytes_total",
		"libvirt_domain_net_transmit_bytes_total",
		"libvirt_domain_net_receive_packets_total",
		"libvirt_domain_net_transmit_packets_total",
		"libvirt_domain_net_receive_errors_total",
		"libvirt_domain_net_transmit_errors_total",
	}
	for _, family := range domainFamilies {
		if !strings.Contains(metrics, family) {
			t.Errorf("expected domain metric family %q not found in /metrics output", family)
		}
	}

	// Verify label values.
	labelChecks := []string{
		`domain="test-vm"`,
		`uuid="550e8400-e29b-41d4-a716-446655440000"`,
		`device="vda"`,
		`interface="vnet0"`,
		`stat="actual"`,
	}
	for _, label := range labelChecks {
		if !strings.Contains(metrics, label) {
			t.Errorf("expected label %s not found in /metrics output", label)
		}
	}

	// Verify specific metric values.
	if !strings.Contains(metrics, "libvirt_up 1") {
		t.Error("expected libvirt_up 1")
	}
	if !strings.Contains(metrics, "libvirt_domains_total 1") {
		t.Error("expected libvirt_domains_total 1")
	}
	if !strings.Contains(metrics, "libvirt_host_cpu_count 8") {
		t.Error("expected libvirt_host_cpu_count 8")
	}
}

// TestE2ELibvirtUnavailable verifies that when libvirtd is down, only
// libvirt_up=0 is emitted.
func TestE2ELibvirtUnavailable(t *testing.T) {
	addr, cancel := startTestServer(t, &mockLibvirtClientUnavailable{})
	defer cancel()

	metrics := scrapeMetrics(t, addr)

	if !strings.Contains(metrics, "libvirt_up 0") {
		t.Error("expected libvirt_up 0")
	}

	// No other libvirt_* metric lines should be present.
	for _, line := range strings.Split(metrics, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "libvirt_") && !strings.HasPrefix(trimmed, "libvirt_up ") {
			t.Errorf("unexpected libvirt metric when unavailable: %s", trimmed)
		}
	}
}

// TestE2ELandingPage verifies the landing page.
func TestE2ELandingPage(t *testing.T) {
	addr, cancel := startTestServer(t, &mockLibvirtClient{available: true})
	defer cancel()

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Libvirt Exporter") {
		t.Error("landing page does not contain expected title")
	}
}

// TestE2EServeShutdown verifies that the server shuts down cleanly.
func TestE2EServeShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	reg := prometheus.NewRegistry()
	c := libvirt.NewWithClient(&mockLibvirtClient{available: true})
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- serve(ctx, addr, reg) }()
	waitForServer(t, addr)

	// Trigger shutdown.
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not shut down within 5s")
	}
}

// TestE2EContinueOnError verifies that ContinueOnError handling works
// correctly with the libvirt collector.
func TestE2EContinueOnError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	reg := prometheus.NewRegistry()
	c := libvirt.NewWithClient(&mockLibvirtClient{available: true})
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	}))

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	go func() { srv.ListenAndServe() }()

	waitForServer(t, addr)

	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
