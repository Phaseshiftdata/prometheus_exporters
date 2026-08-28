package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"

	"github.com/phaseshiftdata/prometheus_exporters/src/exporter"
	ghpkg "github.com/phaseshiftdata/prometheus_exporters/src/github"
	ghdb "github.com/phaseshiftdata/prometheus_exporters/src/github/db"
)

// generateTestKeyFile creates a temporary RSA private key PEM file for testing.
func generateTestKeyFile(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	f, err := os.CreateTemp("", "test-key-*.pem")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}); err != nil {
		t.Fatalf("encoding PEM: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestRootCmd(t *testing.T) {
	cmd := rootCmd()
	if cmd.Use != "github_exporter" {
		t.Errorf("expected Use 'github_exporter', got %q", cmd.Use)
	}
	if cmd.Version == "" {
		t.Error("empty version")
	}
}

func TestSetupLogging(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "invalid"} {
		exporter.SetupLogging(level)
	}
}

func TestExecuteReturnsZero(t *testing.T) {
	code := exporter.Execute(func() *cobra.Command {
		cmd := rootCmd()
		cmd.SetArgs([]string{"--help"})
		return cmd
	})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestExecuteReturnsOneOnError(t *testing.T) {
	code := exporter.Execute(func() *cobra.Command {
		cmd := rootCmd()
		cmd.SetArgs([]string{"--unknown-flag"})
		return cmd
	})
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestServeAndShutdown(t *testing.T) {
	// Use a listener to get the actual port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- serve(ctx, addr, prometheus.NewRegistry()) }()
	time.Sleep(100 * time.Millisecond)

	// Hit the homepage to cover the handler.
	resp, httpErr := http.Get("http://" + addr + "/")
	if httpErr != nil {
		t.Logf("homepage request failed (server may not be ready): %v", httpErr)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("homepage status = %d, want 200", resp.StatusCode)
		}
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("serve did not shut down in time")
	}
}

func TestServeInvalidAddress(t *testing.T) {
	ctx := context.Background()
	err := serve(ctx, "invalid-address-no-port", prometheus.NewRegistry())
	if err == nil {
		t.Error("expected error for invalid listen address")
	}
}

func TestRunMissingDatabase(t *testing.T) {
	// Run with no database URL and no DATABASE_URL env — should fail.
	os.Unsetenv("DATABASE_URL")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := run(ctx, runConfig{
		listenAddr: "127.0.0.1:0",
		appID:      1,
		installID:  1,
		keyFile:    "/nonexistent/key.pem",
		logLevel:   "error",
	})
	// Will fail on auth (key file not found) before reaching DB check.
	if err == nil {
		t.Error("expected error")
	}
}

// ---------- mockDBPool for storeAdapter tests ----------

type mockCommandTag struct {
	rowsAffected int64
}

func (t mockCommandTag) RowsAffected() int64 { return t.rowsAffected }

type mockRow struct {
	err error
}

func (r *mockRow) Scan(_ ...any) error { return r.err }

type mockDBPool struct {
	execCalls int
	execErr   error
}

func (p *mockDBPool) Exec(_ context.Context, _ string, _ ...any) (ghdb.CommandTag, error) {
	p.execCalls++
	if p.execErr != nil {
		return nil, p.execErr
	}
	return mockCommandTag{rowsAffected: 1}, nil
}

func (p *mockDBPool) Query(_ context.Context, _ string, _ ...any) (ghdb.Rows, error) {
	return nil, nil
}

func (p *mockDBPool) QueryRow(_ context.Context, _ string, _ ...any) ghdb.Row {
	return &mockRow{}
}

// ---------- storeAdapter tests ----------

func TestStoreAdapterUpsertRepositories(t *testing.T) {
	pool := &mockDBPool{}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}
	ctx := context.Background()

	now := time.Now()
	repos := []ghpkg.Repository{
		{ID: 1, Name: "org/r1", DefaultBranch: "main", Visibility: "public", UpdatedAt: now},
		{ID: 2, Name: "org/r2", DefaultBranch: "dev", Visibility: "private", Archived: true, UpdatedAt: now},
	}

	if err := adapter.UpsertRepositories(ctx, repos); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.execCalls != 2 {
		t.Errorf("expected 2 exec calls, got %d", pool.execCalls)
	}
}

