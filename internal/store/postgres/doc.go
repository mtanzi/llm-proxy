// Package postgres is the Postgres adapter for the core storage ports.
//
// It owns its SQL and its schema: hand-written pgx queries and goose
// migrations embedded from the migrations directory. Table names never leave
// the package. Postgres 16 or later.
//
// Migration policy: every migration is expand then contract. A release adds
// tables, columns or indexes in a backward-compatible way, and a later release
// removes what is no longer used, so a one-version rollback needs no restore.
// A shipped migration is never edited; a change is a new numbered file.
package postgres
