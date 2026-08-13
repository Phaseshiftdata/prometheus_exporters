package db

import "context"

// Bulk upsert methods that satisfy the github.StoreWriter interface.
// Each iterates the slice and calls the single-record upsert.

// UpsertRepositories upserts a batch of repositories.
func (s *Store) UpsertRepositories(ctx context.Context, repos []Repository) error {
	for _, r := range repos {
		if err := s.UpsertRepository(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// UpsertWorkflowRuns upserts a batch of workflow runs.
func (s *Store) UpsertWorkflowRuns(ctx context.Context, runs []WorkflowRun) error {
	for _, r := range runs {
		if err := s.UpsertWorkflowRun(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// UpsertWorkflowJobs upserts a batch of workflow jobs.
func (s *Store) UpsertWorkflowJobs(ctx context.Context, jobs []WorkflowJob) error {
	for _, j := range jobs {
		if err := s.UpsertWorkflowJob(ctx, j); err != nil {
			return err
		}
	}
	return nil
}

// UpsertPullRequests upserts a batch of pull requests.
func (s *Store) UpsertPullRequests(ctx context.Context, prs []PullRequest) error {
	for _, pr := range prs {
		if err := s.UpsertPullRequest(ctx, pr); err != nil {
			return err
		}
	}
	return nil
}

// UpsertCommits upserts a batch of commits.
func (s *Store) UpsertCommits(ctx context.Context, commits []Commit) error {
	for _, c := range commits {
		if err := s.UpsertCommit(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// UpsertTags upserts a batch of tags.
func (s *Store) UpsertTags(ctx context.Context, tags []Tag) error {
	for _, t := range tags {
		if err := s.UpsertTag(ctx, t); err != nil {
			return err
		}
	}
	return nil
}