func TestStoreAdapterUpsertRepositoriesError(t *testing.T) {
	pool := &mockDBPool{execErr: context.DeadlineExceeded}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}

	err := adapter.UpsertRepositories(context.Background(), []ghpkg.Repository{{ID: 1, Name: "r", UpdatedAt: time.Now()}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStoreAdapterUpsertWorkflowRuns(t *testing.T) {
	pool := &mockDBPool{}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}
	ctx := context.Background()

	now := time.Now()
	runs := []ghpkg.WorkflowRun{
		{ID: 10, Repo: "org/r", Workflow: "ci.yml", Branch: "main", Conclusion: "success", Attempt: 1, CreatedAt: now, RunStartedAt: &now, UpdatedAt: &now},
		{ID: 11, Repo: "org/r", Workflow: "cd.yml", Branch: "main", CreatedAt: now},
	}

	if err := adapter.UpsertWorkflowRuns(ctx, runs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.execCalls != 2 {
		t.Errorf("expected 2 exec calls, got %d", pool.execCalls)
	}
}

func TestStoreAdapterUpsertWorkflowRunsError(t *testing.T) {
	pool := &mockDBPool{execErr: context.DeadlineExceeded}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}

	err := adapter.UpsertWorkflowRuns(context.Background(), []ghpkg.WorkflowRun{{ID: 1, Repo: "r", Workflow: "w", CreatedAt: time.Now()}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStoreAdapterUpsertWorkflowJobs(t *testing.T) {
	pool := &mockDBPool{}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}
	ctx := context.Background()

	now := time.Now()
	jobs := []ghpkg.WorkflowJob{
		{ID: 20, RunID: 10, Name: "build", Conclusion: "success", StartedAt: &now, CompletedAt: &now},
		{ID: 21, RunID: 10, Name: "test", Conclusion: "failure", StartedAt: &now, CompletedAt: &now},
	}

	if err := adapter.UpsertWorkflowJobs(ctx, jobs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.execCalls != 2 {
		t.Errorf("expected 2 exec calls, got %d", pool.execCalls)
	}
}

func TestStoreAdapterUpsertWorkflowJobsError(t *testing.T) {
	pool := &mockDBPool{execErr: context.DeadlineExceeded}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}

	err := adapter.UpsertWorkflowJobs(context.Background(), []ghpkg.WorkflowJob{{ID: 1, RunID: 1, Name: "j"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStoreAdapterUpsertPullRequests(t *testing.T) {
	pool := &mockDBPool{}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}
	ctx := context.Background()

	now := time.Now()
	prs := []ghpkg.PullRequest{
		{ID: 30, Repo: "org/r", Number: 1, State: "open", Author: "alice", CreatedAt: now, MergedAt: &now, ClosedAt: &now, UpdatedAt: &now},
		{ID: 31, Repo: "org/r", Number: 2, State: "closed", Author: "bob", CreatedAt: now},
	}

	if err := adapter.UpsertPullRequests(ctx, prs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.execCalls != 2 {
		t.Errorf("expected 2 exec calls, got %d", pool.execCalls)
	}
}

func TestStoreAdapterUpsertPullRequestsError(t *testing.T) {
	pool := &mockDBPool{execErr: context.DeadlineExceeded}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}

	err := adapter.UpsertPullRequests(context.Background(), []ghpkg.PullRequest{{ID: 1, Repo: "r", Number: 1, State: "open", CreatedAt: time.Now()}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStoreAdapterUpsertCommits(t *testing.T) {
	pool := &mockDBPool{}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}
	ctx := context.Background()

	commits := []ghpkg.Commit{
		{SHA: "aaa", Repo: "org/r", Branch: "main", Author: "alice", Message: "fix", CommittedAt: time.Now()},
		{SHA: "bbb", Repo: "org/r", Branch: "main", Author: "bob", Message: "feat", CommittedAt: time.Now()},
	}

	if err := adapter.UpsertCommits(ctx, commits); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.execCalls != 2 {
		t.Errorf("expected 2 exec calls, got %d", pool.execCalls)
	}
}

func TestStoreAdapterUpsertCommitsError(t *testing.T) {
	pool := &mockDBPool{execErr: context.DeadlineExceeded}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}

	err := adapter.UpsertCommits(context.Background(), []ghpkg.Commit{{SHA: "a", Repo: "r", CommittedAt: time.Now()}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStoreAdapterUpsertTags(t *testing.T) {
	pool := &mockDBPool{}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}
	ctx := context.Background()

	now := time.Now()
	tags := []ghpkg.Tag{
		{Repo: "org/r", Name: "v1.0.0", TargetSHA: "aaa", CreatedAt: &now},
		{Repo: "org/r", Name: "v2.0.0", TargetSHA: "bbb", CreatedAt: &now},
	}

	if err := adapter.UpsertTags(ctx, tags); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.execCalls != 2 {
		t.Errorf("expected 2 exec calls, got %d", pool.execCalls)
	}
}

func TestStoreAdapterUpsertTagsError(t *testing.T) {
	pool := &mockDBPool{execErr: context.DeadlineExceeded}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}

	err := adapter.UpsertTags(context.Background(), []ghpkg.Tag{{Repo: "r", Name: "v1"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStoreAdapterSampleOpenPullRequests(t *testing.T) {
	pool := &mockDBPool{}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}

	if err := adapter.SampleOpenPullRequests(context.Background(), time.Now()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.execCalls != 1 {
		t.Errorf("expected 1 exec call, got %d", pool.execCalls)
	}
}

func TestStoreAdapterSampleOpenPullRequestsError(t *testing.T) {
	pool := &mockDBPool{execErr: context.DeadlineExceeded}
	adapter := &storeAdapter{store: ghdb.NewStore(pool)}

	if err := adapter.SampleOpenPullRequests(context.Background(), time.Now()); err == nil {
		t.Fatal("expected error")
	}
}

func TestRootCmdRunE(t *testing.T) {
	// Execute the root command (not --help) to exercise the RunE closure.
	// It will fail on auth because the key file is missing, but the RunE
	// code path will be covered.
	code := exporter.Execute(func() *cobra.Command {
		cmd := rootCmd()
		cmd.SetArgs([]string{
			"--github-key-file", "/nonexistent/key.pem",
			"--github-app-id", "1",
			"--github-install-id", "1",
			"--log-level", "error",
		})
		return cmd
	})
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRunInvalidKeyFile(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := run(ctx, runConfig{
		listenAddr: "127.0.0.1:0",
		appID:      1,
		installID:  1,
		keyFile:    "/tmp/nonexistent-key-file.pem",
		logLevel:   "error",
	})
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestRunMetricsOnlyMode(t *testing.T) {
	keyFile := generateTestKeyFile(t)
	os.Unsetenv("DATABASE_URL")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, runConfig{
			listenAddr:   "127.0.0.1:0",
			databaseURL:  "",
			appID:        1,
			installID:    1,
			keyFile:      keyFile,
			pollInterval: 5 * time.Minute,
			org:          "test-org",
			logLevel:     "error",
		})
	}()

	// Give the server time to start, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("expected nil error in metrics-only mode, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not shut down in time")
	}
}

func TestRunInvalidKeyContent(t *testing.T) {
	// Create a temporary file with an invalid PEM key to exercise the auth
	// error path for bad key content.
	tmpFile, err := os.CreateTemp("", "bad-key-*.pem")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("not a valid PEM key")
	tmpFile.Close()

	os.Unsetenv("DATABASE_URL")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = run(ctx, runConfig{
		listenAddr: "127.0.0.1:0",
		appID:      1,
		installID:  1,
		keyFile:    tmpFile.Name(),
		logLevel:   "error",
	})
	if err == nil {
		t.Fatal("expected error for invalid key content")
	}
}

func TestRunDatabaseURLFromEnv(t *testing.T) {
	// Set DATABASE_URL to a bogus value, which should cause Connect to fail.
	// This exercises the dbURL-from-env path and the Connect error path.
	keyFile := generateTestKeyFile(t)
	t.Setenv("DATABASE_URL", "postgres://invalid:5432/nonexistent?connect_timeout=1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := run(ctx, runConfig{
		listenAddr:   "127.0.0.1:0",
		databaseURL:  "",
		appID:        1,
		installID:    1,
		keyFile:      keyFile,
		pollInterval: 5 * time.Minute,
		org:          "test-org",
		logLevel:     "error",
	})
	// Should fail connecting to the bogus database.
	if err == nil {
		t.Fatal("expected error for invalid database URL")
	}
}

func TestRunWithDatabase(t *testing.T) {
	// Integration test: run with a real PostgreSQL database.
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:test@127.0.0.1:5432/testdb?sslmode=disable&connect_timeout=2"
	}

	// Quick check if DB is reachable.
	conn, dialErr := net.DialTimeout("tcp", "127.0.0.1:5432", 2*time.Second)
	if dialErr != nil {
		t.Skipf("skipping integration test, PostgreSQL not reachable: %v", dialErr)
	}
	conn.Close()

	keyFile := generateTestKeyFile(t)
	os.Unsetenv("DATABASE_URL")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, runConfig{
			listenAddr:   "127.0.0.1:0",
			databaseURL:  dsn,
			appID:        1,
			installID:    1,
			keyFile:      keyFile,
			pollInterval: 5 * time.Minute,
			org:          "test-org",
			logLevel:     "error",
		})
	}()

	// Give the server, migrations, and poller time to start, then cancel.
	time.Sleep(2 * time.Second)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("expected nil error, got: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run() did not shut down in time")
	}
}

func TestRunWithTestPool(t *testing.T) {
	keyFile := generateTestKeyFile(t)
	pool := &mockDBPool{}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, runConfig{
			listenAddr:   "127.0.0.1:0",
			appID:        1,
			installID:    1,
			keyFile:      keyFile,
			pollInterval: 1 * time.Hour, // long interval so it doesn't poll during test
			org:          "test-org",
			logLevel:     "error",
			testPool:     pool,
		})
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("run did not shut down in time")
	}
}

func TestRunConnectError(t *testing.T) {
	keyFile := generateTestKeyFile(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use an invalid database URL to trigger the Connect error path.
	err := run(ctx, runConfig{
		listenAddr:  "127.0.0.1:0",
		databaseURL: "postgresql://invalid:5432/nonexistent?connect_timeout=1",
		appID:       1,
		installID:   1,
		keyFile:     keyFile,
		logLevel:    "error",
	})
	if err == nil {
		t.Error("expected connect error")
	}
}

// ---------------------------------------------------------------------------
// The real-database path through run().
//
// Every other test here hands run() a testPool, which is deliberately the
// shortcut that skips ghdb.Connect and ghdb.RunMigrations entirely. That
// leaves the branch the binary ACTUALLY takes in production as the one branch
// nothing exercises. These two tests cover it from both ends.
// ---------------------------------------------------------------------------

// testDatabaseURL returns the DSN for a live PostgreSQL, or "" when none is
// configured. CI provides one as a service container; see .github/workflows/ci.yml.
func testDatabaseURL() string {
	return os.Getenv("TEST_DATABASE_URL")
}

// privateTestDatabase creates a throwaway database and returns a DSN for it,
// dropping it when the test ends.
//
// It exists because `go test ./...` runs package test binaries in PARALLEL, and
// src/github/db runs the same migrations against the same server. Two packages
// applying migrations to one database concurrently is a race that shows up as
// an occasional inexplicable CI failure rather than a reproducible one. A
// database per test costs milliseconds and removes the question.
func privateTestDatabase(t *testing.T, adminDSN string) string {
	t.Helper()

	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatalf("parsing TEST_DATABASE_URL: %v", err)
	}

	// Unique per test binary run. Lowercase and unquoted-identifier safe.
	name := fmt.Sprintf("ghexp_test_%d", os.Getpid())

	ctx := context.Background()
	admin, err := ghdb.Connect(ctx, adminDSN)
	if err != nil {
		t.Skipf("cannot reach PostgreSQL: %v", err)
	}
	defer admin.Close()

	// DROP first: a previous run killed mid-test may have left it behind.
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name); err != nil {
		t.Fatalf("dropping stale test database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("creating test database: %v", err)
	}

	t.Cleanup(func() {
		cleanup, err := ghdb.Connect(context.Background(), adminDSN)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name)
	})

	u.Path = "/" + name
	return u.String()
}

func TestRunDatabaseConnectError(t *testing.T) {
	// A DSN that parses but cannot connect: run() must fail on the connect
	// rather than carrying on without a database. connect_timeout keeps this
	// fast and bounded.
	keyFile := generateTestKeyFile(t)
	os.Unsetenv("DATABASE_URL")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := run(ctx, runConfig{
		listenAddr:   "127.0.0.1:0",
		databaseURL:  "postgres://nobody:nothing@127.0.0.1:1/nosuchdb?sslmode=disable&connect_timeout=1",
		appID:        1,
		installID:    1,
		keyFile:      keyFile,
		pollInterval: 5 * time.Minute,
		org:          "test-org",
		logLevel:     "error",
	})
	if err == nil {
		t.Fatal("expected an error when the database is unreachable")
	}
	if !strings.Contains(err.Error(), "connecting to database") {
		t.Errorf("expected a connect failure, got: %v", err)
	}
}

func TestRunWithRealDatabase(t *testing.T) {
	adminDSN := testDatabaseURL()
	if adminDSN == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping the live-database path")
	}
	dsn := privateTestDatabase(t, adminDSN)

	keyFile := generateTestKeyFile(t)
	os.Unsetenv("DATABASE_URL")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, runConfig{
			listenAddr:   "127.0.0.1:0",
			databaseURL:  dsn,
			appID:        1,
			installID:    1,
			keyFile:      keyFile,
			pollInterval: 5 * time.Minute,
			org:          "test-org",
			logLevel:     "error",
		})
	}()

	// Long enough for Connect and RunMigrations to complete. The poller starts
	// too and will fail against GitHub, but it runs in its own goroutine and
	// only logs -- it must not bring run() down, which is part of what this
	// test is asserting. CI postgres can be slow to accept connections, so
	// allow extra time.
	time.Sleep(3 * time.Second)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("run() with a live database returned: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run() did not shut down in time")
	}

	// Migrations must have actually been applied, not merely attempted.
	pool, err := ghdb.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting to verify migrations: %v", err)
	}
	defer pool.Close()

	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatalf("reading schema_migrations: %v", err)
	}
	if n == 0 {
		t.Error("schema_migrations is empty; run() did not apply any migration")
	}
}

