package github

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// NewTestAuth creates an Auth for testing that returns a fixed token
// without requiring a real GitHub App private key.
func NewTestAuth(token string, expiry time.Time) *Auth {
	a := &Auth{
		cachedToken: token,
		tokenExpiry: expiry,
		nowFunc:     time.Now,
	}
	a.tokenRefresh = func(ctx context.Context) (string, time.Time, error) {
		return token, time.Now().Add(1 * time.Hour), nil
	}
	return a
}

// SetHTTPClient overrides the HTTP client and installs a URL-rewriting
// transport so that requests to api.github.com are redirected to baseURL.
// This is intended for use in tests with httptest.Server.
func (c *Client) SetHTTPClient(httpClient *http.Client, baseURL string) {
	transport := httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	c.httpClient = &http.Client{
		Transport: &testURLRewriter{
			base:      baseURL,
			transport: transport,
		},
	}
}

// testURLRewriter redirects requests from api.github.com to a test server.
type testURLRewriter struct {
	base      string
	transport http.RoundTripper
}

func (u *testURLRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(u.base, "http://")
	return u.transport.RoundTrip(req)
}
