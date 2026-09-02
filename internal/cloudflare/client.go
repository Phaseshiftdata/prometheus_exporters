// Package cloudflare provides a typed client for the Cloudflare GraphQL Analytics API
// and a wrapper around cloudflare-go for REST v4 operations.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// GraphQLEndpoint is the Cloudflare GraphQL Analytics API endpoint.
	GraphQLEndpoint = "https://api.cloudflare.com/client/v4/graphql"

	// RESTV4Base is the base URL for the Cloudflare REST v4 API.
	RESTV4Base = "https://api.cloudflare.com/client/v4"

	// maxResponseBytes caps how much of a response body is read.
	maxResponseBytes = 100 << 20 // 100 MiB

	// maxErrorBodyLog caps how much of an error response body is stored
	// in APIError to prevent upstream content from flooding logs.
	maxErrorBodyLog = 512
)

// deniedDimensions contains dimension names that must never appear in a GraphQL query.
// These are privacy-sensitive fields per spec section 10.1.
var deniedDimensions = map[string]bool{
	"userEmail":         true,
	"userUID":           true,
	"deviceIdentifier":  true,
	"sourceIP":          true,
	"destinationIP":     true,
	"clientIP":          true,
	"deviceId":          true,
	"email":             true,
	"userIdentifier":    true,
	"deviceSerialNumber": true,
}

// Client communicates with the Cloudflare APIs.
type Client struct {
	httpClient *http.Client
	apiToken   []byte
	tokenMu    sync.RWMutex
	userAgent  string
}

// NewClient creates a new Cloudflare API client. The token is copied
// into a []byte so the caller's string can be garbage-collected.
func NewClient(apiToken string, timeout time.Duration) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		apiToken:   []byte(apiToken),
		userAgent:  "cloudflare_exporter/1.0",
	}
}

// Close zeroes the API token from memory.
func (c *Client) Close() {
	c.tokenMu.Lock()
	for i := range c.apiToken {
		c.apiToken[i] = 0
	}
	c.tokenMu.Unlock()
}

// token returns the API token as a string, safe for concurrent use.
func (c *Client) token() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return string(c.apiToken)
}

// SetHTTPClient replaces the underlying HTTP client. This is intended for
// testing with httptest.Server.
func (c *Client) SetHTTPClient(hc *http.Client) {
	c.httpClient = hc
}

// GraphQLRequest represents a GraphQL request payload.
type GraphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// GraphQLResponse represents a GraphQL response.
type GraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []GraphQLError  `json:"errors,omitempty"`
}

// GraphQLError represents a single GraphQL error.
type GraphQLError struct {
	Message    string         `json:"message"`
	Path       []string       `json:"path,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// Error implements the error interface.
func (e GraphQLError) Error() string {
	return e.Message
}

// QueryGraphQL sends a GraphQL query and returns the parsed response.
func (c *Client) QueryGraphQL(ctx context.Context, query string, variables map[string]any) (*GraphQLResponse, http.Header, error) {
	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling GraphQL request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GraphQLEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("creating GraphQL request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("executing GraphQL request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.Header, fmt.Errorf("reading GraphQL response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, resp.Header, &RateLimitError{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp.Header, &APIError{
			StatusCode: resp.StatusCode,
			Body:       truncateErrorBody(string(respBody)),
		}
	}

	var gqlResp GraphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, resp.Header, fmt.Errorf("decoding GraphQL response: %w", err)
	}

	return &gqlResp, resp.Header, nil
}

// RESTGet performs a GET request to the REST v4 API.
func (c *Client) RESTGet(ctx context.Context, path string) (json.RawMessage, http.Header, error) {
	url := RESTV4Base + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("creating REST request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("executing REST request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.Header, fmt.Errorf("reading REST response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, resp.Header, &RateLimitError{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp.Header, &APIError{
			StatusCode: resp.StatusCode,
			Body:       truncateErrorBody(string(body)),
		}
	}

	return body, resp.Header, nil
}

// RESTResponse is the standard Cloudflare REST v4 response wrapper.
type RESTResponse struct {
	Success  bool              `json:"success"`
	Errors   []RESTError       `json:"errors"`
	Messages []json.RawMessage `json:"messages"`
	Result   json.RawMessage   `json:"result"`
}

// RESTError represents a Cloudflare REST API error.
type RESTError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ParseRESTResponse parses a standard Cloudflare REST v4 response.
func ParseRESTResponse(data json.RawMessage) (*RESTResponse, error) {
	var resp RESTResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing REST response: %w", err)
	}
	if !resp.Success {
		msgs := make([]string, len(resp.Errors))
		for i, e := range resp.Errors {
			msgs[i] = e.Message
		}
		return &resp, &APIError{
			StatusCode: 0,
			Body:       strings.Join(msgs, "; "),
		}
	}
	return &resp, nil
}

// RateLimitError indicates a 429 response.
type RateLimitError struct {
	StatusCode int
	Headers    http.Header
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited (HTTP %d)", e.StatusCode)
}

// APIError indicates a non-success HTTP response.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (HTTP %d): %s", e.StatusCode, e.Body)
}

// truncateErrorBody returns at most maxErrorBodyLog bytes of s to prevent
// upstream error content from flooding logs.
func truncateErrorBody(s string) string {
	if len(s) <= maxErrorBodyLog {
		return s
	}
	return s[:maxErrorBodyLog] + "...(truncated)"
}

// ValidateDimensions checks that no denied dimension names are present.
// This is the enforcement mechanism for NFR-3 (privacy).
func ValidateDimensions(dimensions []string) error {
	for _, d := range dimensions {
		if deniedDimensions[d] {
			return fmt.Errorf("prohibited dimension %q: privacy violation per NFR-3", d)
		}
	}
	return nil
}

// IsDeniedDimension returns true if the dimension name is prohibited.
func IsDeniedDimension(name string) bool {
	return deniedDimensions[name]
}

// RedactToken replaces any occurrence of the API token in a string with [REDACTED].
func (c *Client) RedactToken(s string) string {
	tok := c.token()
	if tok == "" {
		return s
	}
	return strings.ReplaceAll(s, tok, "[REDACTED]")
}