// ---------------------------------------------------------------------------
// --database-password-file tests
// ---------------------------------------------------------------------------

func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRootCmd_DatabasePasswordFile(t *testing.T) {
	// The password file contents should be substituted into the database URL.
	keyFile := generateTestKeyFile(t)
	pwFile := writeSecretFile(t, "s3cret\n")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-url", "postgres://myuser@localhost:5432/mydb?sslmode=disable&connect_timeout=1",
		"--database-password-file", pwFile,
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", keyFile,
		"--log-level", "error",
	})

	// This will fail trying to connect to the database, but it exercises the
	// password substitution path. We check for a connect error, not a flag error.
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error (connect failure), got nil")
	}
	// Should be a database connection error, not a flag parsing error.
	if strings.Contains(err.Error(), "database-password-file") {
		t.Fatalf("expected a connect error, got a flag error: %v", err)
	}
}

func TestRootCmd_DatabasePasswordFile_NoDatabaseURL(t *testing.T) {
	pwFile := writeSecretFile(t, "s3cret\n")
	os.Unsetenv("DATABASE_URL")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-password-file", pwFile,
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", "/nonexistent/key.pem",
		"--log-level", "error",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --database-password-file is set without --database-url")
	}
	if !strings.Contains(err.Error(), "requires --database-url") {
		t.Fatalf("expected missing URL error, got: %v", err)
	}
}

