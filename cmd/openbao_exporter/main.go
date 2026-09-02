// openbao_exporter is a Prometheus exporter for OpenBao cluster metrics.
//
// It connects to an OpenBao seed node, collects health and native metrics,
// discovers cluster members via raft configuration, and re-exposes
// everything at /metrics in standard Prometheus text format.
package main

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

	"github.com/phaseshiftdata/prometheus_exporters/src/collector"
	"github.com/phaseshiftdata/prometheus_exporters/src/collector/openbao"
	"github.com/phaseshiftdata/prometheus_exporters/src/exporter"
	"github.com/phaseshiftdata/prometheus_exporters/src/secrets"
	"github.com/phaseshiftdata/prometheus_exporters/src/version"
)

func main() {
	os.Exit(exporter.Execute(rootCmd))
}

func rootCmd() *cobra.Command {
	var (
		listenAddr   string
		openbaoAddr  string
		tokenFile    string
		logLevel     string
		pollInterval time.Duration
	)

	cmd := &cobra.Command{
		Use:     "openbao_exporter",
		Short:   "Prometheus exporter for OpenBao cluster metrics",
		Version: exporter.VersionString(),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if openbaoAddr == "" {
				return fmt.Errorf("--openbao-addr is required")
			}
			// Basic URL validation.
			if !strings.HasPrefix(openbaoAddr, "http://") && !strings.HasPrefix(openbaoAddr, "https://") {
				return fmt.Errorf("--openbao-addr must start with http:// or https://, got %q", openbaoAddr)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), config{
				listenAddr:   listenAddr,
				openbaoAddr:  openbaoAddr,
				tokenFile:    tokenFile,
				logLevel:     logLevel,
				pollInterval: pollInterval,
			})
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen-address", "127.0.0.1:9100", "Address to listen on for metrics")
	cmd.Flags().StringVar(&openbaoAddr, "openbao-addr", "", "OpenBao API address (required, e.g., https://openbao:8200)")
	cmd.Flags().StringVar(&tokenFile, "openbao-token-file", "", "Path to file containing OpenBao token (optional, enables cluster discovery)")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 30*time.Second, "How often to re-discover cluster members")

	return cmd
}

type config struct {
	listenAddr   string
	openbaoAddr  string
	tokenFile    string
	logLevel     string
	pollInterval time.Duration
}

func run(ctx context.Context, cfg config, regOverride ...*prometheus.Registry) error {
	exporter.SetupLogging(cfg.logLevel)

	// Resolve token: file takes precedence over env var. The token is
	// never accepted as a CLI flag to avoid /proc/cmdline exposure.
	// ReadSecretFile validates symlinks and file permissions (must be
	// 0600 or stricter).
	var token string
	if cfg.tokenFile != "" {
		var err error
		token, err = secrets.ReadSecretFile(cfg.tokenFile)
		if err != nil {
			return fmt.Errorf("--openbao-token-file: %w", err)
		}
	} else if envToken := os.Getenv("OPENBAO_TOKEN"); envToken != "" {
		token = envToken
	}

	client := openbao.NewClient(cfg.openbaoAddr, token)
	defer client.ZeroToken()

	coll := openbao.New(client, cfg.pollInterval)

	var reg *prometheus.Registry
	if len(regOverride) > 0 && regOverride[0] != nil {
		reg = regOverride[0]
	} else {
		reg = prometheus.NewRegistry()
	}

	if err := reg.Register(coll); err != nil {
		return fmt.Errorf("registering collector %s: %w", coll.Name(), err)
	}
	slog.Info("registered collector", "name", coll.Name())

	return serve(ctx, cfg.listenAddr, "OpenBao Exporter", reg, coll)
}

// serve starts an HTTP server that exposes combined Prometheus metrics at
// /metrics. It appends native OpenBao metrics (raw text from
// /v1/sys/metrics?format=prometheus) after the registry-based metrics.
func serve(ctx context.Context, listenAddr, exporterName string, reg *prometheus.Registry, coll *openbao.Collector) error {
	baseHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorHandling:       promhttp.ContinueOnError,
		ErrorLog:            slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		DisableCompression:  true,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// Serve registry metrics first via an internal recorder so we can
		// append native OpenBao metrics afterwards.
		rec := &responseRecorder{header: make(http.Header)}
		baseHandler.ServeHTTP(rec, r)

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(rec.body)

		// Append native OpenBao metrics after validating they are
		// well-formed Prometheus exposition format. This prevents a
		// compromised OpenBao instance from injecting arbitrary metrics.
		native := coll.NativeMetrics()
		if native != "" {
			if err := exporter.ValidatePrometheusText(native); err != nil {
				slog.Warn("native OpenBao metrics failed validation; skipping", "error", err)
			} else {
				if len(rec.body) > 0 && rec.body[len(rec.body)-1] != '\n' {
					w.Write([]byte("\n"))
				}
				w.Write([]byte(native))
			}
		}
	})
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

// responseRecorder captures the response from an http.Handler.
type responseRecorder struct {
	header http.Header
	body   []byte
	status int
}

func (r *responseRecorder) Header() http.Header       { return r.header }
func (r *responseRecorder) WriteHeader(code int)       { r.status = code }
func (r *responseRecorder) Write(b []byte) (int, error) { r.body = append(r.body, b...); return len(b), nil }

// createAllCollectors returns a slice of collectors for use by the exporter.
// This follows the pattern used by other exporters in this repository.
func createAllCollectors(openbaoAddr, token string, pollInterval time.Duration) []collector.Collector {
	client := openbao.NewClient(openbaoAddr, token)
	return []collector.Collector{
		openbao.New(client, pollInterval),
	}
}
