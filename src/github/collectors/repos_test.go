package collectors

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRepoCollector_CollectAll(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"id":             1,
				"name":           "repo-a",
				"default_branch": "main",
				"visibility":     "private",
				"archived":       false,
				"updated_at":     "2026-01-01T00:00:00Z",
			},
			{
				"id":             2,
				"name":           "repo-b",
				"default_branch": "develop",
				"visibility":     "public",
				"archived":       true,
				"updated_at":     "2026-06-15T12:00:00Z",
			},
		})
	}))

	collector := &RepoCollector{Client: client}
	repos, err := collector.CollectAll(context.Background(), "my-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[0].Name != "repo-a" {
		t.Errorf("expected repo-a, got %s", repos[0].Name)
	}
	if repos[1].Archived != true {
		t.Error("expected repo-b to be archived")
	}
}

func TestRepoCollector_Pagination(t *testing.T) {
	var requestCount atomic.Int32

	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := requestCount.Add(1)
		if page == 1 {
			repos := make([]map[string]interface{}, 100)
			for i := 0; i < 100; i++ {
				repos[i] = map[string]interface{}{
					"id":             int64(i + 1),
					"name":           "repo-" + string(rune('a'+i%26)),
					"default_branch": "main",
					"visibility":     "private",
					"archived":       false,
					"updated_at":     "2026-01-01T00:00:00Z",
				}
			}
			json.NewEncoder(w).Encode(repos)
			return
		}
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"id":             101,
				"name":           "repo-last",
				"default_branch": "main",
				"visibility":     "private",
				"archived":       false,
				"updated_at":     "2026-01-01T00:00:00Z",
			},
		})
	}))

	collector := &RepoCollector{Client: client}
	repos, err := collector.CollectAll(context.Background(), "my-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 101 {
		t.Fatalf("expected 101 repos, got %d", len(repos))
	}
	if int(requestCount.Load()) != 2 {
		t.Fatalf("expected 2 requests, got %d", requestCount.Load())
	}
}

func TestRepoCollector_EmptyResult(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))

	collector := &RepoCollector{Client: client}
	repos, err := collector.CollectAll(context.Background(), "empty-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(repos))
	}
}

func TestRepoCollector_APIError(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	collector := &RepoCollector{Client: client}
	_, err := collector.CollectAll(context.Background(), "my-org")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestRepoCollector_MaxPagesExceeded(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repos := make([]map[string]interface{}, 100)
		for i := 0; i < 100; i++ {
			repos[i] = map[string]interface{}{
				"id":             int64(i + 1),
				"name":           "repo",
				"default_branch": "main",
				"visibility":     "private",
				"archived":       false,
				"updated_at":     "2026-01-01T00:00:00Z",
			}
		}
		json.NewEncoder(w).Encode(repos)
	}))

	collector := &RepoCollector{Client: client}
	_, err := collector.CollectAll(context.Background(), "my-org")
	if err == nil {
		t.Fatal("expected error when pagination exceeds maxPages")
	}
	if !strings.Contains(err.Error(), "pagination exceeded") {
		t.Errorf("expected pagination exceeded error, got: %v", err)
	}
}
