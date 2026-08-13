package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// httpTestClient wraps an httptest.Server and implements APIClient for tests.
type httpTestClient struct {
	server *httptest.Server
}

func newHTTPTestClient(t *testing.T, handler http.Handler) *httpTestClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &httpTestClient{server: server}
}

func (c *httpTestClient) Get(ctx context.Context, url string, result interface{}) (bool, error) {
	// Rewrite the URL to point at our test server.
	// Extract the path from the original URL.
	// URL format: https://api.github.com/path...
	path := ""
	const prefix = "https://api.github.com"
	if len(url) > len(prefix) {
		path = url[len(prefix):]
	}

	resp, err := c.server.Client().Get(c.server.URL + path)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return false, err
	}
	return true, nil
}
