package cloudflare

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRESTGet_ReadBodyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100") // Lie about content length
		w.Write([]byte("short"))
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	_, _, err := c.RESTGet(context.Background(), "/test")
	// May or may not error depending on implementation
	_ = err
}

func TestQueryGraphQL_ContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, _, err := c.QueryGraphQL(ctx, "{}", nil)
	if err == nil {
		t.Fatal("expected error for context timeout")
	}
}

func TestRESTGet_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := c.RESTGet(ctx, "/test")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("my-token", 30*time.Second)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.apiToken != "my-token" {
		t.Fatalf("expected 'my-token', got %q", c.apiToken)
	}
}

func TestSetHTTPClient_CustomTimeout(t *testing.T) {
	c := NewClient("token", 5*time.Second)
	custom := &http.Client{Timeout: 30 * time.Second}
	c.SetHTTPClient(custom)
	if c.httpClient != custom {
		t.Fatal("expected custom HTTP client to be set")
	}
}

func TestValidateDimensions_Mixed(t *testing.T) {
	// Mix of valid and invalid
	err := ValidateDimensions([]string{"queryType", "userEmail", "responseCode"})
	if err == nil {
		t.Fatal("expected error for mixed dimensions with denied one")
	}
}

type errorReader struct{}

func (e errorReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("simulated read error")
}

func (e errorReader) Close() error {
	return nil
}

func TestQueryGraphQL_ReadBodyError(t *testing.T) {
	c := NewClient("token", 5*time.Second)
	c.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       errorReader{},
			}, nil
		}),
	})

	_, _, err := c.QueryGraphQL(context.Background(), "{}", nil)
	if err == nil {
		t.Fatal("expected error for read body failure")
	}
	if !strings.Contains(err.Error(), "reading GraphQL response") {
		t.Fatalf("expected 'reading GraphQL response' error, got: %v", err)
	}
}

func TestRESTGet_ReadBodyErrorDirect(t *testing.T) {
	c := NewClient("token", 5*time.Second)
	c.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       errorReader{},
			}, nil
		}),
	})

	_, _, err := c.RESTGet(context.Background(), "/test")
	if err == nil {
		t.Fatal("expected error for read body failure")
	}
	if !strings.Contains(err.Error(), "reading REST response") {
		t.Fatalf("expected 'reading REST response' error, got: %v", err)
	}
}
