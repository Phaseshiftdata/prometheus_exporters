package main

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector/arp"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/netgraph"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/tcpstate"
	"github.com/phaseshiftdata/prometheus_exporters/src/exporter"
)

func TestRootCmd(t *testing.T) {
	cmd := rootCmd()
	if cmd.Use != "network_exporter" {
		t.Errorf("expected Use 'network_exporter', got %q", cmd.Use)
	}
	if cmd.Version == "" {
		t.Error("empty version")
	}
}

func TestRootCmdExecute(t *testing.T) {
	cmd := rootCmd()
	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--listen-address", "127.0.0.1:0"})

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("cmd.Execute returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("cmd did not shut down in time")
	}
}

func TestSetupLogging(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "invalid"} {
		exporter.SetupLogging(level) // should not panic
	}
}

func TestCreateNetworkCollectors(t *testing.T) {
	collectors := createNetworkCollectors("/proc", "/sys", arp.DefaultMaxEntries, netgraph.DefaultMaxEdges, tcpstate.DefaultMaxConnections, "")
	if len(collectors) != 6 {
		t.Errorf("expected 6 collectors, got %d", len(collectors))
	}
}

func TestServeAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- exporter.Serve(ctx, "127.0.0.1:0", "Network Exporter", prometheus.NewRegistry()) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("serve did not shut down in time")
	}
}

func TestServeInvalidAddress(t *testing.T) {
	ctx := context.Background()
	err := exporter.Serve(ctx, "invalid-address-no-port", "Network Exporter", prometheus.NewRegistry())
	if err == nil {
		t.Error("expected error for invalid listen address")
	}
}

func TestRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx, "127.0.0.1:0", "/proc", "/sys", "info", arp.DefaultMaxEntries, netgraph.DefaultMaxEdges, tcpstate.DefaultMaxConnections, "", nil) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("run did not shut down in time")
	}
}

func TestRunRegistrationError(t *testing.T) {
	// Pre-register a collector that conflicts with the arp collector's metric name.
	reg := prometheus.NewRegistry()
	conflicting := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "network_arp_entry",
		Help: "conflict",
	}, []string{"ip", "mac", "device", "state"})
	reg.MustRegister(conflicting)

	err := run(context.Background(), "127.0.0.1:0", "/proc", "/sys", "info", arp.DefaultMaxEntries, netgraph.DefaultMaxEdges, tcpstate.DefaultMaxConnections, "", reg)
	if err == nil {
		t.Error("expected registration error")
	}
}

func TestExecuteReturnsZero(t *testing.T) {
	code := exporter.Execute(func() *cobra.Command {
		cmd := rootCmd()
		cmd.SetArgs([]string{"--help"})
		return cmd
	})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestExecuteReturnsOneOnError(t *testing.T) {
	code := exporter.Execute(func() *cobra.Command {
		cmd := rootCmd()
		cmd.SetArgs([]string{"--unknown-flag"})
		return cmd
	})
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestServeShutdownError(t *testing.T) {
	// Start a server, close its listener externally to force a shutdown error,
	// then cancel the context. The slog.Error path inside the goroutine fires.
	ctx, cancel := context.WithCancel(context.Background())
	reg := prometheus.NewRegistry()

	errCh := make(chan error, 1)
	go func() { errCh <- exporter.Serve(ctx, "127.0.0.1:0", "Network Exporter", reg) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Error("serve did not shut down in time")
	}
}

func TestRunAllLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "warn", "error"} {
		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() { errCh <- run(ctx, "127.0.0.1:0", "/proc", "/sys", level, arp.DefaultMaxEntries, netgraph.DefaultMaxEdges, tcpstate.DefaultMaxConnections, "", nil) }()
		time.Sleep(50 * time.Millisecond)
		cancel()
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			t.Errorf("run with level %s did not shut down", level)
		}
	}
}
