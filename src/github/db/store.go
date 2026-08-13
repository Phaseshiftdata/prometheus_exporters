package db

import (
	"context"
	"fmt"
)

// Store wraps a DBPool and provides upsert methods for all GitHub entities.
type Store struct {
	pool DBPool
}

// NewStore creates a new Store from an existing DBPool.
// For production use, pass a *pgxpool.Pool wrapped in a PoolAdapter.
func NewStore(pool DBPool) *Store {
	return &Store{pool: pool}
}

// Pool returns the underlying DBPool for direct queries if needed.
func (s *Store) Pool() DBPool {
	return s.pool
}

// Close is a no-op for the interface-based store. Callers that own the
// underlying pgxpool.Pool should close it directly.
func (s *Store) Close() {}

// UpsertRepository inserts or updates a repository record.
func (s *Store) UpsertRepository(ctx context.Context, repo Repository) error {
	const query = `
		INSERT INTO repositories (id, name, default_branch, visibility, archived, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			default_branch = EXCLUDED.default_branch,
			visibility = EXCLUDED.visibility,
			archived = EXCLUDED.archived,
			updated_at = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, query,
		repo.ID, repo.Name, repo.DefaultBranch, repo.Visibility, repo.Archived, repo.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert repository %d: %w", repo.ID, err)
	}
	return nil
}

// UpsertWorkflowRun inserts or updates a workflow run record.
func (s *Store) UpsertWorkflowRun(ctx context.Context, run WorkflowRun) error {
	const query = `
		INSERT INTO workflow_runs (id, repo, workflow, branch, conclusion, attempt, created_at, run_started_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			repo = EXCLUDED.repo,
			workflow = EXCLUDED.workflow,
			branch = EXCLUDED.branch,
			conclusion = EXCLUDED.conclusion,
			attempt = EXCLUDED.attempt,
			run_started_at = EXCLUDED.run_started_at,
			updated_at = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, query,
		run.ID, run.Repo, run.Workflow, run.Branch, run.Conclusion, run.Attempt,
		run.CreatedAt, run.RunStartedAt, run.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert workflow run %d: %w", run.ID, err)
	}
	return nil
}

// UpsertWorkflowJob inserts or updates a workflow job record.
//
// The WHERE EXISTS is not decoration. The collectors page through the whole of
// a repository's history, so the first poll against an empty database offers
// runs that are already older than the retention window; the statement trigger
// rolls each one up and deletes it before the poller gets as far as its jobs,
// and the job then has no run to point at. Without this guard that is a foreign
// key violation, and because the bulk upsert stops at the first error, one
// ancient run silently costs the entire remaining batch of jobs -- including
// the recent ones. A job whose run has already been compacted has nowhere to
// live and is skipped; its run is already counted in the rollup.
func (s *Store) UpsertWorkflowJob(ctx context.Context, job WorkflowJob) error {
	const query = `
		INSERT INTO workflow_jobs (id, run_id, name, conclusion, started_at, completed_at)
		SELECT $1::bigint, $2::bigint, $3::text, $4::text, $5::timestamptz, $6::timestamptz
		WHERE EXISTS (SELECT 1 FROM workflow_runs WHERE id = $2::bigint)
		ON CONFLICT (id) DO UPDATE SET
			run_id = EXCLUDED.run_id,
			name = EXCLUDED.name,
			conclusion = EXCLUDED.conclusion,
			started_at = EXCLUDED.started_at,
			completed_at = EXCLUDED.completed_at`
	_, err := s.pool.Exec(ctx, query,
		job.ID, job.RunID, job.Name, job.Conclusion, job.StartedAt, job.CompletedAt)
	if err != nil {
		return fmt.Errorf("upsert workflow job %d: %w", job.ID, err)
	}
	return nil
}

