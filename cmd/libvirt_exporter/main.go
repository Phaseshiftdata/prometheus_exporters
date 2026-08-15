// libvirt_exporter is a Prometheus exporter for libvirt hypervisor and VM metrics.
//
// It connects to libvirtd via the libvirt API and exposes hypervisor-level
// metrics (CPU count, memory) as well as per-domain metrics (state, CPU time,
// memory stats, block I/O, and network I/O).
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
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/libvirt"
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
	var libvirtURI string
	var logLevel string

	cmd := &cobra.Command{
		Use:     "libvirt_exporter",
		Short:   "Prometheus exporter for libvirt hypervisor and VM metrics",
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version.Version, version.GitCommit, version.BuildDate),
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
	setupLogging(logLevel)

	if reg == nil {
		reg = prometheus.NewRegistry()
	}

	c := createCollector(libvirtURI)
	if err := reg.Register(c); err != nil {
		return fmt.Errorf("registering collector %s: %w", c.Name(), err)
	}
	slog.Info("registered collector", "name", c.Name())

	return serve(ctx, listenAddr, reg)
}

func createCollector(libvirtURI string) collector.Collector {
	return libvirt.New(libvirtURI)
}

func serve(ctx context.Context, listenAddr string, reg *prometheus.Registry) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
		ErrorLog:      slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><title>Libvirt Exporter</title></head>
<body><h1>Libvirt Exporter</h1><p><a href="/metrics">Metrics</a></p></body></html>`)
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

	slog.Info("starting libvirt_exporter", "address", listenAddr, "version", version.Version)
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