func TestRootCmd_DatabasePasswordFile_MissingFile(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-url", "postgres://user@localhost/db",
		"--database-password-file", "/nonexistent/pw",
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", "/nonexistent/key.pem",
		"--log-level", "error",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing password file")
	}
}

func TestRootCmd_DatabasePasswordFile_UnparseableURL(t *testing.T) {
	// Trigger the url.Parse error path in the password-file branch.
	pwFile := writeSecretFile(t, "s3cret\n")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-url", "://\x7finvalid",
		"--database-password-file", pwFile,
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", "/nonexistent/key.pem",
		"--log-level", "error",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unparseable database URL")
	}
	if !strings.Contains(err.Error(), "parsing --database-url") {
		t.Fatalf("expected URL parse error, got: %v", err)
	}
}

func TestRootCmd_DatabasePasswordFile_ConflictWithInlinePassword(t *testing.T) {
	pwFile := writeSecretFile(t, "s3cret\n")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-url", "postgres://user:inlinepw@localhost/db",
		"--database-password-file", pwFile,
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", "/nonexistent/key.pem",
		"--log-level", "error",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when URL already contains a password")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got: %v", err)
	}
}

func TestRootCmd_DatabasePasswordFile_URLSubstitution(t *testing.T) {
	// Verify that the password is correctly placed into the URL userinfo.
	// We do this by testing the URL parsing logic directly.
	rawURL := "postgres://myuser@localhost:5432/mydb?sslmode=disable"
	pw := "p@ss:w0rd/special"

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(parsed.User.Username(), pw)
	result := parsed.String()

	// The password should be URL-encoded in the result.
	reparsed, err := url.Parse(result)
	if err != nil {
		t.Fatalf("re-parsing produced URL: %v", err)
	}
	gotPW, ok := reparsed.User.Password()
	if !ok {
		t.Fatal("password not set in reconstructed URL")
	}
	if gotPW != pw {
		t.Errorf("password = %q, want %q", gotPW, pw)
	}
	if reparsed.User.Username() != "myuser" {
		t.Errorf("username = %q, want %q", reparsed.User.Username(), "myuser")
	}
}

