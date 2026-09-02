// Package exporter provides shared utilities for all Prometheus exporters
// in this repository: logging setup, HTTP serving with graceful shutdown,
// main-function boilerplate, and version formatting.
package exporter

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
		escaped := html.EscapeString(exporterName)
		fmt.Fprintf(w, `<html><head><title>%s</title></head>
<body><h1>%s</h1><p><a href="/metrics">Metrics</a></p></body></html>`, escaped, escaped)
	})

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
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

// ValidatePrometheusText parses text as Prometheus exposition format and
// returns an error if the text is malformed. This should be used to validate
// any externally-sourced metric text before writing it to an HTTP response,
// preventing metric injection from compromised upstream sources.
// ValidatePrometheusText checks that text is syntactically valid Prometheus
// exposition format. Each line must be a comment (# HELP, # TYPE), a blank
// line, or a metric line matching "name{labels} value [timestamp]".
// This is a line-level structural check that prevents injection of arbitrary
// content without depending on the expfmt parser's global state.
func ValidatePrometheusText(text string) error {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Comment lines (# HELP, # TYPE, # EOF) are safe.
		if strings.HasPrefix(line, "#") {
			continue
		}
		// Metric lines must start with a valid metric name character (a-zA-Z_:).
		if len(line) > 0 {
			ch := line[0]
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' || ch == ':') {
				return fmt.Errorf("invalid metric line (bad first character %q): %s", string(ch), truncateLine(line))
			}
		}
	}
	return nil
}

func truncateLine(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

