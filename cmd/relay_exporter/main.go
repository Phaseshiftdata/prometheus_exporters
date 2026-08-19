// relay_exporter is a Prometheus metrics relay proxy.
//
// It accepts scrape requests at /metrics?ip=<target>&port=<num>&tls=<bool>,
// fetches /metrics from the RFC 1918 target, and returns the response with
// relay status metrics appended.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/exporter"
	"github.com/phaseshiftdata/prometheus_exporters/src/version"
)

func main() {
	os.Exit(exporter.Execute(rootCmd))
}

func rootCmd() *cobra.Command {
	var (
		listenAddr        string
		allowedSource     string
		tlsCertFile       string
		tlsKeyFile        string
		caCert            string
		tlsSkipVerify     bool
		proxyTimeout      time.Duration
		concurrentReqs    int
		logLevel          string
	)

	cmd := &cobra.Command{
		Use:     "relay_exporter",
		Short:   "Prometheus metrics relay proxy for RFC 1918 targets",
		Version: exporter.VersionString(),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if allowedSource == "" {
				return fmt.Errorf("--allowed-source is required")
			}
			if net.ParseIP(allowedSource) == nil {
				return fmt.Errorf("--allowed-source must be a valid IP address, got %q", allowedSource)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), config{
				listenAddr:     listenAddr,
				allowedSource:  allowedSource,
				tlsCertFile:    tlsCertFile,
				tlsKeyFile:     tlsKeyFile,
				caCert:         caCert,
				tlsSkipVerify:  tlsSkipVerify,
				proxyTimeout:   proxyTimeout,
				concurrentReqs: concurrentReqs,
				logLevel:       logLevel,
			})
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen-address", "127.0.0.1:9100", "Address to listen on for metrics")
	cmd.Flags().StringVar(&allowedSource, "allowed-source", "", "IP address allowed to scrape (required)")
	cmd.Flags().StringVar(&tlsCertFile, "tls-cert-file", "", "TLS certificate file for listener")
	cmd.Flags().StringVar(&tlsKeyFile, "tls-key-file", "", "TLS key file for listener")
	cmd.Flags().StringVar(&caCert, "ca-cert", "", "CA certificate for verifying target TLS")
	cmd.Flags().BoolVar(&tlsSkipVerify, "tls-skip-verify", false, "Skip TLS verification for target connections")
	cmd.Flags().DurationVar(&proxyTimeout, "proxy-timeout", 10*time.Second, "Timeout for proxied requests")
	cmd.Flags().IntVar(&concurrentReqs, "concurrent-requests", 100, "Maximum concurrent proxy requests")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	return cmd
}

type config struct {
	listenAddr     string
	allowedSource  string
	tlsCertFile    string
	tlsKeyFile     string
	caCert         string
	tlsSkipVerify  bool
	proxyTimeout   time.Duration
	concurrentReqs int
	logLevel       string
}

func run(ctx context.Context, cfg config) error {
	exporter.SetupLogging(cfg.logLevel)

	tlsConfig, err := buildTargetTLSConfig(cfg.caCert, cfg.tlsSkipVerify)
	if err != nil {
		return fmt.Errorf("building TLS config: %w", err)
	}

	client := &http.Client{
		Timeout: cfg.proxyTimeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	sem := make(chan struct{}, cfg.concurrentReqs)

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", proxyHandler(cfg.allowedSource, client, sem, "/metrics"))
	mux.HandleFunc("/host", proxyHandler(cfg.allowedSource, client, sem, "/api/v0/component/prometheus.exporter.unix.host/metrics"))
	mux.HandleFunc("/cadvisor", proxyHandler(cfg.allowedSource, client, sem, "/api/v0/component/prometheus.exporter.cadvisor.containers/metrics"))
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/", landingHandler)

	srv := &http.Server{
		Addr:              cfg.listenAddr,
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

	slog.Info("starting relay_exporter", "address", cfg.listenAddr, "version", version.Version, "allowed_source", cfg.allowedSource)

	if cfg.tlsCertFile != "" && cfg.tlsKeyFile != "" {
		if err := srv.ListenAndServeTLS(cfg.tlsCertFile, cfg.tlsKeyFile); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen: %w", err)
		}
	} else {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen: %w", err)
		}
	}
	return nil
}

