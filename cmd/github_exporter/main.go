// github_exporter polls the GitHub API for CI/CD, PR, commit, and release
// data across all phaseshiftdata repositories. It writes records to
// PostgreSQL and exposes aggregate Prometheus metrics.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	ghpkg "github.com/phaseshiftdata/prometheus_exporters/src/github"
	ghdb "github.com/phaseshiftdata/prometheus_exporters/src/github/db"
	"github.com/phaseshiftdata/prometheus_exporters/src/secrets"
	"github.com/phaseshiftdata/prometheus_exporters/src/version"
)

func main() {
	os.Exit(execute())
}

func execute() int {
	if err := rootCmd().Execute(); err != nil {
		return 1
	}
	return 0
}

func rootCmd() *cobra.Command {
	var (
		listenAddr           string
		databaseURL          string
		databasePasswordFile string
		appID                int64
		installID            int64
		keyFile              string
		pollInterval         time.Duration
		org                  string
		logLevel             string
		jobBudget            int
		backfillInterval     time.Duration
		backfillMinRateLeft  int
	)

	cmd := &cobra.Command{
		Use:     "github_exporter",
		Short:   "Prometheus exporter for GitHub CI/CD and repository statistics",
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version.Version, version.GitCommit, version.BuildDate),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedDatabaseURL := databaseURL
			if databasePasswordFile != "" {
				if resolvedDatabaseURL == "" {
					resolvedDatabaseURL = os.Getenv("DATABASE_URL")
				}
				if resolvedDatabaseURL == "" {
					return fmt.Errorf("--database-password-file requires --database-url or DATABASE_URL to be set")
				}
				pw, err := secrets.ReadSecretFile(databasePasswordFile)
				if err != nil {
					return fmt.Errorf("--database-password-file: %w", err)
				}
				parsed, err := url.Parse(resolvedDatabaseURL)
				if err != nil {
					return fmt.Errorf("parsing --database-url for password substitution: %w", err)
				}
				// Check that the URL does not already carry a password.
				if _, alreadySet := parsed.User.Password(); alreadySet {
					return fmt.Errorf("--database-password-file and a password in --database-url are mutually exclusive")
				}
				user := parsed.User.Username()
				parsed.User = url.UserPassword(user, pw)
				resolvedDatabaseURL = parsed.String()
			}

			return run(cmd.Context(), runConfig{
				listenAddr:          listenAddr,
				databaseURL:         resolvedDatabaseURL,
				appID:               appID,
				installID:           installID,
				keyFile:             keyFile,
				pollInterval:        pollInterval,
				org:                 org,
				logLevel:            logLevel,
				jobBudget:           jobBudget,
				backfillInterval:    backfillInterval,
				backfillMinRateLeft: backfillMinRateLeft,
			})
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen-address", "127.0.0.1:9102", "Address to listen on for metrics")
	cmd.Flags().StringVar(&databaseURL, "database-url", "", "PostgreSQL connection string (or DATABASE_URL env)")
	cmd.Flags().StringVar(&databasePasswordFile, "database-password-file", "", "Path to file containing the database password (substituted into --database-url)")
	cmd.Flags().Int64Var(&appID, "github-app-id", 0, "GitHub App ID")
	cmd.Flags().Int64Var(&installID, "github-install-id", 0, "GitHub App Installation ID")
	cmd.Flags().StringVar(&keyFile, "github-key-file", "", "Path to GitHub App private key PEM")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 5*time.Minute, "Polling interval")
	cmd.Flags().StringVar(&org, "org", "", "GitHub organization")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	// The three below are the pacing controls. They exist as flags rather than
	// constants because the right values depend on how many repositories the
	// installation has and how much of the App's quota it is willing to spend
	// on history, and because on 2026-08-11 the only lever available was the
	// poll interval -- which changes how often the exporter falls over, not
	// whether it does.
	cmd.Flags().IntVar(&jobBudget, "job-budget-per-poll", ghpkg.DefaultJobBudgetPerPoll,
		"Maximum workflow-jobs requests one poll cycle may issue across all repos")
	cmd.Flags().DurationVar(&backfillInterval, "backfill-interval", ghpkg.DefaultBackfillInterval,
		"Minimum spacing between historical backfill requests")
	cmd.Flags().IntVar(&backfillMinRateLeft, "backfill-min-rate-limit", ghpkg.DefaultBackfillMinRateLimit,
		"Pause backfill while the remaining GitHub rate limit is below this")

	return cmd
}

type runConfig struct {
	listenAddr          string
	databaseURL         string
	appID               int64
	installID           int64
	keyFile             string
	pollInterval        time.Duration
	org                 string
	logLevel            string
	jobBudget           int
	backfillInterval    time.Duration
	backfillMinRateLeft int
	// testPool overrides database connection for testing. When set,
	// Connect and RunMigrations are skipped.
	testPool ghdb.DBPool
}