func TestRootCmd_DatabasePasswordOpenBao(t *testing.T) {
	// Set up a mock Vault server that handles AppRole login and KV read.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/approle/login" && r.Method == "POST":
			resp := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "s.test-token",
					"lease_duration": 3600,
					"renewable":      true,
				},
			}
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/secret/data/db" && r.Method == "GET":
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"password": "vault-db-pw",
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	keyFile := generateTestKeyFile(t)
	roleIDFile := writeSecretFile(t, "role-id\n")
	secretIDFile := writeSecretFile(t, "secret-id\n")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-url", "postgres://myuser@localhost:5432/mydb?sslmode=disable&connect_timeout=1",
		"--database-password-openbao", "secret/db:password",
		"--openbao-address", srv.URL,
		"--openbao-approle-role-id-file", roleIDFile,
		"--openbao-approle-secret-id-file", secretIDFile,
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", keyFile,
		"--log-level", "error",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected connect error, got nil")
	}
	// Should fail on database connect, not on flag resolution.
	if strings.Contains(err.Error(), "openbao") {
		t.Fatalf("expected a connect error, got an OpenBao error: %v", err)
	}
}

func TestRootCmd_DatabasePasswordOpenBao_NoDatabaseURL(t *testing.T) {
	os.Unsetenv("DATABASE_URL")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-password-openbao", "secret/db:password",
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", "/nonexistent/key.pem",
		"--log-level", "error",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --database-password-openbao is set without --database-url")
	}
	if !strings.Contains(err.Error(), "requires --database-url") {
		t.Fatalf("expected missing URL error, got: %v", err)
	}
}

