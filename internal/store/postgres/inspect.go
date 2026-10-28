package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// StoredValue is one stored value of a text-like or jsonb column, as text.
type StoredValue struct {
	Table, Column, Value string
}

// StoredValues returns every non-null value of every text-like and jsonb
// column in the current schema. A jsonb value includes its keys. It is the
// hook the contract suite uses to prove that no body text is stored. It reads
// whole tables, so it is for tests only.
func (s *Store) StoredValues(ctx context.Context) ([]StoredValue, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND data_type IN ('text', 'character varying', 'character', 'json', 'jsonb')
		ORDER BY table_name, ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("postgres: stored values: %w", err)
	}
	type column struct{ table, name string }
	cols, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (column, error) {
		var c column
		return c, r.Scan(&c.table, &c.name)
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: stored values: %w", err)
	}

	var out []StoredValue
	for _, c := range cols {
		table, name := pgx.Identifier{c.table}.Sanitize(), pgx.Identifier{c.name}.Sanitize()
		rows, err := s.pool.Query(ctx, fmt.Sprintf("SELECT %s::text FROM %s WHERE %s IS NOT NULL", name, table, name))
		if err != nil {
			return nil, fmt.Errorf("postgres: stored values: %w", err)
		}
		values, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, fmt.Errorf("postgres: stored values: %w", err)
		}
		for _, v := range values {
			out = append(out, StoredValue{Table: c.table, Column: c.name, Value: v})
		}
	}
	return out, nil
}

// CountRows returns the number of rows in a table of the store: "calls" or
// "tool_events". It is for tests only.
func (s *Store) CountRows(ctx context.Context, table string) (int, error) {
	switch table {
	case "calls", "tool_events":
	default:
		return 0, fmt.Errorf("postgres: count rows: unknown table %q", table)
	}
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		return 0, fmt.Errorf("postgres: count rows: %w", err)
	}
	return n, nil
}