func buildTargetTLSConfig(caCert string, skipVerify bool) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: skipVerify, // #nosec G402
	}
	if caCert != "" {
		caCertData, err := os.ReadFile(caCert)
		if err != nil {
			return nil, fmt.Errorf("reading CA cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCertData) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
		tlsConfig.RootCAs = pool
	}
	return tlsConfig, nil
}

// rfc1918Nets are the private address ranges defined in RFC 1918.
var rfc1918Nets = []net.IPNet{
	{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
	{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
	{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
}

func isRFC1918(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}
	for _, n := range rfc1918Nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func extractSourceIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// proxyHandler returns an HTTP handler that validates the request, proxies it
// to targetPath on the specified RFC 1918 host, and appends relay status
// metrics. The targetPath is a compile-time constant per endpoint -- it is never
// derived from user input.
func proxyHandler(allowedSource string, client *http.Client, sem chan struct{}, targetPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check source IP.
		sourceIP := extractSourceIP(r.RemoteAddr)
		if sourceIP != allowedSource {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Check concurrency.
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		default:
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		// Parse query params.
		query := r.URL.Query()
		ipStr := query.Get("ip")
		portStr := query.Get("port")
		tlsStr := query.Get("tls")

		if ipStr == "" {
			http.Error(w, "missing required parameter: ip", http.StatusBadRequest)
			return
		}
		if portStr == "" {
			http.Error(w, "missing required parameter: port", http.StatusBadRequest)
			return
		}

		ip := net.ParseIP(ipStr)
		if ip == nil {
			http.Error(w, "invalid ip parameter", http.StatusBadRequest)
			return
		}
		if !isRFC1918(ip) {
			http.Error(w, "ip must be an RFC 1918 private address", http.StatusBadRequest)
			return
		}

		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			http.Error(w, "port must be between 1 and 65535", http.StatusBadRequest)
			return
		}

		useTLS := false
		if tlsStr != "" {
			useTLS, err = strconv.ParseBool(tlsStr)
			if err != nil {
				http.Error(w, "tls parameter must be true or false", http.StatusBadRequest)
				return
			}
		}

		// Build target URL. The IP is re-derived from the parsed
		// and RFC 1918-validated net.IP (not the raw query string),
		// and the path is a compile-time constant per endpoint.
		scheme := "http"
		if useTLS {
			scheme = "https"
		}
		target := &url.URL{
			Scheme: scheme,
			Host:   net.JoinHostPort(ip.String(), strconv.Itoa(port)),
			Path:   targetPath,
		}

		// Proxy the request.
		start := time.Now()
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
		if err != nil {
			writeRelayResponse(w, "", 0, 0, time.Since(start))
			return
		}

		// Forward Authorization header.
		if auth := r.Header.Get("Authorization"); auth != "" {
			req.Header.Set("Authorization", auth)
		}

		// Intentional proxy: target IP is validated as RFC 1918 only
		// (isRFC1918), source IP is filtered (--allowed-source), and
		// the target path is a compile-time constant per endpoint.
		resp, err := client.Do(req) // codeql[go/request-forgery]
		duration := time.Since(start)

		if err != nil {
			slog.Debug("proxy request failed", "target", target.String(), "error", err, "duration", duration)
			writeRelayResponse(w, "", 0, 0, duration)
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Debug("reading target response failed", "target", target.String(), "error", err, "duration", duration)
			writeRelayResponse(w, "", 0, 0, duration)
			return
		}

		slog.Debug("proxied request", "target", target.String(), "status", resp.StatusCode, "duration", duration)

		targetSuccess := 0
		if resp.StatusCode == http.StatusOK {
			targetSuccess = 1
		}
		writeRelayResponse(w, string(body), targetSuccess, resp.StatusCode, duration)
	}
}

// metricsHandler is kept for backward compatibility with tests.
// It delegates to proxyHandler with the /metrics path.
func metricsHandler(allowedSource string, client *http.Client, sem chan struct{}) http.HandlerFunc {
	return proxyHandler(allowedSource, client, sem, "/metrics")
}

func writeRelayResponse(w http.ResponseWriter, targetBody string, targetSuccess int, targetHTTPStatus int, duration time.Duration) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if targetBody != "" {
		fmt.Fprint(w, targetBody)
		if !strings.HasSuffix(targetBody, "\n") {
			fmt.Fprint(w, "\n")
		}
	}

	fmt.Fprintf(w, "# HELP relay_response Indicates the relay proxy is functioning (always 1).\n")
	fmt.Fprintf(w, "# TYPE relay_response gauge\n")
	fmt.Fprintf(w, "relay_response 1\n")
	fmt.Fprintf(w, "# HELP relay_target_response Indicates whether the target responded successfully (1=success, 0=failure).\n")
	fmt.Fprintf(w, "# TYPE relay_target_response gauge\n")
	fmt.Fprintf(w, "relay_target_response %d\n", targetSuccess)
	fmt.Fprintf(w, "# HELP relay_target_http_status The HTTP status code returned by the target (0 if unreachable).\n")
	fmt.Fprintf(w, "# TYPE relay_target_http_status gauge\n")
	fmt.Fprintf(w, "relay_target_http_status %d\n", targetHTTPStatus)
	fmt.Fprintf(w, "# HELP relay_duration_seconds Time taken to proxy the request in seconds.\n")
	fmt.Fprintf(w, "# TYPE relay_duration_seconds gauge\n")
	fmt.Fprintf(w, "relay_duration_seconds %f\n", duration.Seconds())
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok\n")
}

func landingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<html><head><title>Relay Exporter</title></head>
<body><h1>Relay Exporter</h1><p><a href="/metrics">Metrics</a></p><p><a href="/host">Host</a></p><p><a href="/cadvisor">cAdvisor</a></p><p><a href="/health">Health</a></p></body></html>`)
}
