package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mtanzi/spyglass/internal/core"
	"github.com/mtanzi/spyglass/internal/store/postgres/pgtest"
)

func TestClassifyBySQLStateClass(t *testing.T) {
	cases := []struct {
		code       string
		unwritable bool
	}{
		{"22021", true}, // invalid byte sequence
		{"22P05", true}, // untranslatable character
		{"22003", true}, // numeric value out of range
		{"22P02", true}, // invalid text representation
		{"23502", true}, // not null violation
		{"23514", true}, // check violation
		{"23505", true}, // unique violation
		{"23P01", true}, // exclusion violation
		{"54000", true}, // program limit exceeded
		{"54001", true}, // statement too complex
		{"08006", false},
		{"40001", false},
		{"40P01", false},
		{"42P01", false}, // undefined table: a schema bug hits every record
		{"42703", false},
		{"53100", false}, // disk full
		{"57P01", false}, // admin shutdown
		{"57014", false},
		{"XX000", false},
		{"", false},
	}
	for _, c := range cases {
		pg := &pgconn.PgError{Code: c.code, ConstraintName: "k", ColumnName: "col"}
		err := classify(fmt.Errorf("exec: %w", pg), callColumns)
		if got := errors.Is(err, core.ErrUnwritable); got != c.unwritable {
			t.Errorf("%q: unwritable = %v, want %v", c.code, got, c.unwritable)
		}
		if !errors.Is(err, pg) {
			t.Errorf("%q: classified error lost the driver error", c.code)
		}
		if u, ok := errors.AsType[*unwritableError](err); ok {
			if u.SQLState() != c.code || u.Constraint() != "k" || u.Column() != "col" {
				t.Errorf("%q: cause = %q %q %q", c.code, u.SQLState(), u.Constraint(), u.Column())
			}
		}
	}
}

func TestClassifyTransientErrors(t *testing.T) {
	for _, err := range []error{
		context.Canceled,
		context.DeadlineExceeded,
		io.ErrUnexpectedEOF,
		&net.OpError{Op: "read", Err: errors.New("connection reset by peer")},
		errors.New("closed pool"),
	} {
		if got := classify(err, callColumns); errors.Is(got, core.ErrUnwritable) || got != err {
			t.Errorf("%v: classified as %v, want unchanged", err, got)
		}
	}
	if classify(nil, callColumns) != nil {
		t.Error("classify(nil) is not nil")
	}
}

// TestClassifyRealErrors feeds a real Postgres each class, so a driver change
// that moves an error out of its class fails here.
func TestClassifyRealErrors(t *testing.T) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, pgtest.NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(ctx, "CREATE TABLE t (a integer NOT NULL, b text CONSTRAINT b_short CHECK (length(b) < 3))"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		sql        string
		args       []any
		unwritable bool
		code       string
		column     string
		constraint string
	}{
		{"invalid_byte_sequence", `SELECT convert_from('\xff'::bytea, 'UTF8')`, nil, true, "22021", "", ""},
		{"invalid_text_representation", `SELECT 'x'::integer`, nil, true, "22P02", "", ""},
		{"numeric_out_of_range", `SELECT 2147483648::integer`, nil, true, "22003", "", ""},
		{"not_null", `INSERT INTO t (a) VALUES (NULL)`, nil, true, "23502", "a", ""},
		{"check", `INSERT INTO t (a, b) VALUES (1, 'long')`, nil, true, "23514", "", "b_short"},
		{"program_limit", `SELECT repeat('x', 1073741824)`, nil, true, "54000", "", ""},
		{"client_encode", `INSERT INTO t (a) VALUES ($1)`, []any{int64(1) << 40}, true, "", "bytes", ""},
		{"undefined_table", `SELECT * FROM nope`, nil, false, "", "", ""},
		{"undefined_column", `SELECT nope FROM t`, nil, false, "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := conn.Exec(ctx, c.sql, c.args...)
			if err == nil {
				t.Fatal("statement succeeded")
			}
			// The lone bind argument is index 0, so an encode failure names
			// the first column of the list.
			got := classify(err, []string{"bytes"})
			if errors.Is(got, core.ErrUnwritable) != c.unwritable {
				t.Fatalf("unwritable = %v, want %v: %v", !c.unwritable, c.unwritable, err)
			}
			if !c.unwritable {
				return
			}
			u, _ := errors.AsType[*unwritableError](got)
			if u.SQLState() != c.code || u.Constraint() != c.constraint || u.Column() != c.column {
				t.Errorf("cause = %q %q %q, want %q %q %q (%v)", u.SQLState(), u.Constraint(), u.Column(), c.code, c.constraint, c.column, err)
			}
		})
	}
}

func TestClassifyStatementTimeoutIsTransient(t *testing.T) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, pgtest.NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(ctx, "SET statement_timeout = 10"); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, "SELECT pg_sleep(1)")
	if err == nil {
		t.Fatal("statement did not time out")
	}
	if errors.Is(classify(err, nil), core.ErrUnwritable) {
		t.Fatalf("statement timeout classified as unwritable: %v", err)
	}
}
