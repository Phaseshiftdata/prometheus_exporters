package collectors

import (
	"context"
	"fmt"
	"time"
)

// RepoCollector collects repository metadata from the GitHub API.
type RepoCollector struct {
	Client APIClient
}

// apiRepo is the JSON shape returned by the GitHub repos API.
type apiRepo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility"`
	Archived      bool   `json:"archived"`
	UpdatedAt     string `json:"updated_at"`
}

// CollectAll fetches all repositories for the given organization.
func (c *RepoCollector) CollectAll(ctx context.Context, org string) ([]Repository, error) {
	var repos []Repository
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/orgs/%s/repos?per_page=100&page=%d", org, page)
		var batch []apiRepo
		modified, err := c.Client.Get(ctx, url, &batch)
		if err != nil {
			return nil, fmt.Errorf("fetching repos page %d: %w", page, err)
		}
		if !modified || len(batch) == 0 {
			break
		}

		for _, r := range batch {
			updatedAt, _ := time.Parse(time.RFC3339, r.UpdatedAt)
			repos = append(repos, Repository{
				ID:            r.ID,
				Name:          r.Name,
				DefaultBranch: r.DefaultBranch,
				Visibility:    r.Visibility,
				Archived:      r.Archived,
				UpdatedAt:     updatedAt,
			})
		}

		if len(batch) < 100 {
			break
		}
		page++
	}

	return repos, nil
}
