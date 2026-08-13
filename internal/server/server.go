// Package server provides the HTTP server for the exporter.
package server

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/asymmetric-effort/prometheus-exporters/internal/discovery"
)

// Config holds server configuration.
type Config struct {
	ListenAddress     string
	MetricsPath       string
	TLSCertFile       string
	TLSKeyFile        string
	BasicAuthUsername  string
	BasicAuthPassword string
}

// Server is the HTTP server for the exporter.
type Server struct {
	httpServer *http.Server
	registry   *prometheus.Registry
	logger     *zap.Logger
	config     Config
	matrix     func() *discovery.CapabilityMatrix
	healthy    func() bool
}

// NewServer creates a new exporter HTTP server.
func NewServer(cfg Config, logger *zap.Logger) *Server {
	registry := prometheus.NewRegistry()

	// Register Go and process collectors explicitly
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return &Server{
		registry: registry,
		logger:   logger,
		config:   cfg,
		healthy:  func() bool { return true },
	}
}

// Registry returns the custom prometheus registry.
func (s *Server) Registry() *prometheus.Registry {
	return s.registry
}

// SetCapabilityMatrixFunc sets the function that returns the current capability matrix.
func (s *Server) SetCapabilityMatrixFunc(f func() *discovery.CapabilityMatrix) {
	s.matrix = f
}

// SetHealthFunc sets the function that determines health status.
func (s *Server) SetHealthFunc(f func() bool) {
	s.healthy = f
}

// Start begins serving HTTP requests.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	metricsHandler := promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})

	mux.Handle(s.config.MetricsPath, s.maybeAuth(metricsHandler))
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/capabilities", s.handleCapabilities)
	mux.HandleFunc("/", s.handleRoot)

	s.httpServer = &http.Server{
		Addr:              s.config.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	s.logger.Info("starting HTTP server",
		zap.String("address", s.config.ListenAddress),
		zap.String("metrics_path", s.config.MetricsPath),
	)

	if s.config.TLSCertFile != "" && s.config.TLSKeyFile != "" {
		s.httpServer.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		return s.httpServer.ListenAndServeTLS(s.config.TLSCertFile, s.config.TLSKeyFile)
	}

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down HTTP server")
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if s.healthy() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"healthy"}`)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"status":"unhealthy"}`)
	}
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.matrix == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"discovery not yet complete"}`)
		return
	}

	matrix := s.matrix()
	if matrix == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"discovery not yet complete"}`)
		return
	}

	data, err := matrix.ToJSON()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"failed to serialize capabilities: %s"}`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Cloudflare Exporter</title></head>
<body>
<h1>Cloudflare Exporter</h1>
<p><a href="%s">Metrics</a></p>
<p><a href="/health">Health</a></p>
<p><a href="/capabilities">Capabilities</a></p>
</body>
</html>`, s.config.MetricsPath)
}

func (s *Server) maybeAuth(next http.Handler) http.Handler {
	if s.config.BasicAuthUsername == "" && s.config.BasicAuthPassword == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(s.config.BasicAuthUsername)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(s.config.BasicAuthPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="cloudflare_exporter"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// MarshalCapabilityMatrix serializes the matrix for --capabilities output.
func MarshalCapabilityMatrix(matrix *discovery.CapabilityMatrix) ([]byte, error) {
	return json.MarshalIndent(matrix, "", "  ")
}
