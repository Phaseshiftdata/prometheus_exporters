package exporter

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/version"
)

// ---------- SetupLogging ----------

func TestSetupLogging(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "invalid"} {
		SetupLogging(level) // must not panic
	}
}

// ---------- Serve ----------

func TestServeStartupAndShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Serve(ctx, addr, "Test Exporter", prometheus.NewRegistry()) }()

	// Wait for the server to become ready.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Hit the landing page.
	resp, httpErr := http.Get("http://" + addr + "/")
	if httpErr != nil {
		t.Fatalf("GET /: %v", httpErr)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Test Exporter") {
		t.Error("landing page does not contain exporter name")
	}

	// Hit /metrics.
	resp, httpErr = http.Get("http://" + addr + "/metrics")
	if httpErr != nil {
		t.Fatalf("GET /metrics: %v", httpErr)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /metrics status = %d, want 200", resp.StatusCode)
	}

	// Shutdown.
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Serve did not shut down in time")
	}
}

func TestServeInvalidAddress(t *testing.T) {
	err := Serve(context.Background(), "invalid-address-no-port", "Test", prometheus.NewRegistry())
	if err == nil {
		t.Error("expected error for invalid listen address")
	}
}

// ---------- Execute ----------

func TestExecuteSuccess(t *testing.T) {
	makeCmd := func() *cobra.Command {
		return &cobra.Command{
			Use: "test",
			RunE: func(cmd *cobra.Command, args []string) error {
				return nil
			},
		}
	}
	code := Execute(makeCmd)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestExecuteFailure(t *testing.T) {
	makeCmd := func() *cobra.Command {
		cmd := &cobra.Command{
			Use: "test",
			RunE: func(cmd *cobra.Command, args []string) error {
				return nil
			},
		}
		cmd.SetArgs([]string{"--unknown-flag"})
		return cmd
	}
	code := Execute(makeCmd)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

// ---------- VersionString ----------

func TestVersionString(t *testing.T) {
	vs := VersionString()
	if !strings.Contains(vs, version.Version) {
		t.Errorf("VersionString() = %q, does not contain Version %q", vs, version.Version)
	}
	if !strings.Contains(vs, version.GitCommit) {
		t.Errorf("VersionString() = %q, does not contain GitCommit %q", vs, version.GitCommit)
	}
	if !strings.Contains(vs, version.BuildDate) {
		t.Errorf("VersionString() = %q, does not contain BuildDate %q", vs, version.BuildDate)
	}
}
