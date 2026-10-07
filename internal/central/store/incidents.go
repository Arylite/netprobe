package store

import (
	"context"
	"fmt"
	"time"
)

// Kinds of incident.
const (
	IncidentCheck = "check" // a check keeps failing on an edge
	IncidentEdge  = "edge"  // an edge stopped reporting
)

// Events an incident tells its channels about.
const (
	EventOpened   = "opened"
	EventResolved = "resolved"
)

// How an incident ended.
const (
	ResolutionRecovered    = "recovered"
	ResolutionEdgeRevoked  = "edge revoked"
	ResolutionCheckRemoved = "check removed"
)

const (
	maxIncidents       = 500
	maxDetailLength    = 512
	maxResolutionBytes = 64
	// alertLock serialises the alert evaluation of several centrals.
	alertLock = 7_304_202
)

// Incident is something that went wrong on an edge, until it is resolved.
type Incident struct {
	ID         int64
	Kind       string
	CheckID    string // empty for an edge incident
	EdgeID     string
	EdgeName   string
	StartedAt  time.Time
	ResolvedAt *time.Time
	Detail     string
	Resolution string
}

// Open reports whether the incident is still going on.
func (i Incident) Open() bool { return i.ResolvedAt == nil }

// Notification is an event of an incident that a channel has not been told yet.
type Notification struct {
	Incident Incident
	Event    string
	Channel  Channel
}

// At is when the event happened.
func (n Notification) At() time.Time {
	if n.Event == EventResolved && n.Incident.ResolvedAt != nil {
		return *n.Incident.ResolvedAt
	}
	return n.Incident.StartedAt
}

const incidentColumns = `i.id, i.kind, i.check_id, i.edge_id, e.name, i.started_at, i.resolved_at, i.detail, i.resolution`

type scanner interface{ Scan(dest ...any) error }

func scanIncident(row scanner, extra ...any) (Incident, error) {
	var i Incident
	err := row.Scan(append([]any{&i.ID, &i.Kind, &i.CheckID, &i.EdgeID, &i.EdgeName, &i.StartedAt, &i.ResolvedAt, &i.Detail, &i.Resolution}, extra...)...)
	return i, err
}

// OpenIncident records an incident, unless the same subject already has one
// open. It reports whether it opened a new one.
func (s *Store) OpenIncident(ctx context.Context, kind, checkID, edgeID, detail string, at time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO incidents (kind, check_id, edge_id, started_at, detail) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (kind, check_id, edge_id) WHERE resolved_at IS NULL DO NOTHING`,
		kind, checkID, edgeID, at, truncate(detail, maxDetailLength))
	if err != nil {
		return false, fmt.Errorf("open incident: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ResolveIncident ends an incident that is still open.
func (s *Store) ResolveIncident(ctx context.Context, id int64, at time.Time, resolution string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE incidents SET resolved_at = $2, resolution = $3 WHERE id = $1 AND resolved_at IS NULL`,
		id, at, truncate(resolution, maxResolutionBytes))
	if err != nil {
		return fmt.Errorf("resolve incident: %w", err)
	}
	return nil
}

// OpenIncidents returns the incidents that are still going on.
func (s *Store) OpenIncidents(ctx context.Context) ([]Incident, error) {
	return s.incidents(ctx, `WHERE i.resolved_at IS NULL ORDER BY i.started_at, i.id`)
}

// ListIncidents returns the latest incidents, newest first, open ones only
// when asked. At most 500 are returned whatever limit asks.
func (s *Store) ListIncidents(ctx context.Context, openOnly bool, limit int) ([]Incident, error) {
	limit = min(max(limit, 1), maxIncidents)
	where := ""
	if openOnly {
		where = "WHERE i.resolved_at IS NULL "
	}
	return s.incidents(ctx, where+`ORDER BY i.started_at DESC, i.id DESC LIMIT $1`, limit)
}

func (s *Store) incidents(ctx context.Context, clause string, args ...any) ([]Incident, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+incidentColumns+` FROM incidents i JOIN edges e ON e.id = i.edge_id `+clause, args...)
	if err != nil {
		return nil, fmt.Errorf("read incidents: %w", err)
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, fmt.Errorf("read incidents: %w", err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// PendingNotifications returns what the channels were not told yet, oldest
// first. A channel only hears of incidents that started after it was created,
// and nothing older than since is sent: an outage of the webhook must not turn
// into a burst of stale news when it comes back. It hears that an incident is
// resolved only if it heard that it opened.
func (s *Store) PendingNotifications(ctx context.Context, since time.Time) ([]Notification, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+incidentColumns+`, ev.event, c.name, c.url, c.secret, c.created_at
		   FROM incidents i
		   JOIN edges e ON e.id = i.edge_id
		   CROSS JOIN (VALUES ('opened'), ('resolved')) AS ev(event)
		   CROSS JOIN channels c
		  WHERE (ev.event = 'opened' OR (i.resolved_at IS NOT NULL AND EXISTS (
		            SELECT 1 FROM deliveries o
		             WHERE o.incident_id = i.id AND o.event = 'opened' AND o.channel = c.name)))
		    AND i.started_at >= c.created_at
		    AND (CASE ev.event WHEN 'opened' THEN i.started_at ELSE i.resolved_at END) > $1
		    AND NOT EXISTS (SELECT 1 FROM deliveries d
		                     WHERE d.incident_id = i.id AND d.event = ev.event AND d.channel = c.name)
		  ORDER BY (CASE ev.event WHEN 'opened' THEN i.started_at ELSE i.resolved_at END), i.id`, since)
	if err != nil {
		return nil, fmt.Errorf("read pending notifications: %w", err)
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		i, err := scanIncident(rows, &n.Event, &n.Channel.Name, &n.Channel.URL, &n.Channel.Secret, &n.Channel.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("read pending notifications: %w", err)
		}
		n.Incident = i
		out = append(out, n)
	}
	return out, rows.Err()
}

// RecordDelivery notes that a channel was told of an event.
func (s *Store) RecordDelivery(ctx context.Context, incidentID int64, event, channel string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO deliveries (incident_id, event, channel, delivered_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT DO NOTHING`, incidentID, event, channel, at)
	if err != nil {
		return fmt.Errorf("record delivery: %w", err)
	}
	return nil
}

// WithAlertLock runs fn if no other central is evaluating alerts, so that
// several centrals on one database do not tell everything twice. It reports
// whether fn ran.
func (s *Store) WithAlertLock(ctx context.Context, fn func(context.Context) error) (bool, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("lock alerts: %w", err)
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, alertLock).Scan(&got); err != nil {
		return false, fmt.Errorf("lock alerts: %w", err)
	}
	if !got {
		return false, nil
	}
	defer func() {
		// A lock that cannot be released must not go back to the pool with its
		// connection: closing the connection drops it.
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, alertLock); err != nil {
			_ = conn.Hijack().Close(context.WithoutCancel(ctx))
		}
	}()
	return true, fn(ctx)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Do not cut a multi-byte character in two.
	for max > 0 && s[max]&0xC0 == 0x80 {
		max--
	}
	return s[:max]
}
