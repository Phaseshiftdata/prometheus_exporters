package github

import "time"

// How far back the exporter is willing to collect, and why it is not "as far
// as GitHub will let us".
//
// RetentionWindow mirrors migration 003. The raw tables keep ninety days; past
// that, a statement-level trigger rolls each row into its *_daily row and
// deletes it. Note what that makes true of the rollups: they ACCUMULATE, by
// design and with a long comment in 003 saying so, because a raw row reaches
// them exactly once -- in the statement that deletes it.
//
// Which means a run fetched from beyond the window is worse than wasted work.
// Wasted it certainly is: the row is inserted, rolled up, and deleted before
// the poll gets as far as its jobs, and the jobs are then discarded by
// UpsertWorkflowJob's WHERE EXISTS because the run they point at is already
// gone. So the jobs request -- the expensive one, one per run -- buys nothing
// at all. But the run row itself is actively harmful: fetch and insert the same
// pruned run a second time and its day is counted a second time. The old
// collector walked all of history on every cycle, so before this change every
// fifteen minutes re-inflated the daily counts for every day older than ninety.
//
// CollectionHorizon is therefore set INSIDE the retention window rather than
// at it. The margin is not decoration. The prune boundary is evaluated by
// PostgreSQL's NOW() and the fetch boundary by the exporter's clock, on a
// different host; at exactly ninety days on both sides, a few minutes of skew
// -- or a run created within a minute of the boundary -- decides whether a run
// is fetchable and prunable at the same time. A day of margin makes the
// property unconditional: everything the exporter can fetch is newer than
// anything the prune can have consumed, so every insert is idempotent and the
// rollups cannot double-count. One day of history is a cheap price for a
// guarantee that holds without anyone having to reason about clocks.
//
// The horizon is also the answer to "how deep does backfill go". Not "all of
// it": there is nothing to gain past the horizon and a correctness problem in
// trying. History from before the exporter existed is simply not recoverable
// into the rollups, because the rollups can only be built forward from rows
// that pass through the prune once. Pretending otherwise would produce numbers
// that are wrong in the direction that looks like success.
const (
	RetentionWindow = 90 * 24 * time.Hour
	HorizonMargin   = 24 * time.Hour

	// CollectionHorizon is the default age limit for anything fetched from the
	// workflow runs API.
	CollectionHorizon = RetentionWindow - HorizonMargin
)
