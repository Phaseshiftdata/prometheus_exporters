package collectors

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPRCollector_Collect(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"id":         1,
				"number":     42,
				"state":      "open",
				"user":       map[string]string{"login": "alice"},
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-02T00:00:00Z",
			},
			{
				"id":         2,
				"number":     43,
				"state":      "closed",
				"user":       map[string]string{"login": "bob"},
				"created_at": "2026-01-01T00:00:00Z",
				"merged_at":  "2026-01-03T00:00:00Z",
				"closed_at":  "2026-01-03T00:00:00Z",
				"updated_at": "2026-01-03T00:00:00Z",
			},
		})
	}))

	collector := &PRCollector{Client: client}
	prs, err := collector.Collect(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(prs) != 2 {
		t.Fatalf("expected 2 PRs, got %d", len(prs))
	}
	if prs[0].Author != "alice" {
		t.Errorf("expected alice, got %s", prs[0].Author)
	}
	if prs[0].State != "open" {
		t.Errorf("expected open, got %s", prs[0].State)
	}
	if prs[1].MergedAt == nil {
		t.Error("expected MergedAt to be set for closed PR")
	}
}

func TestPRCollector_EmptyResult(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))

	collector := &PRCollector{Client: client}
	prs, err := collector.Collect(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 0 {
		t.Fatalf("expected 0 PRs, got %d", len(prs))
	}
}

func TestPRCollector_APIError(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	collector := &PRCollector{Client: client}
	_, err := collector.Collect(context.Background(), "org", "repo")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPRCollector_Pagination(t *testing.T) {
	page := 0
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		var batch []map[string]interface{}
		if page == 1 {
			for i := 0; i < 100; i++ {
				batch = append(batch, map[string]interface{}{
					"id":         i + 1,
					"number":     i + 1,
					"state":      "open",
					"user":       map[string]string{"login": "dev"},
					"created_at": "2026-01-01T00:00:00Z",
					"updated_at": "2026-01-01T00:00:00Z",
				})
			}
		} else {
			batch = append(batch, map[string]interface{}{
				"id":         999,
				"number":     999,
				"state":      "open",
				"user":       map[string]string{"login": "dev"},
				"created_at": "2026-02-01T00:00:00Z",
				"updated_at": "2026-02-01T00:00:00Z",
			})
		}
		json.NewEncoder(w).Encode(batch)
	}))

	collector := &PRCollector{Client: client}
	prs, err := collector.Collect(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 101 {
		t.Errorf("expected 101 PRs, got %d", len(prs))
	}
}

func TestPRCollector_MaxPagesExceeded(t *testing.T) {
	client := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch []map[string]interface{}
		for i := 0; i < 100; i++ {
			batch = append(batch, map[string]interface{}{
				"id":         i + 1,
				"number":     i + 1,
				"state":      "open",
				"user":       map[string]string{"login": "dev"},
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-01T00:00:00Z",
			})
		}
		json.NewEncoder(w).Encode(batch)
	}))

	collector := &PRCollector{Client: client}
	_, err := collector.Collect(context.Background(), "org", "repo")
	if err == nil {
		t.Fatal("expected error when pagination exceeds maxPages")
	}
	if !strings.Contains(err.Error(), "pagination exceeded") {
		t.Errorf("expected pagination exceeded error, got: %v", err)
	}
}
