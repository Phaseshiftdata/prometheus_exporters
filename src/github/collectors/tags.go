package collectors

import (
	"context"
	"fmt"
	"time"
)

// TagCollector collects tag and release data from the GitHub API.
type TagCollector struct {
	Client APIClient
}

type apiTag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

type apiRelease struct {
	TagName     string `json:"tag_name"`
	CreatedAt   string `json:"created_at"`
	PublishedAt string `json:"published_at"`
}

// Collect fetches tags and release metadata for a given repository.
func (c *TagCollector) Collect(ctx context.Context, org, repo string) ([]Tag, error) {
	// Fetch releases first so we can look up created_at by tag name.
	releases, err := c.collectReleases(ctx, org, repo)
	if err != nil {
		return nil, fmt.Errorf("fetching releases: %w", err)
	}

	releaseMap := make(map[string]time.Time, len(releases))
	for _, r := range releases {
		dateStr := r.PublishedAt
		if dateStr == "" {
			dateStr = r.CreatedAt
		}
		if dateStr != "" {
			if t, parseErr := time.Parse(time.RFC3339, dateStr); parseErr == nil {
				releaseMap[r.TagName] = t
			}
		}
	}

	var tags []Tag
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/tags?per_page=100&page=%d", org, repo, page)
		var batch []apiTag
		modified, getErr := c.Client.Get(ctx, url, &batch)
		if getErr != nil {
			return nil, fmt.Errorf("fetching tags page %d: %w", page, getErr)
		}
		if !modified || len(batch) == 0 {
			break
		}

		for _, t := range batch {
			tag := Tag{
				Repo:      repo,
				Name:      t.Name,
				TargetSHA: t.Commit.SHA,
			}
			if created, ok := releaseMap[t.Name]; ok {
				created := created // copy for pointer
				tag.CreatedAt = &created
			}
			tags = append(tags, tag)
		}

		if len(batch) < 100 {
			break
		}
		page++
	}

	return tags, nil
}

func (c *TagCollector) collectReleases(ctx context.Context, org, repo string) ([]apiRelease, error) {
	var releases []apiRelease
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=100&page=%d", org, repo, page)
		var batch []apiRelease
		modified, err := c.Client.Get(ctx, url, &batch)
		if err != nil {
			return nil, err
		}
		if !modified || len(batch) == 0 {
			break
		}
		releases = append(releases, batch...)
		if len(batch) < 100 {
			break
		}
		page++
	}

	return releases, nil
}
