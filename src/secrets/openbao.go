// Package secrets provides utilities for reading credentials from files
// and from OpenBao/Vault KV v2 stores.
package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// maxVaultResponseBytes caps how much of a Vault/OpenBao response is read.
	maxVaultResponseBytes = 1 << 20 // 1 MiB

	// maxVaultErrorBodyLog caps how much of a Vault error body is included
	// in error messages to prevent credential-like content from flooding logs.
	maxVaultErrorBodyLog = 512
)

// truncateVaultBody returns at most maxVaultErrorBodyLog bytes of s.
func truncateVaultBody(s string) string {
	if len(s) <= maxVaultErrorBodyLog {
		return s
	}
	return s[:maxVaultErrorBodyLog] + "...(truncated)"
}

// OpenBaoConfig holds the connection and authentication parameters for an
// OpenBao (or Vault) server.
type OpenBaoConfig struct {
	Address      string // e.g. "https://vault.example.com:8200"
	RoleIDFile   string // path to file containing the AppRole role_id
	SecretIDFile string // path to file containing the AppRole secret_id
}

// OpenBaoClient authenticates to an OpenBao/Vault server via AppRole,
// reads KV v2 secrets, and handles token renewal. It does not depend on
// the HashiCorp vault client library; all HTTP calls use net/http.
type OpenBaoClient struct {
	cfg    OpenBaoConfig
	client *http.Client
	logger *slog.Logger

	mu        sync.RWMutex
	token     string
	tokenTTL  time.Duration
	expiresAt time.Time

	cancel context.CancelFunc

	// Metrics
	authenticated    prometheus.Gauge
	readErrors       *prometheus.CounterVec
	lastReadSuccess  prometheus.Gauge
	tokenRenewals    prometheus.Counter
	tokenRenewErrors prometheus.Counter
}

