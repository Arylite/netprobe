package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLock serialises migrations when several centrals start together.
const migrationLock = 7_304_201

// Store is the database of the central.
type Store struct {
	pool   *pgxpool.Pool
	cipher *Cipher
}

// Option changes how a Store is opened.
type Option func(*Store)

// WithCipher encrypts the secrets of the channels, and reads them back.
func WithCipher(c *Cipher) Option { return func(s *Store) { s.cipher = c } }

// Open connects to the database and brings its schema up to date.
func Open(ctx context.Context, url string, opts ...Option) (*Store, error) {
	updateTimescale(ctx, url)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect to the database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("reach the database: %w", err)
	}
	s := &Store{pool: pool}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// updateTimescale brings the extension to the version the server has, which a
// new image brings without touching the database. It must be the first command
// of a session, so it has a connection of its own. It is best effort: a database
// without the extension yet, or a user who may not update it, is left to the
// migrations and to the diagnostics.
func updateTimescale(ctx context.Context, url string) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	_, _ = conn.Exec(ctx, "ALTER EXTENSION timescaledb UPDATE")
}

// Close releases the connections.
func (s *Store) Close() { s.pool.Close() }

// Ping checks that the database answers.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		version, convErr := strconv.Atoi(prefix)
		if !ok || convErr != nil {
			return nil, fmt.Errorf("migration %q: want NNNN_name.sql", e.Name())
		}
		body, err := migrationFiles.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: version, name: e.Name(), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// migrate applies the migrations that were not applied yet, each in its own
// transaction. The lock lives on one connection for the whole run.
func (s *Store) migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLock); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLock) }()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version integer PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	var current int
	if err := conn.QueryRow(ctx, "SELECT COALESCE(max(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := applyMigration(ctx, conn, m); err != nil {
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, m migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", m.version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
