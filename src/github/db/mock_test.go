package db

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// mockCommandTag implements CommandTag for testing.
type mockCommandTag struct {
	rowsAffected int64
}

func (t mockCommandTag) RowsAffected() int64 { return t.rowsAffected }

// mockRow implements Row for testing.
type mockRow struct {
	values []any
	err    error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if i < len(r.values) {
			switch ptr := d.(type) {
			case *int:
				switch v := r.values[i].(type) {
				case int:
					*ptr = v
				case int64:
					*ptr = int(v)
				}
			case *int64:
				switch v := r.values[i].(type) {
				case int64:
					*ptr = v
				case int:
					*ptr = int64(v)
				}
			case *string:
				if v, ok := r.values[i].(string); ok {
					*ptr = v
				}
			case *bool:
				switch v := r.values[i].(type) {
				case bool:
					*ptr = v
				case int:
					*ptr = v != 0
				}
			case *time.Time:
				switch v := r.values[i].(type) {
				case time.Time:
					*ptr = v
				}
			}
		}
	}
	return nil
}

// mockRows implements Rows for testing.
type mockRows struct {
	data    [][]any
	cursor  int
	scanErr error
}

func (r *mockRows) Next() bool {
	return r.cursor < len(r.data)
}

func (r *mockRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.cursor >= len(r.data) {
		return fmt.Errorf("no more rows")
	}
	row := r.data[r.cursor]
	r.cursor++
	for i, d := range dest {
		if i < len(row) {
			switch ptr := d.(type) {
			case *int:
				switch v := row[i].(type) {
				case int:
					*ptr = v
				case int64:
					*ptr = int(v)
				}
			case *int64:
				switch v := row[i].(type) {
				case int64:
					*ptr = v
				case int:
					*ptr = int64(v)
				}
			case *string:
				if v, ok := row[i].(string); ok {
					*ptr = v
				}
			case *bool:
				switch v := row[i].(type) {
				case bool:
					*ptr = v
				case int:
					*ptr = v != 0
				}
			case *time.Time:
				switch v := row[i].(type) {
				case time.Time:
					*ptr = v
				}
			}
		}
	}
	return nil
}

func (r *mockRows) Err() error  { return nil }
func (r *mockRows) Close()      {}

// execCall records a single call to Exec.
type execCall struct {
	SQL  string
	Args []any
}

// mockPool implements DBPool for testing.
type mockPool struct {
	mu        sync.Mutex
	execCalls []execCall
	execErr   error

	// queryRowResults maps SQL prefix to the mock row to return.
	queryRowResults map[string]*mockRow
	queryRowDefault *mockRow

	// queryResults maps SQL prefix to the mock rows to return.
	queryResults map[string]*mockRows
}

func newMockPool() *mockPool {
	return &mockPool{
		queryRowResults: make(map[string]*mockRow),
		queryResults:    make(map[string]*mockRows),
		queryRowDefault: &mockRow{values: []any{0}}, // default: count=0
	}
}

func (p *mockPool) Exec(_ context.Context, sql string, args ...any) (CommandTag, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.execCalls = append(p.execCalls, execCall{SQL: sql, Args: args})
	if p.execErr != nil {
		return nil, p.execErr
	}
	return mockCommandTag{rowsAffected: 1}, nil
}

func (p *mockPool) Query(_ context.Context, sql string, _ ...any) (Rows, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for prefix, rows := range p.queryResults {
		if len(sql) >= len(prefix) && sql[:len(prefix)] == prefix {
			return rows, nil
		}
	}
	return &mockRows{}, nil
}

func (p *mockPool) QueryRow(_ context.Context, sql string, _ ...any) Row {
	p.mu.Lock()
	defer p.mu.Unlock()
	for prefix, row := range p.queryRowResults {
		if len(sql) >= len(prefix) && sql[:len(prefix)] == prefix {
			return row
		}
	}
	return p.queryRowDefault
}

func (p *mockPool) getExecCalls() []execCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]execCall, len(p.execCalls))
	copy(out, p.execCalls)
	return out
}
