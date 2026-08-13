package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestCommitCollector_Collect(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"sha": "aaa111",
				"commit": map[string]interface{}{
					"message": "first commit",
					"author": map[string]string{
						"name": "dev1",
						"date": "2026-01-01T00:00:00Z",
					},
				},
			},
			{
				"sha": "bbb222",
				"commit": map[string]interface{}{
					"message": "second commit",
					"author": map[string]string{
						"name": "dev2",
						"date": "2026-01-02T00:00:00Z",
					},
				},
			},
		})
	}))

	collector := &CommitCollector{Client: client}
	commits, err := collector.Collect(context.Background(), "org", "repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].SHA != "aaa111" {
		t.Errorf("expected aaa111, got %s", commits[0].SHA)
	}
	if commits[0].Branch != "main" {
		t.Errorf("expected main, got %s", commits[0].Branch)
	}
	if commits[1].Author != "dev2" {
		t.Errorf("expected dev2, got %s", commits[1].Author)
	}
}

func TestCommitCollector_EmptyResult(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))

	collector := &CommitCollector{Client: client}
	commits, err := collector.Collect(context.Background(), "org", "repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("expected 0 commits, got %d", len(commits))
	}
}

func TestCommitCollector_APIError(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	collector := &CommitCollector{Client: client}
	_, err := collector.Collect(context.Background(), "org", "repo", "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCommitCollector_Pagination(t *testing.T) {
	page := 0
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		var batch []map[string]interface{}
		if page == 1 {
			for i := 0; i < 100; i++ {
				batch = append(batch, map[string]interface{}{
					"sha": fmt.Sprintf("sha%d", i),
					"commit": map[string]interface{}{
						"message": fmt.Sprintf("commit %d", i),
						"author":  map[string]string{"name": "dev", "date": "2026-01-01T00:00:00Z"},
					},
				})
			}
		} else {
			batch = append(batch, map[string]interface{}{
				"sha": "sha-last",
				"commit": map[string]interface{}{
					"message": "last commit",
					"author":  map[string]string{"name": "dev", "date": "2026-01-02T00:00:00Z"},
				},
			})
		}
		json.NewEncoder(w).Encode(batch)
	}))

	collector := &CommitCollector{Client: client}
	commits, err := collector.Collect(context.Background(), "org", "repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 101 {
		t.Errorf("expected 101 commits, got %d", len(commits))
	}
}