func run(ctx context.Context, cfg runConfig) error {
	setupLogging(cfg.logLevel)

	// Resolve database URL from flag or environment.
	dbURL := cfg.databaseURL
	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}

	// Set up GitHub auth.
	auth, err := ghpkg.NewAuth(cfg.appID, cfg.installID, cfg.keyFile)
	if err != nil {
		return fmt.Errorf("github auth: %w", err)
	}

	client := ghpkg.NewClient(auth)

	// Set up Prometheus metrics.
	reg := prometheus.NewRegistry()
	metrics := ghpkg.NewMetrics(reg)

	// Being throttled becomes a number rather than only a log line, and it
	// carries WHICH limit did it. The two want opposite responses -- a
	// secondary throttle wants a few seconds and slower pacing, primary
	// exhaustion wants the hour -- and on 2026-08-11 they were told apart by
	// hand, with a freshly minted token and a curl, after the fact.
	client.SetRateLimitObserver(func(kind string, _ time.Duration) {
		metrics.RateLimitedTotal.WithLabelValues(kind).Inc()
	})

	// Start the poller if database is configured.
	if dbURL != "" || cfg.testPool != nil {
		var pool ghdb.DBPool
		if cfg.testPool != nil {
			pool = cfg.testPool
		} else {
			pgPool, err := ghdb.Connect(ctx, dbURL)
			if err != nil {
				return fmt.Errorf("connecting to database: %w", err)
			}
			defer pgPool.Close()
			pool = pgPool

			slog.Info("running database migrations")
			if err := ghdb.RunMigrations(ctx, pool); err != nil {
				return fmt.Errorf("running migrations: %w", err)
			}
		}

		store := &storeAdapter{ghdb.NewStore(pool)}

		// Two loops, deliberately. The poller keeps recent data fresh and must
		// finish quickly; the backfiller walks history at a bounded request
		// rate and may take hours. They were one loop until 2026-08-11, when
		// the historical walk -- one request per workflow run for its jobs,
		// over every run in every repository, restarted from scratch every
		// cycle -- tripped GitHub's secondary rate limit and the first poll
		// never finished.
		poller := ghpkg.NewPoller(client, cfg.org, cfg.pollInterval, metrics,
			ghpkg.WithJobBudget(cfg.jobBudget))
		go func() {
			if err := poller.Run(ctx, store); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("poller stopped with error", "error", err)
			}
		}()

		backfiller := ghpkg.NewBackfiller(client, cfg.org, metrics,
			ghpkg.WithBackfillInterval(cfg.backfillInterval),
			ghpkg.WithBackfillMinRateLimit(cfg.backfillMinRateLeft))
		go func() {
			if err := backfiller.Run(ctx, store); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("backfiller stopped with error", "error", err)
			}
		}()
	} else {
		slog.Warn("no database URL configured; running metrics-only mode")
	}

	return serve(ctx, cfg.listenAddr, reg)
}

