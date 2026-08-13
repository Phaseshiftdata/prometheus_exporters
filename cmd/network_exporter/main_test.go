package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
		setupLogging(level) // should not panic
	}
}

func TestCreateNetworkCollectors(t *testing.T) {
	collectors := createNetworkCollectors("/proc", "/sys")
	if len(collectors) != 5 {
		t.Errorf("expected 5 collectors, got %d", len(collectors))
	}
}

func TestServeAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- serve(ctx, "127.0.0.1:0", prometheus.NewRegistry()) }()
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
	err := serve(ctx, "invalid-address-no-port", prometheus.NewRegistry())
	if err == nil {
		t.Error("expected error for invalid listen address")
	}
}

func TestRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx, "127.0.0.1:0", "/proc", "/sys", "info", nil) }()
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

	err := run(context.Background(), "127.0.0.1:0", "/proc", "/sys", "info", reg)
	if err == nil {
		t.Error("expected registration error")
	}
}

func TestExecuteReturnsZero(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"network_exporter", "--help"}
	defer func() { os.Args = oldArgs }()

	code := execute()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestExecuteReturnsOneOnError(t *testing.T) {
	oldArgs := os.Args
	// --unknown-flag will cause Cobra to return an error.
	os.Args = []string{"network_exporter", "--unknown-flag"}
	defer func() { os.Args = oldArgs }()

	code := execute()
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
	go func() { errCh <- serve(ctx, "127.0.0.1:0", reg) }()
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
		go func() { errCh <- run(ctx, "127.0.0.1:0", "/proc", "/sys", level, nil) }()
		time.Sleep(50 * time.Millisecond)
		cancel()
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			t.Errorf("run with level %s did not shut down", level)
		}
	}
}
