package store

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// LastResultPerEdge returns when each edge last reported. An edge that never
// reported is absent.
func (s *Store) LastResultPerEdge(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.pool.Query(ctx, `SELECT edge_id, max(at) FROM results GROUP BY edge_id`)
	if err != nil {
		return nil, fmt.Errorf("read last results: %w", err)
	}
	defer rows.Close()
	last := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("read last results: %w", err)
		}
		last[id] = at
	}
	return last, rows.Err()
}

// Outcome is what one run of a check came to.
type Outcome struct {
	At    time.Time
	OK    bool
	Error string
}

// PairOutcomes are the latest outcomes of one check on one edge, newest first.
type PairOutcomes struct {
	CheckID string
	EdgeID  string
	Newest  []Outcome
}

// RecentOutcomes returns, for every check on every active edge that reported
// since the given time, its n latest outcomes.
func (s *Store) RecentOutcomes(ctx context.Context, n int, since time.Time) ([]PairOutcomes, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, e.id, r.at, r.ok, r.error
		   FROM checks c
		  CROSS JOIN edges e
		  CROSS JOIN LATERAL (SELECT at, ok, error FROM results
		                       WHERE check_id = c.id AND edge_id = e.id AND at > $2
		                       ORDER BY at DESC LIMIT $1) r
		  WHERE e.revoked_at IS NULL
		  ORDER BY c.id, e.id, r.at DESC`, n, since)
	if err != nil {
		return nil, fmt.Errorf("read recent outcomes: %w", err)
	}
	defer rows.Close()
	var out []PairOutcomes
	for rows.Next() {
		var checkID, edgeID string
		var o Outcome
		if err := rows.Scan(&checkID, &edgeID, &o.At, &o.OK, &o.Error); err != nil {
			return nil, fmt.Errorf("read recent outcomes: %w", err)
		}
		if last := len(out) - 1; last < 0 || out[last].CheckID != checkID || out[last].EdgeID != edgeID {
			out = append(out, PairOutcomes{CheckID: checkID, EdgeID: edgeID})
		}
		p := &out[len(out)-1]
		p.Newest = append(p.Newest, o)
	}
	return out, rows.Err()
}

// LastResultSince is LastResultPerEdge restricted to the results after a
// time, which a database that has run for a year can answer quickly.
func (s *Store) LastResultSince(ctx context.Context, since time.Time) (map[string]time.Time, error) {
	rows, err := s.pool.Query(ctx, `SELECT edge_id, max(at) FROM results WHERE at > $1 GROUP BY edge_id`, since)
	if err != nil {
		return nil, fmt.Errorf("read last results: %w", err)
	}
	defer rows.Close()
	last := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("read last results: %w", err)
		}
		last[id] = at
	}
	return last, rows.Err()
}

// Summary is how a check did on an edge over a window, with its latest result.
type Summary struct {
	CheckID   string
	EdgeID    string
	Samples   int
	Succeeded int
	LastAt    time.Time
	LastOK    bool
	LastRTT   float64
	LastError string
	// P95RTT is the 95th percentile of the round-trip time of the successful
	// runs; nil when none succeeded.
	P95RTT *float64
}

// Summaries returns one summary per check and edge that reported since the
// given time.
func (s *Store) Summaries(ctx context.Context, since time.Time) ([]Summary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT check_id, edge_id,
		        count(*), count(*) FILTER (WHERE ok),
		        percentile_cont(0.95) WITHIN GROUP (ORDER BY rtt_millis) FILTER (WHERE ok),
		        (array_agg(at ORDER BY at DESC))[1],
		        (array_agg(ok ORDER BY at DESC))[1],
		        (array_agg(rtt_millis ORDER BY at DESC))[1],
		        (array_agg(error ORDER BY at DESC))[1]
		   FROM results WHERE at > $1
		  GROUP BY check_id, edge_id ORDER BY check_id, edge_id`, since)
	if err != nil {
		return nil, fmt.Errorf("read summaries: %w", err)
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var m Summary
		if err := rows.Scan(&m.CheckID, &m.EdgeID, &m.Samples, &m.Succeeded, &m.P95RTT, &m.LastAt, &m.LastOK, &m.LastRTT, &m.LastError); err != nil {
			return nil, fmt.Errorf("read summaries: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetRetention drops the results older than d, in the background, or keeps
// them for ever when d is zero.
func (s *Store) SetRetention(ctx context.Context, d time.Duration) error {
	if d < 0 {
		return errors.New("retention must not be negative")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("set retention: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT remove_retention_policy('results', if_exists => true)`); err != nil {
		return fmt.Errorf("set retention: %w", err)
	}
	if d > 0 {
		if _, err := tx.Exec(ctx, `SELECT add_retention_policy('results', make_interval(secs => $1::double precision))`, d.Seconds()); err != nil {
			return fmt.Errorf("set retention: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// Retention returns how long results are kept; zero means for ever.
func (s *Store) Retention(ctx context.Context) (time.Duration, error) {
	var seconds *float64
	err := s.pool.QueryRow(ctx,
		`SELECT extract(epoch FROM (config->>'drop_after')::interval)
		   FROM timescaledb_information.jobs
		  WHERE proc_name = 'policy_retention' AND hypertable_name = 'results'`).Scan(&seconds)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && seconds == nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read retention: %w", err)
	}
	return time.Duration(*seconds * float64(time.Second)), nil
}
