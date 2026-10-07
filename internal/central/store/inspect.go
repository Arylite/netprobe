package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Info describes a database as it is, without changing it.
type Info struct {
	ServerVersion    string
	TimescaleVersion string // empty when the extension is not created in this database
	// TimescaleLatest is the version the server has; it differs from
	// TimescaleVersion until the extension is updated.
	TimescaleLatest string
	// TimescaleAvailable tells whether the server has the extension at all.
	TimescaleAvailable bool
	SchemaVersion      int // 0 when no migration was applied
	LatestVersion      int // the newest migration this binary knows
}

// Inspect reads the state of a database without migrating it, which is what a
// diagnostic must do: it must not change what it looks at.
func Inspect(ctx context.Context, url string) (Info, error) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return Info{}, fmt.Errorf("connect to the database: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	var info Info
	if err := conn.QueryRow(ctx, `SHOW server_version`).Scan(&info.ServerVersion); err != nil {
		return Info{}, fmt.Errorf("read the server version: %w", err)
	}
	err = conn.QueryRow(ctx, `SELECT extversion FROM pg_extension WHERE extname = 'timescaledb'`).Scan(&info.TimescaleVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Info{}, fmt.Errorf("read the TimescaleDB version: %w", err)
	}

	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb')`).Scan(&info.TimescaleAvailable); err != nil {
		return Info{}, fmt.Errorf("read the available extensions: %w", err)
	}
	if info.TimescaleAvailable {
		if err := conn.QueryRow(ctx, `SELECT default_version FROM pg_available_extensions WHERE name = 'timescaledb'`).Scan(&info.TimescaleLatest); err != nil {
			return Info{}, fmt.Errorf("read the available extensions: %w", err)
		}
	}

	var hasTable bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&hasTable); err != nil {
		return Info{}, fmt.Errorf("read the schema: %w", err)
	}
	if hasTable {
		if err := conn.QueryRow(ctx, `SELECT COALESCE(max(version), 0) FROM schema_migrations`).Scan(&info.SchemaVersion); err != nil {
			return Info{}, fmt.Errorf("read the schema version: %w", err)
		}
	}

	migrations, err := loadMigrations()
	if err != nil {
		return Info{}, err
	}
	if len(migrations) > 0 {
		info.LatestVersion = migrations[len(migrations)-1].version
	}
	return info, nil
}
