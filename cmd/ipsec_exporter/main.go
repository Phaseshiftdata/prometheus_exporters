// ipsec_exporter is a Prometheus exporter for host network and IPsec metrics.
//
// It includes everything network_exporter provides, plus IPsec SA metrics
// and tunnel auto-discovery via the strongSwan VICI socket. It runs only
// on hosts that terminate IPsec tunnels (e.g. cocky-wiles).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/arp"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/conntrack"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/firewall"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/iface"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/ipsec"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/netgraph"
	"github.com/phaseshiftdata/prometheus_exporters/src/exporter"
)

func main() {
	os.Exit(exporter.Execute(rootCmd))
}

func rootCmd() *cobra.Command {
	var listenAddr string
	var procPath string
	var sysPath string
	var viciSocket string
	var logLevel string

	cmd := &cobra.Command{
		Use:     "ipsec_exporter",
		Short:   "Prometheus exporter for host network and IPsec metrics",
		Version: exporter.VersionString(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), listenAddr, procPath, sysPath, viciSocket, logLevel, nil)
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen-address", "127.0.0.1:9100", "Address to listen on for metrics")
	cmd.Flags().StringVar(&procPath, "proc-path", "/proc", "Path to procfs mount")
	cmd.Flags().StringVar(&sysPath, "sys-path", "/sys", "Path to sysfs mount")
	cmd.Flags().StringVar(&viciSocket, "vici-socket", "/var/run/charon.vici", "Path to strongSwan VICI socket")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	return cmd
}

func run(ctx context.Context, listenAddr, procPath, sysPath, viciSocket, logLevel string, reg *prometheus.Registry) error {
	exporter.SetupLogging(logLevel)

	if reg == nil {
		reg = prometheus.NewRegistry()
	}

	for _, c := range createAllCollectors(procPath, sysPath, viciSocket) {
		if err := reg.Register(c); err != nil {
			return fmt.Errorf("registering collector %s: %w", c.Name(), err)
		}
		slog.Info("registered collector", "name", c.Name())
	}

	return exporter.Serve(ctx, listenAddr, "IPsec Exporter", reg)
}

func createAllCollectors(procPath, sysPath, viciSocket string) []collector.Collector {
	return []collector.Collector{
		arp.New(),
		iface.New(sysPath),
		netgraph.New(procPath),
		conntrack.New(procPath),
		firewall.New(),
		ipsec.New(viciSocket),
	}
}
