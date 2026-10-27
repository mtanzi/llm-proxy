package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/mtanzi/llm-proxy/internal/core"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Config holds the connection settings. cmd fills it from the database
// section of the config file.
type Config struct {
	URL      string // a postgres:// URL or key=value connection string
	MaxConns int    // pool size; zero or less keeps the pgx default
}

// Store is the Postgres adapter. It implements core.CallWriter.
type Store struct {
	pool *pgxpool.Pool
}

var _ core.CallWriter = (*Store)(nil)

// Open builds the connection pool. Connections are made on first use, so a
// database that is down fails Ping or the first query, not Open.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		// The parse error can quote the URL, which may hold a password.
		return nil, errors.New("postgres: invalid database url")
	}
	if cfg.MaxConns > 0 {
		pc.MaxConns = int32(cfg.MaxConns)
	}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Migrate applies the embedded migrations that are not applied yet. It holds
// goose's Postgres session lock while it runs, so concurrent starts apply each
// migration once.
func (s *Store) Migrate(ctx context.Context) error {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("postgres: migrations: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("postgres: migrations: %w", err)
	}
	db := stdlib.OpenDBFromPool(s.pool)
	// Closing the database/sql handle leaves the pool open.
	defer func() { _ = db.Close() }()
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("postgres: migrations: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("postgres: migrations: %w", err)
	}
	return nil
}

// Ping checks that the database answers.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close closes the pool. It waits for connections in use to be returned.
func (s *Store) Close() {
	s.pool.Close()
}
