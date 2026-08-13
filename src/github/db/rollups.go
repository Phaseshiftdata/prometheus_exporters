package db

import (
	"context"
	"fmt"
	"time"
)

// Everything in the daily rollups is a sum of events except one column, and the
// prune triggers in migration 003 own all of the sums. They can, because a raw
// row reaches the rollup exactly once -- in the statement that deletes it -- so
// adding is exact. Recomputing those same columns from here would be a second
// writer that cannot tell a day it has already counted from one it has not, and
// would quietly double or erase the history it was meant to protect.
//
// open_at_eod is the column no trigger can produce. The number of pull requests
// open at the end of a day is a level, not a total: it cannot be recovered from
// opened and merged counts, and once the raw rows are pruned it cannot be
// recovered at all. So it is measured while the rows are still there, stored per
// day, and kept for as long as the rollup is -- which is forever.

// SampleOpenPullRequests records how many pull requests stood open at the end of
// the given day, per repository, and stores it in pull_requests_daily.
//
// It reads the answer out of the raw timestamps rather than counting rows whose
// state column currently says "open", so re-sampling a day that has already
// passed returns the same number it would have returned at the time. That is
// what makes it safe to call for yesterday as well as today: a poll that lands
// after midnight still closes out the day that just ended, instead of stamping
// this morning's open set onto it.
//
// Repositories with nothing open are written as an explicit zero. A missing row
// and a row saying zero look identical on a graph but mean different things --
// "nobody measured" and "nobody had a pull request open".
func (s *Store) SampleOpenPullRequests(ctx context.Context, day time.Time) error {
	dayStr := day.Format("2006-01-02")
	if _, err := s.pool.Exec(ctx, sampleOpenPullRequestsSQL, dayStr); err != nil {
		return fmt.Errorf("sampling open pull requests for %s: %w", dayStr, err)
	}
	return nil
}

// SampleOpenPullRequestsSQL is exported so tests can assert on the statement
// without a live database.
const SampleOpenPullRequestsSQL = sampleOpenPullRequestsSQL

const sampleOpenPullRequestsSQL = `
	INSERT INTO pull_requests_daily (day, repo, open_at_eod)
	SELECT
		$1::date,
		r.name,
		COUNT(pr.id)
	FROM repositories r
	LEFT JOIN pull_requests pr
		ON pr.repo = r.name
		AND pr.created_at < ($1::date + INTERVAL '1 day')
		AND (pr.closed_at IS NULL OR pr.closed_at >= ($1::date + INTERVAL '1 day'))
	GROUP BY r.name
	ON CONFLICT (day, repo) DO UPDATE SET
		open_at_eod = EXCLUDED.open_at_eod`
