package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestTagCollector_Collect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"tag_name":     "v1.0.0",
				"created_at":   "2026-01-01T00:00:00Z",
				"published_at": "2026-01-01T12:00:00Z",
			},
		})
	})
	mux.HandleFunc("/repos/org/repo/tags", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"name":   "v1.0.0",
				"commit": map[string]string{"sha": "abc123"},
			},
			{
				"name":   "v0.9.0",
				"commit": map[string]string{"sha": "def456"},
			},
		})
	})

	client := newHTTPTestClient(t, mux)
	collector := &TagCollector{Client: client}
	tags, err := collector.Collect(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	if tags[0].Name != "v1.0.0" {
		t.Errorf("expected v1.0.0, got %s", tags[0].Name)
	}
	if tags[0].CreatedAt == nil {
		t.Error("expected CreatedAt to be set for v1.0.0 (from release)")
	}
	if tags[0].TargetSHA != "abc123" {
		t.Errorf("expected abc123, got %s", tags[0].TargetSHA)
	}

	if tags[1].Name != "v0.9.0" {
		t.Errorf("expected v0.9.0, got %s", tags[1].Name)
	}
	if tags[1].CreatedAt != nil {
		t.Error("expected nil CreatedAt for v0.9.0")
	}
}

func TestTagCollector_NoReleases(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	})
	mux.HandleFunc("/repos/org/repo/tags", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"name":   "v1.0.0",
				"commit": map[string]string{"sha": "abc"},
			},
		})
	})

	client := newHTTPTestClient(t, mux)
	collector := &TagCollector{Client: client}
	tags, err := collector.Collect(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].CreatedAt != nil {
		t.Error("expected nil CreatedAt when no matching release")
	}
}

func TestTagCollector_EmptyResult(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	})
	mux.HandleFunc("/repos/org/repo/tags", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	})

	client := newHTTPTestClient(t, mux)
	collector := &TagCollector{Client: client}
	tags, err := collector.Collect(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected 0 tags, got %d", len(tags))
	}
}

func TestTagCollector_ReleaseFetchError(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	collector := &TagCollector{Client: client}
	_, err := collector.Collect(context.Background(), "org", "repo")
	if err == nil {
		t.Fatal("expected error when release fetch fails")
	}
}

func TestTagCollector_TagFetchError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	})
	mux.HandleFunc("/repos/org/repo/tags", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	client := newHTTPTestClient(t, mux)
	collector := &TagCollector{Client: client}
	_, err := collector.Collect(context.Background(), "org", "repo")
	if err == nil {
		t.Fatal("expected error when tag fetch fails")
	}
}

func TestTagCollector_ReleaseWithOnlyCreatedAt(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"tag_name":   "v1.0.0",
				"created_at": "2026-06-15T10:00:00Z",
				// published_at intentionally omitted
			},
		})
	})
	mux.HandleFunc("/repos/org/repo/tags", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"name":   "v1.0.0",
				"commit": map[string]string{"sha": "abc123"},
			},
		})
	})

	client := newHTTPTestClient(t, mux)
	collector := &TagCollector{Client: client}
	tags, err := collector.Collect(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].CreatedAt == nil {
		t.Error("expected CreatedAt from release created_at")
	}
}

func TestTagCollector_Pagination(t *testing.T) {
	tagPage := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	})
	mux.HandleFunc("/repos/org/repo/tags", func(w http.ResponseWriter, r *http.Request) {
		tagPage++
		var batch []map[string]interface{}
		if tagPage == 1 {
			for i := 0; i < 100; i++ {
				batch = append(batch, map[string]interface{}{
					"name":   fmt.Sprintf("v1.%d.0", i),
					"commit": map[string]string{"sha": fmt.Sprintf("sha%d", i)},
				})
			}
		} else {
			batch = append(batch, map[string]interface{}{
				"name":   "v2.0.0",
				"commit": map[string]string{"sha": "sha-last"},
			})
		}
		json.NewEncoder(w).Encode(batch)
	})

	client := newHTTPTestClient(t, mux)
	collector := &TagCollector{Client: client}
	tags, err := collector.Collect(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 101 {
		t.Errorf("expected 101 tags (100+1), got %d", len(tags))
	}
}

func TestTagCollector_ReleasePagination(t *testing.T) {
	releasePage := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		releasePage++
		var batch []map[string]interface{}
		if releasePage == 1 {
			for i := 0; i < 100; i++ {
				batch = append(batch, map[string]interface{}{
					"tag_name":     fmt.Sprintf("v1.%d.0", i),
					"published_at": "2026-01-01T00:00:00Z",
				})
			}
		} else {
			batch = append(batch, map[string]interface{}{
				"tag_name":     "v2.0.0",
				"published_at": "2026-06-01T00:00:00Z",
			})
		}
		json.NewEncoder(w).Encode(batch)
	})
	mux.HandleFunc("/repos/org/repo/tags", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "v2.0.0", "commit": map[string]string{"sha": "abc"}},
		})
	})

	client := newHTTPTestClient(t, mux)
	collector := &TagCollector{Client: client}
	tags, err := collector.Collect(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].CreatedAt == nil {
		t.Error("expected CreatedAt for v2.0.0")
	}
}
