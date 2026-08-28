// libvirt_exporter is a Prometheus exporter for libvirt hypervisor and VM metrics.
//
// It connects to libvirtd via the libvirt API and exposes hypervisor-level
// metrics (CPU count, memory) as well as per-domain metrics (state, CPU time,
// memory stats, block I/O, and network I/O).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/libvirt"
	"github.com/phaseshiftdata/prometheus_exporters/src/exporter"
)

// allowedTransports lists the libvirt URI transport suffixes that target
// the local hypervisor only.  Remote transports like +ssh, +tcp, +tls
// are rejected.
var allowedTransports = map[string]bool{
	"":      true, // e.g. qemu:///system
	"+unix": true, // e.g. qemu+unix:///system
}

// validateLocalURI ensures the libvirt URI targets only the local
// hypervisor.  It rejects URIs that contain a hostname or use a remote
// transport (ssh, tcp, tls).
func validateLocalURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid libvirt URI %q: %w", raw, err)
	}

	// The scheme is "driver+transport" (e.g. "qemu+ssh", "qemu+unix", "qemu").
	// Extract the transport suffix after the first "+".
	scheme := u.Scheme
	transport := ""
	if idx := strings.Index(scheme, "+"); idx >= 0 {
		transport = scheme[idx:]
	}

	if !allowedTransports[transport] {
		return fmt.Errorf("libvirt URI %q uses remote transport %q; only local connections are allowed", raw, transport)
	}

	// A local URI has an empty authority: qemu:///system (three slashes).
	// A remote URI has a hostname: qemu+ssh://host/system.
	if u.Host != "" {
		return fmt.Errorf("libvirt URI %q contains hostname %q; only local connections are allowed", raw, u.Host)
	}

	return nil
}

func main() {
	os.Exit(exporter.Execute(rootCmd))
}

func rootCmd() *cobra.Command {
	var listenAddr string
	var libvirtURI string
	var logLevel string

	cmd := &cobra.Command{
		Use:     "libvirt_exporter",
		Short:   "Prometheus exporter for libvirt hypervisor and VM metrics",
		Version: exporter.VersionString(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), listenAddr, libvirtURI, logLevel, nil)
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen-address", "127.0.0.1:9177", "Address to listen on for metrics")
	cmd.Flags().StringVar(&libvirtURI, "libvirt-uri", "qemu:///system", "Libvirt connection URI")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	return cmd
}

func run(ctx context.Context, listenAddr, libvirtURI, logLevel string, reg *prometheus.Registry) error {
	exporter.SetupLogging(logLevel)

	if err := validateLocalURI(libvirtURI); err != nil {
		return err
	}

	if reg == nil {
		reg = prometheus.NewRegistry()
	}

	c := createCollector(libvirtURI)
	if err := reg.Register(c); err != nil {
		return fmt.Errorf("registering collector %s: %w", c.Name(), err)
	}
	slog.Info("registered collector", "name", c.Name())

	return exporter.Serve(ctx, listenAddr, "Libvirt Exporter", reg)
}

func createCollector(libvirtURI string) collector.Collector {
	return libvirt.New(libvirtURI)
}
