// Package exporter provides shared utilities for all Prometheus exporters
// in this repository: logging setup, HTTP serving with graceful shutdown,
// main-function boilerplate, and version formatting.
package exporter

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

	"github.com/phaseshiftdata/prometheus_exporters/src/version"
)

// SetupLogging configures the default slog logger with the given level string.
// Accepted values are "debug", "info", "warn", and "error". Any other value
// defaults to info.
func SetupLogging(level string) {
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

// Serve starts an HTTP server that exposes Prometheus metrics at /metrics and
// a landing page at /. It blocks until the context is canceled or a SIGINT /
// SIGTERM is received, then performs a graceful shutdown.
//
// The /metrics handler uses promhttp.ContinueOnError so that one failing
// collector does not blank the entire response.
func Serve(ctx context.Context, listenAddr, exporterName string, reg *prometheus.Registry) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
		ErrorLog:      slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><title>%s</title></head>
<body><h1>%s</h1><p><a href="/metrics">Metrics</a></p></body></html>`, exporterName, exporterName)
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

	slog.Info("starting "+exporterName, "address", listenAddr, "version", version.Version)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}

// Execute runs a cobra root command and returns 0 on success or 1 on error.
// It is intended to be called from main() as os.Exit(exporter.Execute(rootCmd)).
func Execute(rootCmd func() *cobra.Command) int {
	if err := rootCmd().Execute(); err != nil {
		return 1
	}
	return 0
}

// VersionString returns a formatted version string containing the semantic
// version, git commit, and build date.
func VersionString() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", version.Version, version.GitCommit, version.BuildDate)
}
