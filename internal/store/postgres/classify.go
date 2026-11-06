package postgres

import (
	"errors"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// unwritableError is a write error caused by the content of the records. It
// matches core.ErrUnwritable and keeps the driver error. Its accessors name the
// cause for the recorder's drop log; they hold no stored value.
type unwritableError struct {
	err        error
	sqlState   string
	constraint string
	column     string
}

func (e *unwritableError) Error() string   { return e.err.Error() }
func (e *unwritableError) Unwrap() []error { return []error{core.ErrUnwritable, e.err} }

// SQLState is the SQLSTATE of the error, or "" for a client-side failure.
func (e *unwritableError) SQLState() string { return e.sqlState }

// Constraint is the name of the violated constraint, when Postgres names one.
func (e *unwritableError) Constraint() string { return e.constraint }

// Column is the name of the column, when known.
func (e *unwritableError) Column() string { return e.column }

// encodeFailure matches the error pgx returns when it cannot encode a bind
// value, before the statement is sent. pgx has no error type for it: the
// error is fmt.Errorf("failed to encode args[%d]: %w", ...) around the pgtype
// error. A test pins the wording against the pgx in go.mod.
var encodeFailure = regexp.MustCompile(`failed to encode args\[(\d+)\]`)

// classify marks an error of an INSERT into cols as unwritable when the
// content of the rows caused it: SQLSTATE class 22 (data exception), 23
// (integrity constraint violation) or 54 (program limit exceeded), or a value
// pgx could not encode. Every other error, including one nobody recognizes,
// is returned unchanged and is transient: an outage must never drop records.
func classify(err error, cols []string) error {
	if err == nil {
		return nil
	}
	if pg, ok := errors.AsType[*pgconn.PgError](err); ok {
		if len(pg.Code) == 5 {
			switch pg.Code[:2] {
			case "22", "23", "54":
				return &unwritableError{err: err, sqlState: pg.Code, constraint: pg.ConstraintName, column: pg.ColumnName}
			}
		}
		return err
	}
	if m := encodeFailure.FindStringSubmatch(err.Error()); m != nil {
		u := &unwritableError{err: err}
		if i, convErr := strconv.Atoi(m[1]); convErr == nil && len(cols) > 0 {
			u.column = cols[i%len(cols)]
		}
		return u
	}
	return err
}