// NewOpenBaoClient creates a client, authenticates via AppRole, and starts
// a background goroutine that renews the token before expiry.
//
// If the initial authentication fails because the vault is sealed or
// unreachable, the client is still returned. Background retry will keep
// attempting to authenticate, and ReadSecret calls will return an error
// until a token is obtained.
func NewOpenBaoClient(cfg OpenBaoConfig, reg prometheus.Registerer, logger *slog.Logger) (*OpenBaoClient, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("openbao address is required")
	}
	if cfg.RoleIDFile == "" {
		return nil, fmt.Errorf("openbao approle role-id file is required")
	}
	if cfg.SecretIDFile == "" {
		return nil, fmt.Errorf("openbao approle secret-id file is required")
	}

	c := &OpenBaoClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
		authenticated: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "openbao_authenticated",
			Help: "Whether the exporter holds a valid OpenBao token (1=yes, 0=no).",
		}),
		readErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "openbao_read_errors_total",
			Help: "Total number of secret read errors by reason.",
		}, []string{"reason"}),
		lastReadSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "openbao_last_read_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful secret read.",
		}),
		tokenRenewals: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "openbao_token_renewals_total",
			Help: "Total number of successful token renewals.",
		}),
		tokenRenewErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "openbao_token_renewal_errors_total",
			Help: "Total number of token renewal errors.",
		}),
	}

	if reg != nil {
		reg.MustRegister(c.authenticated, c.readErrors, c.lastReadSuccess,
			c.tokenRenewals, c.tokenRenewErrors)
	}

	// Attempt initial login. If it fails, we log and continue; the
	// background renewal goroutine will keep retrying.
	if err := c.login(); err != nil {
		logger.Warn("initial OpenBao login failed; will retry in background",
			"error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.renewLoop(ctx)

	return c, nil
}

// Close stops the background token renewal goroutine and clears the
// cached token from memory.
func (c *OpenBaoClient) Close() {
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

// ReadSecret reads a KV v2 secret at the given path and returns the value
// of the specified field. The path should be the logical path, e.g.
// "secret/data/myapp/config". If the path does not contain "/data/" it is
// rewritten automatically (secret/myapp -> secret/data/myapp).
func (c *OpenBaoClient) ReadSecret(path, field string) (string, error) {
	c.mu.RLock()
	tok := c.token
	c.mu.RUnlock()

	if tok == "" {
		c.readErrors.WithLabelValues("unauthenticated").Inc()
		return "", fmt.Errorf("openbao: not authenticated (no token)")
	}

	apiPath := ensureKVv2DataPath(path)
	url := strings.TrimRight(c.cfg.Address, "/") + "/v1/" + strings.TrimLeft(apiPath, "/")

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		c.readErrors.WithLabelValues("request_error").Inc()
		return "", fmt.Errorf("openbao: creating request: %w", err)
	}
	req.Header.Set("X-Vault-Token", tok)

	resp, err := c.client.Do(req)
	if err != nil {
		c.readErrors.WithLabelValues("connection_error").Inc()
		return "", fmt.Errorf("openbao: reading secret: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxVaultResponseBytes))
	if err != nil {
		c.readErrors.WithLabelValues("read_body_error").Inc()
		return "", fmt.Errorf("openbao: reading response body: %w", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusBadGateway {
		c.readErrors.WithLabelValues("sealed").Inc()
		return "", fmt.Errorf("openbao: vault appears sealed or unavailable (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		c.readErrors.WithLabelValues("permission_denied").Inc()
		return "", fmt.Errorf("openbao: permission denied reading %s (HTTP %d)", path, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		c.readErrors.WithLabelValues("http_error").Inc()
		return "", fmt.Errorf("openbao: unexpected status %d reading %s: %s", resp.StatusCode, path, truncateVaultBody(string(body)))
	}

	// KV v2 response structure: { "data": { "data": { "field": "value" }, "metadata": {...} } }
	var result struct {
		Data struct {
			Data map[string]interface{} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		c.readErrors.WithLabelValues("parse_error").Inc()
		return "", fmt.Errorf("openbao: parsing response: %w", err)
	}

	val, ok := result.Data.Data[field]
	if !ok {
		c.readErrors.WithLabelValues("field_not_found").Inc()
		return "", fmt.Errorf("openbao: field %q not found at path %s", field, path)
	}

	strVal, ok := val.(string)
	if !ok {
		c.readErrors.WithLabelValues("field_not_string").Inc()
		return "", fmt.Errorf("openbao: field %q at path %s is not a string", field, path)
	}

	c.lastReadSuccess.SetToCurrentTime()
	return strVal, nil
}

// login authenticates to OpenBao using the AppRole method.
func (c *OpenBaoClient) login() error {
	roleID, err := ReadSecretFile(c.cfg.RoleIDFile)
	if err != nil {
		return fmt.Errorf("reading role_id: %w", err)
	}
	secretID, err := ReadSecretFile(c.cfg.SecretIDFile)
	if err != nil {
		return fmt.Errorf("reading secret_id: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"role_id":   roleID,
		"secret_id": secretID,
	})
	if err != nil {
		return fmt.Errorf("marshaling login payload: %w", err)
	}

	url := strings.TrimRight(c.cfg.Address, "/") + "/v1/auth/approle/login"
	resp, err := c.client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("AppRole login request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxVaultResponseBytes))
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("AppRole login failed (HTTP %d): %s", resp.StatusCode, truncateVaultBody(string(body)))
	}

	var result struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
			Renewable     bool   `json:"renewable"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parsing login response: %w", err)
	}

	if result.Auth.ClientToken == "" {
		return fmt.Errorf("AppRole login returned empty token")
	}

	c.mu.Lock()
	c.token = result.Auth.ClientToken
	c.tokenTTL = time.Duration(result.Auth.LeaseDuration) * time.Second
	c.expiresAt = time.Now().Add(c.tokenTTL)
	c.mu.Unlock()

	c.authenticated.Set(1)
	c.logger.Info("authenticated to OpenBao",
		"ttl_seconds", result.Auth.LeaseDuration,
		"renewable", result.Auth.Renewable)
	return nil
}

// renewToken attempts to renew the current token via POST /v1/auth/token/renew-self.
func (c *OpenBaoClient) renewToken() error {
	c.mu.RLock()
	tok := c.token
	c.mu.RUnlock()

	if tok == "" {
		return fmt.Errorf("no token to renew")
	}

	url := strings.TrimRight(c.cfg.Address, "/") + "/v1/auth/token/renew-self"
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("creating renew request: %w", err)
	}
	req.Header.Set("X-Vault-Token", tok)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("renew request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxVaultResponseBytes))
	if err != nil {
		return fmt.Errorf("reading renew response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token renewal failed (HTTP %d): %s", resp.StatusCode, truncateVaultBody(string(body)))
	}

	var result struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parsing renew response: %w", err)
	}

	c.mu.Lock()
	c.tokenTTL = time.Duration(result.Auth.LeaseDuration) * time.Second
	c.expiresAt = time.Now().Add(c.tokenTTL)
	c.mu.Unlock()

	c.tokenRenewals.Inc()
	c.logger.Debug("renewed OpenBao token", "new_ttl_seconds", result.Auth.LeaseDuration)
	return nil
}

// renewLoop runs in the background, renewing the token before it expires.
// If renewal fails, it falls back to a full re-login. If login also fails,
// it retries with exponential backoff.
func (c *OpenBaoClient) renewLoop(ctx context.Context) {
	const (
		minBackoff = 5 * time.Second
		maxBackoff = 5 * time.Minute
	)
	backoff := minBackoff

	for {
		c.mu.RLock()
		ttl := c.tokenTTL
		expiresAt := c.expiresAt
		hasToken := c.token != ""
		c.mu.RUnlock()

		var sleepDur time.Duration
		if !hasToken {
			// No token yet; retry login after backoff.
			sleepDur = backoff
		} else {
			// Renew at 2/3 of the TTL to leave margin.
			remaining := time.Until(expiresAt)
			renewAt := ttl / 3
			if renewAt < 10*time.Second {
				renewAt = 10 * time.Second
			}
			sleepDur = remaining - renewAt
			if sleepDur < minBackoff {
				sleepDur = minBackoff
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(sleepDur):
		}

		if !hasToken {
			// Attempt login.
			if err := c.login(); err != nil {
				c.logger.Warn("OpenBao login retry failed", "error", err, "next_retry", backoff.String())
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
			backoff = minBackoff
			continue
		}

		// Attempt renewal, fall back to re-login.
		if err := c.renewToken(); err != nil {
			c.logger.Warn("token renewal failed, attempting re-login", "error", err)
			c.tokenRenewErrors.Inc()
			if loginErr := c.login(); loginErr != nil {
				c.logger.Error("re-login after renewal failure also failed",
					"renew_error", err, "login_error", loginErr)
				c.authenticated.Set(0)
				c.mu.Lock()
				c.token = ""
				c.mu.Unlock()
				backoff = minBackoff
			}
		}
	}
}

// ensureKVv2DataPath rewrites a logical KV path so that it includes the
// /data/ segment required by the KV v2 API.  For example:
//
//	"secret/myapp/config" -> "secret/data/myapp/config"
//
// If the path already contains /data/ in the right position it is returned
// unchanged.
func ensureKVv2DataPath(path string) string {
	path = strings.TrimLeft(path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		return path
	}
	// Already has /data/ prefix after mount point.
	rest := parts[1]
	if strings.HasPrefix(rest, "data/") || rest == "data" {
		return path
	}
	return parts[0] + "/data/" + rest
}

// ParseOpenBaoRef parses a "<kv-path>:<field>" reference string into its
// path and field components.
func ParseOpenBaoRef(ref string) (path, field string, err error) {
	idx := strings.LastIndex(ref, ":")
	if idx < 0 || idx == 0 || idx == len(ref)-1 {
		return "", "", fmt.Errorf("invalid openbao reference %q: expected format <kv-path>:<field>", ref)
	}
	return ref[:idx], ref[idx+1:], nil
}
