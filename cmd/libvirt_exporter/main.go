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
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/libvirt"
	"github.com/phaseshiftdata/prometheus_exporters/src/exporter"
)

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