func serve(ctx context.Context, listenAddr string, reg *prometheus.Registry) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><title>GitHub Exporter</title></head>
<body><h1>GitHub Exporter</h1><p><a href="/metrics">Metrics</a></p></body></html>`)
	})

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown error", "error", err)
		}
	}()

	slog.Info("starting github_exporter", "address", listenAddr, "version", version.Version)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}

func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}

// storeAdapter wraps db.Store to satisfy github.StoreWriter by converting
// between the github and db type systems.
type storeAdapter struct {
	store *ghdb.Store
}

func (a *storeAdapter) UpsertRepositories(ctx context.Context, repos []ghpkg.Repository) error {
	for _, r := range repos {
		updatedAt := r.UpdatedAt
		if err := a.store.UpsertRepository(ctx, ghdb.Repository{
			ID: r.ID, Name: r.Name, DefaultBranch: r.DefaultBranch,
			Visibility: r.Visibility, Archived: r.Archived, UpdatedAt: &updatedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *storeAdapter) UpsertWorkflowRuns(ctx context.Context, runs []ghpkg.WorkflowRun) error {
	for _, r := range runs {
		if err := a.store.UpsertWorkflowRun(ctx, ghdb.WorkflowRun{
			ID: r.ID, Repo: r.Repo, Workflow: r.Workflow, Branch: r.Branch,
			Conclusion: r.Conclusion, Attempt: r.Attempt, CreatedAt: r.CreatedAt,
			RunStartedAt: r.RunStartedAt, UpdatedAt: r.UpdatedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *storeAdapter) UpsertWorkflowJobs(ctx context.Context, jobs []ghpkg.WorkflowJob) error {
	for _, j := range jobs {
		if err := a.store.UpsertWorkflowJob(ctx, ghdb.WorkflowJob{
			ID: j.ID, RunID: j.RunID, Name: j.Name, Conclusion: j.Conclusion,
			StartedAt: j.StartedAt, CompletedAt: j.CompletedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *storeAdapter) UpsertPullRequests(ctx context.Context, prs []ghpkg.PullRequest) error {
	for _, pr := range prs {
		if err := a.store.UpsertPullRequest(ctx, ghdb.PullRequest{
			ID: pr.ID, Repo: pr.Repo, Number: pr.Number, State: pr.State,
			Author: pr.Author, CreatedAt: pr.CreatedAt, MergedAt: pr.MergedAt,
			ClosedAt: pr.ClosedAt, UpdatedAt: pr.UpdatedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *storeAdapter) UpsertCommits(ctx context.Context, commits []ghpkg.Commit) error {
	for _, c := range commits {
		if err := a.store.UpsertCommit(ctx, ghdb.Commit{
			SHA: c.SHA, Repo: c.Repo, Branch: c.Branch, Author: c.Author,
			Message: c.Message, CommittedAt: c.CommittedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *storeAdapter) SampleOpenPullRequests(ctx context.Context, day time.Time) error {
	return a.store.SampleOpenPullRequests(ctx, day)
}

// The methods below are the ones that make a cold start cheap. They carry the
// question "have we already got this?" across the type boundary so that the
// collectors can decline to ask GitHub -- and they carry the backfiller's
// progress the other way, into PostgreSQL, so that a restart resumes the
// historical walk instead of starting the burst again.

func (a *storeAdapter) SelectRunsForJobFetch(
	ctx context.Context, candidates []ghpkg.RunJobSync,
) ([]int64, error) {
	dbCandidates := make([]ghdb.RunJobSync, len(candidates))
	for i, c := range candidates {
		dbCandidates[i] = ghdb.RunJobSync{RunID: c.RunID, SyncKey: c.SyncKey}
	}
	return a.store.SelectRunsForJobFetch(ctx, dbCandidates)
}

func (a *storeAdapter) MarkJobsSynced(ctx context.Context, runID int64, syncKey time.Time) error {
	return a.store.MarkJobsSynced(ctx, runID, syncKey)
}

func (a *storeAdapter) RunsMissingJobs(
	ctx context.Context, repo string, limit int,
) ([]ghpkg.RunJobSync, error) {
	rows, err := a.store.RunsMissingJobs(ctx, repo, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ghpkg.RunJobSync, len(rows))
	for i, r := range rows {
		out[i] = ghpkg.RunJobSync{RunID: r.RunID, SyncKey: r.SyncKey}
	}
	return out, nil
}

func (a *storeAdapter) ListRepositoryNames(ctx context.Context) ([]string, error) {
	return a.store.ListRepositoryNames(ctx)
}

func (a *storeAdapter) LoadBackfillProgress(
	ctx context.Context, repo string,
) (ghpkg.BackfillProgress, error) {
	p, err := a.store.LoadBackfillProgress(ctx, repo)
	if err != nil {
		return ghpkg.BackfillProgress{}, err
	}
	return ghpkg.BackfillProgress{
		Repo: p.Repo, NextPage: p.NextPage, RunsComplete: p.RunsComplete,
		CompletedAt: p.CompletedAt, PagesFetched: p.PagesFetched,
		RequestsSpent: p.RequestsSpent,
	}, nil
}

func (a *storeAdapter) SaveBackfillProgress(ctx context.Context, p ghpkg.BackfillProgress) error {
	return a.store.SaveBackfillProgress(ctx, ghdb.BackfillProgress{
		Repo: p.Repo, NextPage: p.NextPage, RunsComplete: p.RunsComplete,
		CompletedAt: p.CompletedAt, PagesFetched: p.PagesFetched,
		RequestsSpent: p.RequestsSpent,
	})
}

func (a *storeAdapter) BackfillStats(ctx context.Context) (ghpkg.BackfillStats, error) {
	s, err := a.store.BackfillStats(ctx)
	if err != nil {
		return ghpkg.BackfillStats{}, err
	}
	return ghpkg.BackfillStats{
		PendingJobRuns: s.PendingJobRuns,
		ReposComplete:  s.ReposComplete,
	}, nil
}

func (a *storeAdapter) UpsertTags(ctx context.Context, tags []ghpkg.Tag) error {
	for _, t := range tags {
		if err := a.store.UpsertTag(ctx, ghdb.Tag{
			Repo: t.Repo, Name: t.Name, TargetSHA: t.TargetSHA, CreatedAt: t.CreatedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}
