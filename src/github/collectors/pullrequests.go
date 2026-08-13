package collectors

import (
	"context"
	"fmt"
	"time"
)

// PRCollector collects pull request data from the GitHub API.
type PRCollector struct {
	Client APIClient
}

type apiPullRequest struct {
	ID   int64  `json:"id"`
	Number int  `json:"number"`
	State  string `json:"state"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string `json:"created_at"`
	MergedAt  string `json:"merged_at"`
	ClosedAt  string `json:"closed_at"`
	UpdatedAt string `json:"updated_at"`
}

// Collect fetches all pull requests for a given repository.
func (c *PRCollector) Collect(ctx context.Context, org, repo string) ([]PullRequest, error) {
	var prs []PullRequest
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?state=all&per_page=100&page=%d", org, repo, page)
		var batch []apiPullRequest
		modified, err := c.Client.Get(ctx, url, &batch)
		if err != nil {
			return nil, fmt.Errorf("fetching pull requests page %d: %w", page, err)
		}
		if !modified || len(batch) == 0 {
			break
		}

		for _, p := range batch {
			createdAt, _ := time.Parse(time.RFC3339, p.CreatedAt)
			pr := PullRequest{
				ID:        p.ID,
				Repo:      repo,
				Number:    p.Number,
				State:     p.State,
				Author:    p.User.Login,
				CreatedAt: createdAt,
			}
			if p.MergedAt != "" {
				if t, err := time.Parse(time.RFC3339, p.MergedAt); err == nil {
					pr.MergedAt = &t
				}
			}
			if p.ClosedAt != "" {
				if t, err := time.Parse(time.RFC3339, p.ClosedAt); err == nil {
					pr.ClosedAt = &t
				}
			}
			if p.UpdatedAt != "" {
				if t, err := time.Parse(time.RFC3339, p.UpdatedAt); err == nil {
					pr.UpdatedAt = &t
				}
			}
			prs = append(prs, pr)
		}

		if len(batch) < 100 {
			break
		}
		page++
	}

	return prs, nil
}
