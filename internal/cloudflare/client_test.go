package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateDimensions_AllGood(t *testing.T) {
	dims := []string{"queryType", "responseCode", "datetimeMinute"}
	if err := ValidateDimensions(dims); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateDimensions_Denied(t *testing.T) {
	denied := []string{
		"userEmail", "userUID", "deviceIdentifier", "sourceIP",
		"destinationIP", "clientIP", "deviceId", "email",
		"userIdentifier", "deviceSerialNumber",
	}
	for _, d := range denied {
		err := ValidateDimensions([]string{d})
		if err == nil {
			t.Errorf("expected error for denied dimension %q", d)
		}
	}
}

func TestValidateDimensions_Empty(t *testing.T) {
	if err := ValidateDimensions(nil); err != nil {
		t.Fatalf("expected no error for nil, got: %v", err)
	}
	if err := ValidateDimensions([]string{}); err != nil {
		t.Fatalf("expected no error for empty, got: %v", err)
	}
}

func TestIsDeniedDimension(t *testing.T) {
	if !IsDeniedDimension("userEmail") {
		t.Error("expected userEmail to be denied")
	}
	if IsDeniedDimension("queryType") {
		t.Error("expected queryType to not be denied")
	}
}

func TestRedactToken(t *testing.T) {
	c := NewClient("my-secret-token", 10*time.Second)
	result := c.RedactToken("Error with token my-secret-token in message")
	if result != "Error with token [REDACTED] in message" {
		t.Fatalf("expected redacted, got: %q", result)
	}
}

func TestRedactToken_EmptyToken(t *testing.T) {
	c := NewClient("", 10*time.Second)
	input := "some string"
	if c.RedactToken(input) != input {
		t.Fatal("expected no change for empty token")
	}
}

// newTestClient creates a Client pointing at the given test server.
func newTestClient(server *httptest.Server, token string) *Client {
	c := NewClient(token, 5*time.Second)
	c.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = server.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	})
	return c
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestQueryGraphQL_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing or incorrect auth header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing content-type")
		}
		if r.Method != http.MethodPost {
			t.Error("expected POST")
		}

		resp := GraphQLResponse{
			Data: json.RawMessage(`{"viewer":{"accounts":[]}}`),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := newTestClient(server, "test-token")
	resp, headers, err := c.QueryGraphQL(context.Background(), "{ viewer { accounts { id } } }", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if headers == nil {
		t.Fatal("expected non-nil headers")
	}
}

func TestQueryGraphQL_WithVariables(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Variables == nil {
			t.Error("expected variables in request")
		}
		resp := GraphQLResponse{
			Data: json.RawMessage(`{}`),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	vars := map[string]any{"accountTag": "acc1"}
	_, _, err := c.QueryGraphQL(context.Background(), "query ($accountTag: string!) { viewer {} }", vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQueryGraphQL_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"errors":[]}`))
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	_, _, err := c.QueryGraphQL(context.Background(), "{}", nil)
	if err == nil {
		t.Fatal("expected error for 429")
	}

	rlErr, ok := err.(*RateLimitError)
	if !ok {
		t.Fatalf("expected RateLimitError, got %T", err)
	}
	if rlErr.StatusCode != 429 {
		t.Fatalf("expected 429, got %d", rlErr.StatusCode)
	}
	if rlErr.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestQueryGraphQL_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":[{"message":"forbidden"}]}`))
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	_, _, err := c.QueryGraphQL(context.Background(), "{}", nil)
	if err == nil {
		t.Fatal("expected error for 403")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", apiErr.StatusCode)
	}
	if apiErr.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestQueryGraphQL_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	_, _, err := c.QueryGraphQL(context.Background(), "{}", nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestRESTGet_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("expected GET")
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing auth header")
		}
		resp := struct {
			Success bool            `json:"success"`
			Result  json.RawMessage `json:"result"`
		}{
			Success: true,
			Result:  json.RawMessage(`{"status":"active"}`),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := newTestClient(server, "test-token")
	body, headers, err := c.RESTGet(context.Background(), "/user/tokens/verify")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body == nil {
		t.Fatal("expected non-nil body")
	}
	if headers == nil {
		t.Fatal("expected non-nil headers")
	}
}

func TestRESTGet_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	_, _, err := c.RESTGet(context.Background(), "/test")
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if _, ok := err.(*RateLimitError); !ok {
		t.Fatalf("expected RateLimitError, got %T", err)
	}
}

func TestRESTGet_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`not found`))
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	_, _, err := c.RESTGet(context.Background(), "/test")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", apiErr.StatusCode)
	}
}

func TestParseRESTResponse_Success(t *testing.T) {
	data := json.RawMessage(`{"success":true,"errors":[],"messages":[],"result":{"status":"active"}}`)
	resp, err := ParseRESTResponse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success")
	}
}

func TestParseRESTResponse_Failure(t *testing.T) {
	data := json.RawMessage(`{"success":false,"errors":[{"code":1003,"message":"Invalid token"}],"result":null}`)
	_, err := ParseRESTResponse(data)
	if err == nil {
		t.Fatal("expected error for failed response")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Body == "" {
		t.Error("expected non-empty error body")
	}
}

func TestParseRESTResponse_InvalidJSON(t *testing.T) {
	data := json.RawMessage(`not json`)
	_, err := ParseRESTResponse(data)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGraphQLError_Error(t *testing.T) {
	e := GraphQLError{Message: "test error"}
	if e.Error() != "test error" {
		t.Fatalf("expected 'test error', got %q", e.Error())
	}
}

func TestGraphQLResponse_WithErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GraphQLResponse{
			Data: json.RawMessage(`{}`),
			Errors: []GraphQLError{
				{Message: "some error", Path: []string{"viewer", "accounts"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	resp, _, err := c.QueryGraphQL(context.Background(), "{}", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(resp.Errors))
	}
}

func TestQueryGraphQL_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	c := newTestClient(server, "token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := c.QueryGraphQL(ctx, "{}", nil)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestSetHTTPClient(t *testing.T) {
	c := NewClient("token", 5*time.Second)
	custom := &http.Client{Timeout: 30 * time.Second}
	c.SetHTTPClient(custom)
	// The internal field should be updated. We verify by making a request
	// that uses the custom client's timeout.
	// (Just verify it doesn't panic)
}

func TestNewClient(t *testing.T) {
	c := NewClient("my-token", 10*time.Second)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}
