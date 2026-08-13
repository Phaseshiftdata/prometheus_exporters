// ipsec_exporter is a Prometheus exporter for host network and IPsec metrics.
//
// It includes everything network_exporter provides, plus IPsec SA metrics
// and tunnel auto-discovery via the strongSwan VICI socket. It runs only
// on hosts that terminate IPsec tunnels (e.g. cocky-wiles).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/arp"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/conntrack"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/firewall"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/iface"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/ipsec"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/netgraph"
	"github.com/phaseshiftdata/prometheus_exporters/src/version"
)

func main() {
	os.Exit(execute())
}

func execute() int {
	if err := rootCmd().Execute(); err != nil {
		return 1
	}
	return 0
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
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version.Version, version.GitCommit, version.BuildDate),
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
	setupLogging(logLevel)

	if reg == nil {
		reg = prometheus.NewRegistry()
	}

	for _, c := range createAllCollectors(procPath, sysPath, viciSocket) {
		if err := reg.Register(c); err != nil {
			return fmt.Errorf("registering collector %s: %w", c.Name(), err)
		}
		slog.Info("registered collector", "name", c.Name())
	}

	return serve(ctx, listenAddr, reg)
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

func serve(ctx context.Context, listenAddr string, reg *prometheus.Registry) error {
	mux := http.NewServeMux()
	// ContinueOnError for the same reason as network_exporter, which registers
	// the same collectors: the default discards every metric family when any
	// one collector cannot reach its data source, and a host is far more likely
	// to be missing one facility than all six.
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
		ErrorLog:      slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><title>IPsec Exporter</title></head>
<body><h1>IPsec Exporter</h1><p><a href="/metrics">Metrics</a></p></body></html>`)
	})

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown error", "error", err)
		}
	}()

	slog.Info("starting ipsec_exporter", "address", listenAddr, "version", version.Version)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}

func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}