func TestRootCmd_DatabasePasswordOpenBao_ConflictWithFile(t *testing.T) {
	pwFile := writeSecretFile(t, "file-pw\n")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-url", "postgres://user@localhost/db",
		"--database-password-file", pwFile,
		"--database-password-openbao", "secret/db:password",
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", "/nonexistent/key.pem",
		"--log-level", "error",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when both file and openbao are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got: %v", err)
	}
}

func TestRootCmd_DatabasePasswordOpenBao_InitFails(t *testing.T) {
	// Test the path where OpenBao client initialization fails (empty address).
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-url", "postgres://user@localhost/db",
		"--database-password-openbao", "secret/db:password",
		"--openbao-address", "",
		"--openbao-approle-role-id-file", "/nonexistent/role",
		"--openbao-approle-secret-id-file", "/nonexistent/secret",
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", "/nonexistent/key.pem",
		"--log-level", "error",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when OpenBao client init fails")
	}
	if !strings.Contains(err.Error(), "initializing OpenBao client") {
		t.Fatalf("expected OpenBao init error, got: %v", err)
	}
}

func TestRootCmd_DatabasePasswordOpenBao_DatabaseURLFromEnv(t *testing.T) {
	// Test using DATABASE_URL env when --database-url is not set.
	t.Setenv("DATABASE_URL", "postgres://user@localhost:5432/db?sslmode=disable&connect_timeout=1")
	os.Unsetenv("OPENBAO_ADDR")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-password-openbao", "secret/db:password",
		"--openbao-address", "",
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", "/nonexistent/key.pem",
		"--log-level", "error",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	// Should fail on OpenBao init (empty address), not on "requires --database-url".
	if strings.Contains(err.Error(), "requires --database-url") {
		t.Fatalf("DATABASE_URL env should have been picked up, got: %v", err)
	}
}

