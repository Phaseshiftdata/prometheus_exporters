package collectors

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// CommitCollector collects commit data from the GitHub API.
type CommitCollector struct {
	Client APIClient
}

type apiCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

// Collect fetches commits for a given repository and branch.
func (c *CommitCollector) Collect(ctx context.Context, org, repo string, defaultBranch string) ([]Commit, error) {
	var commits []Commit
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits?sha=%s&per_page=100&page=%d", url.PathEscape(org), url.PathEscape(repo), url.QueryEscape(defaultBranch), page)
		var batch []apiCommit
		modified, err := c.Client.Get(ctx, url, &batch)
		if err != nil {
			return nil, fmt.Errorf("fetching commits page %d: %w", page, err)
		}
		if !modified || len(batch) == 0 {
			break
		}

		for _, ac := range batch {
			committedAt, _ := time.Parse(time.RFC3339, ac.Commit.Author.Date)
			commits = append(commits, Commit{
				SHA:         ac.SHA,
				Repo:        repo,
				Branch:      defaultBranch,
				Author:      ac.Commit.Author.Name,
				Message:     ac.Commit.Message,
				CommittedAt: committedAt,
			})
		}

		if len(batch) < 100 {
			break
		}
		page++
		if page > maxPages {
			return nil, fmt.Errorf("commits pagination exceeded %d pages", maxPages)
		}
	}

	return commits, nil
}
