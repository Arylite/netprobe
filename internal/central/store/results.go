package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Arylite/netprobe/internal/api"
)

const maxRecent = 1000

// Result is a measurement with the edge that made it.
type Result struct {
	EdgeID string
	api.Result
}

// InsertResults stores a batch reported by an edge, all or nothing.
func (s *Store) InsertResults(ctx context.Context, edgeID string, results []api.Result) error {
	if len(results) == 0 {
		return nil
	}
	_, err := s.pool.CopyFrom(ctx,
		pgx.Identifier{"results"},
		[]string{"edge_id", "check_id", "at", "ok", "rtt_millis", "error"},
		pgx.CopyFromSlice(len(results), func(i int) ([]any, error) {
			r := results[i]
			return []any{edgeID, r.CheckID, r.At, r.OK, r.RTTMillis, r.Error}, nil
		}))
	if err != nil {
		return fmt.Errorf("insert results: %w", err)
	}
	return nil
}

// RecentResults returns the latest results of a check, newest first. At most
// maxRecent are returned whatever limit asks.
func (s *Store) RecentResults(ctx context.Context, checkID string, limit int) ([]Result, error) {
	limit = min(max(limit, 1), maxRecent)
	rows, err := s.pool.Query(ctx,
		`SELECT edge_id, check_id, at, ok, rtt_millis, error
		   FROM results WHERE check_id = $1 ORDER BY at DESC LIMIT $2`, checkID, limit)
	if err != nil {
		return nil, fmt.Errorf("read results: %w", err)
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.EdgeID, &r.CheckID, &r.At, &r.OK, &r.RTTMillis, &r.Error); err != nil {
			return nil, fmt.Errorf("read results: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
