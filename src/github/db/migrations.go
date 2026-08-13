package db

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// MigrationEntry holds a parsed migration filename and its SQL content.
type MigrationEntry struct {
	Name string
	SQL  string
}

// LoadMigrations reads all .sql files from the embedded migrations directory,
// sorts them by filename, and returns them in order.
func LoadMigrations() ([]MigrationEntry, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations dir: %w", err)
	}

	var migrations []MigrationEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", e.Name(), err)
		}
		migrations = append(migrations, MigrationEntry{
			Name: e.Name(),
			SQL:  string(data),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Name < migrations[j].Name
	})

	return migrations, nil
}

// MigrationNamesOrdered returns just the sorted filenames for validation.
func MigrationNamesOrdered() ([]string, error) {
	migs, err := LoadMigrations()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(migs))
	for i, m := range migs {
		names[i] = m.Name
	}
	return names, nil
}

// createMigrationsTable ensures the schema_migrations tracking table exists.
const createMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    name TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`

// RunMigrations applies all pending SQL migrations in order.
// It tracks applied migrations in a schema_migrations table and skips any
// that have already been applied. Migrations are forward-only and never
// rolled back. Applying from an empty database produces the same result
// as incremental application.
func RunMigrations(ctx context.Context, pool DBPool) error {
	// Ensure the tracking table exists.
	if _, err := pool.Exec(ctx, createMigrationsTableSQL); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	migrations, err := LoadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migrations {
		applied, err := isMigrationApplied(ctx, pool, m.Name)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", m.Name, err)
		}
		if applied {
			continue
		}

		if _, err := pool.Exec(ctx, m.SQL); err != nil {
			return fmt.Errorf("applying migration %s: %w", m.Name, err)
		}

		if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (name) VALUES ($1)", m.Name); err != nil {
			return fmt.Errorf("recording migration %s: %w", m.Name, err)
		}
	}

	return nil
}

// isMigrationApplied checks whether a migration has already been recorded.
func isMigrationApplied(ctx context.Context, pool DBPool, name string) (bool, error) {
	var count int
	err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE name = $1", name,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