// UpsertPullRequest inserts or updates a pull request record.
func (s *Store) UpsertPullRequest(ctx context.Context, pr PullRequest) error {
	const query = `
		INSERT INTO pull_requests (id, repo, number, state, author, created_at, merged_at, closed_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			repo = EXCLUDED.repo,
			number = EXCLUDED.number,
			state = EXCLUDED.state,
			author = EXCLUDED.author,
			merged_at = EXCLUDED.merged_at,
			closed_at = EXCLUDED.closed_at,
			updated_at = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, query,
		pr.ID, pr.Repo, pr.Number, pr.State, pr.Author,
		pr.CreatedAt, pr.MergedAt, pr.ClosedAt, pr.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert pull request %d: %w", pr.ID, err)
	}
	return nil
}

// UpsertCommit inserts or updates a commit record.
func (s *Store) UpsertCommit(ctx context.Context, commit Commit) error {
	const query = `
		INSERT INTO commits (sha, repo, branch, author, message, committed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (sha) DO UPDATE SET
			repo = EXCLUDED.repo,
			branch = EXCLUDED.branch,
			author = EXCLUDED.author,
			message = EXCLUDED.message,
			committed_at = EXCLUDED.committed_at`
	_, err := s.pool.Exec(ctx, query,
		commit.SHA, commit.Repo, commit.Branch, commit.Author, commit.Message, commit.CommittedAt)
	if err != nil {
		return fmt.Errorf("upsert commit %s: %w", commit.SHA, err)
	}
	return nil
}

// UpsertTag inserts or updates a tag record.
func (s *Store) UpsertTag(ctx context.Context, tag Tag) error {
	const query = `
		INSERT INTO tags (repo, name, target_sha, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo, name) DO UPDATE SET
			target_sha = EXCLUDED.target_sha,
			created_at = EXCLUDED.created_at`
	_, err := s.pool.Exec(ctx, query,
		tag.Repo, tag.Name, tag.TargetSHA, tag.CreatedAt)
	if err != nil {
		return fmt.Errorf("upsert tag %s/%s: %w", tag.Repo, tag.Name, err)
	}
	return nil
}

// UpsertQueries returns the raw SQL for each upsert operation, useful for
// testing SQL construction without a live database.
var UpsertQueries = map[string]string{
	"repository": `
		INSERT INTO repositories (id, name, default_branch, visibility, archived, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			default_branch = EXCLUDED.default_branch,
			visibility = EXCLUDED.visibility,
			archived = EXCLUDED.archived,
			updated_at = EXCLUDED.updated_at`,
	"workflow_run": `
		INSERT INTO workflow_runs (id, repo, workflow, branch, conclusion, attempt, created_at, run_started_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			repo = EXCLUDED.repo,
			workflow = EXCLUDED.workflow,
			branch = EXCLUDED.branch,
			conclusion = EXCLUDED.conclusion,
			attempt = EXCLUDED.attempt,
			run_started_at = EXCLUDED.run_started_at,
			updated_at = EXCLUDED.updated_at`,
	"workflow_job": `
		INSERT INTO workflow_jobs (id, run_id, name, conclusion, started_at, completed_at)
		SELECT $1::bigint, $2::bigint, $3::text, $4::text, $5::timestamptz, $6::timestamptz
		WHERE EXISTS (SELECT 1 FROM workflow_runs WHERE id = $2::bigint)
		ON CONFLICT (id) DO UPDATE SET
			run_id = EXCLUDED.run_id,
			name = EXCLUDED.name,
			conclusion = EXCLUDED.conclusion,
			started_at = EXCLUDED.started_at,
			completed_at = EXCLUDED.completed_at`,
	"pull_request": `
		INSERT INTO pull_requests (id, repo, number, state, author, created_at, merged_at, closed_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			repo = EXCLUDED.repo,
			number = EXCLUDED.number,
			state = EXCLUDED.state,
			author = EXCLUDED.author,
			merged_at = EXCLUDED.merged_at,
			closed_at = EXCLUDED.closed_at,
			updated_at = EXCLUDED.updated_at`,
	"commit": `
		INSERT INTO commits (sha, repo, branch, author, message, committed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (sha) DO UPDATE SET
			repo = EXCLUDED.repo,
			branch = EXCLUDED.branch,
			author = EXCLUDED.author,
			message = EXCLUDED.message,
			committed_at = EXCLUDED.committed_at`,
	"tag": `
		INSERT INTO tags (repo, name, target_sha, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo, name) DO UPDATE SET
			target_sha = EXCLUDED.target_sha,
			created_at = EXCLUDED.created_at`,
}