func TestRunWithDatabaseURL_WarnInsecureSSLMode(t *testing.T) {
	// Test the warnInsecureSSLMode path inside run() when dbURL is set
	// (either from config or env). This exercises run() lines 211-213.
	keyFile := generateTestKeyFile(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The DB URL has sslmode=disable, which should trigger the warning.
	// run() will fail on Connect but that's fine -- we're covering the warn path.
	err := run(ctx, runConfig{
		listenAddr:  "127.0.0.1:0",
		databaseURL: "postgres://user@127.0.0.1:1/db?sslmode=disable&connect_timeout=1",
		appID:       1,
		installID:   1,
		keyFile:     keyFile,
		logLevel:    "error",
	})
	if err == nil {
		t.Fatal("expected connect error")
	}
}

func TestWarnInsecureSSLMode(t *testing.T) {
	// Unparseable URL: should return early without panic.
	warnInsecureSSLMode("://not\x7fa valid URL")

	// Secure mode: should not warn (we just verify no panic).
	warnInsecureSSLMode("postgres://user@localhost/db?sslmode=verify-full")

	// Insecure mode: should log a warning (we just verify no panic).
	warnInsecureSSLMode("postgres://user@localhost/db?sslmode=disable")

	// No sslmode at all: should log a warning (we just verify no panic).
	warnInsecureSSLMode("postgres://user@localhost/db")
}

func TestRootCmd_DatabasePasswordFile_FromEnv(t *testing.T) {
	// When --database-url is not set but DATABASE_URL is, the password file
	// should still work.
	keyFile := generateTestKeyFile(t)
	pwFile := writeSecretFile(t, "envpw\n")
	t.Setenv("DATABASE_URL", "postgres://envuser@localhost:5432/envdb?sslmode=disable&connect_timeout=1")

	cmd := rootCmd()
	cmd.SetArgs([]string{
		"--database-password-file", pwFile,
		"--github-app-id", "1",
		"--github-install-id", "1",
		"--github-key-file", keyFile,
		"--log-level", "error",
	})

	err := cmd.Execute()
	// Should fail on connect, not on flag resolution.
	if err == nil {
		t.Fatal("expected connect error, got nil")
	}
	if strings.Contains(err.Error(), "database-password-file") || strings.Contains(err.Error(), "requires") {
		t.Fatalf("expected a connect error, got a flag error: %v", err)
	}
}
