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
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/arp"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/conntrack"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/firewall"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/iface"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/ipsec"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/netgraph"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/tcpstate"
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
	var maxArpEntries int
	var maxGraphEdges int
	var maxTCPConns int
	var tcpConnStates string
	var hostNetNS string

	cmd := &cobra.Command{
		Use:     "ipsec_exporter",
		Short:   "Prometheus exporter for host network and IPsec metrics",
		Version: exporter.VersionString(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), listenAddr, procPath, sysPath, viciSocket, logLevel, maxArpEntries, maxGraphEdges, maxTCPConns, tcpConnStates, hostNetNS, nil)
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen-address", "127.0.0.1:9100", "Address to listen on for metrics")
	cmd.Flags().StringVar(&procPath, "proc-path", "/proc", "Path to procfs mount")
	cmd.Flags().StringVar(&sysPath, "sys-path", "/sys", "Path to sysfs mount")
	cmd.Flags().StringVar(&viciSocket, "vici-socket", "/var/run/charon.vici", "Path to strongSwan VICI socket")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	cmd.Flags().IntVar(&maxArpEntries, "max-arp-entries", arp.DefaultMaxEntries, "Maximum ARP entries to export (prevents cardinality explosion)")
	cmd.Flags().IntVar(&maxGraphEdges, "max-graph-edges", netgraph.DefaultMaxEdges, "Maximum network graph edges to export (prevents cardinality explosion)")
	cmd.Flags().IntVar(&maxTCPConns, "max-tcp-connections", tcpstate.DefaultMaxConnections, "Maximum TCP connections to export (prevents cardinality explosion)")
	cmd.Flags().StringVar(&tcpConnStates, "tcp-connection-states", "", "Comma-separated list of TCP states to report (default: all states)")
	cmd.Flags().StringVar(&hostNetNS, "host-netns", "", "Path to the host network namespace (e.g. /host/proc/1/ns/net) for reading nftables from inside a non-host-network container")

	return cmd
}

func run(ctx context.Context, listenAddr, procPath, sysPath, viciSocket, logLevel string, maxArpEntries, maxGraphEdges, maxTCPConns int, tcpConnStates, hostNetNS string, reg *prometheus.Registry) error {
	exporter.SetupLogging(logLevel)

	if reg == nil {
		reg = prometheus.NewRegistry()
	}

	for _, c := range createAllCollectors(procPath, sysPath, viciSocket, maxArpEntries, maxGraphEdges, maxTCPConns, tcpConnStates, hostNetNS) {
		if err := reg.Register(c); err != nil {
			return fmt.Errorf("registering collector %s: %w", c.Name(), err)
		}
		slog.Info("registered collector", "name", c.Name())
	}

	return exporter.Serve(ctx, listenAddr, "IPsec Exporter", reg)
}

func createAllCollectors(procPath, sysPath, viciSocket string, maxArpEntries, maxGraphEdges, maxTCPConns int, tcpConnStates, hostNetNS string) []collector.Collector {
	var states []string
	if tcpConnStates != "" {
		for _, s := range strings.Split(tcpConnStates, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				states = append(states, s)
			}
		}
	}
	return []collector.Collector{
		arp.NewWithMax(maxArpEntries),
		iface.New(sysPath),
		netgraph.NewWithMax(procPath, maxGraphEdges),
		conntrack.New(procPath),
		firewall.NewWithNetNS(hostNetNS),
		ipsec.New(viciSocket),
		tcpstate.NewWithMax(procPath, maxTCPConns, states),
	}
}
