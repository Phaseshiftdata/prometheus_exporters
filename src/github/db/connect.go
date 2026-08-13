package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgxPool wraps pgxpool.Pool to implement DBPool.
type PgxPool struct {
	pool *pgxpool.Pool
}

// Connect opens a connection pool to PostgreSQL and returns a DBPool.
func Connect(ctx context.Context, databaseURL string) (*PgxPool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &PgxPool{pool: pool}, nil
}

// Exec implements DBPool.
func (p *PgxPool) Exec(ctx context.Context, sql string, args ...any) (CommandTag, error) {
	tag, err := p.pool.Exec(ctx, sql, args...)
	return pgxCommandTag{tag}, err
}

// Query implements DBPool.
func (p *PgxPool) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgxRows{rows}, nil
}

// QueryRow implements DBPool.
func (p *PgxPool) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return p.pool.QueryRow(ctx, sql, args...)
}

// Close closes the underlying connection pool.
func (p *PgxPool) Close() {
	p.pool.Close()
}

type pgxCommandTag struct {
	tag interface{ RowsAffected() int64 }
}

func (t pgxCommandTag) RowsAffected() int64 { return t.tag.RowsAffected() }

type pgxRows struct {
	rows interface {
		Next() bool
		Scan(dest ...any) error
		Err() error
		Close()
	}
}

func (r pgxRows) Next() bool          { return r.rows.Next() }
func (r pgxRows) Scan(dest ...any) error { return r.rows.Scan(dest...) }
func (r pgxRows) Err() error          { return r.rows.Err() }
func (r pgxRows) Close()              { r.rows.Close() }
