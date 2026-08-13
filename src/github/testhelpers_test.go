package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestGitHubServer creates a test server that serves canned responses
// for all the GitHub API endpoints used by the collectors.
func newTestGitHubServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Repos
	mux.HandleFunc("/orgs/test-org/repos", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"id":             1,
				"name":           "test-repo",
				"default_branch": "main",
				"visibility":     "private",
				"archived":       false,
				"updated_at":     "2026-01-01T00:00:00Z",
			},
		})
	})

	// Workflow runs
	mux.HandleFunc("/repos/test-org/test-repo/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflow_runs": []map[string]interface{}{
				{
					"id":             100,
					"name":           "CI",
					"head_branch":    "main",
					"conclusion":     "success",
					"run_attempt":    1,
					"created_at":     "2026-01-01T00:00:00Z",
					"run_started_at": "2026-01-01T00:00:01Z",
					"updated_at":     "2026-01-01T00:01:00Z",
				},
			},
		})
	})

	// Workflow jobs
	mux.HandleFunc("/repos/test-org/test-repo/actions/runs/100/jobs", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jobs": []map[string]interface{}{
				{
					"id":           200,
					"run_id":       100,
					"name":         "build",
					"conclusion":   "success",
					"started_at":   "2026-01-01T00:00:01Z",
					"completed_at": "2026-01-01T00:01:00Z",
				},
			},
		})
	})

	// Pull requests
	mux.HandleFunc("/repos/test-org/test-repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"id":         300,
				"number":     1,
				"state":      "open",
				"user":       map[string]string{"login": "dev1"},
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-01T01:00:00Z",
			},
		})
	})

	// Commits
	mux.HandleFunc("/repos/test-org/test-repo/commits", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"sha": "abc123",
				"commit": map[string]interface{}{
					"message": "initial commit",
					"author": map[string]string{
						"name": "dev1",
						"date": "2026-01-01T00:00:00Z",
					},
				},
			},
		})
	})

	// Tags
	mux.HandleFunc("/repos/test-org/test-repo/tags", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"name":   "v1.0.0",
				"commit": map[string]string{"sha": "abc123"},
			},
		})
	})

	// Releases
	mux.HandleFunc("/repos/test-org/test-repo/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"tag_name":     "v1.0.0",
				"created_at":   "2026-01-01T00:00:00Z",
				"published_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	return httptest.NewServer(mux)
}

// createPlainClient returns a plain http.Client for use with SetHTTPClient.
func createPlainClient() *http.Client {
	return &http.Client{}
}

// newErrorServer creates a test server that always returns the given status code.
func newErrorServer(statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte("error"))
	}))
}
