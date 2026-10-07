package store

import (
	"context"
	"fmt"
	"time"
)

const maxAudit = 500

// AuditEvent is something a person did that changed what the central holds, or
// that tried to get in.
type AuditEvent struct {
	ID       int64
	At       time.Time
	Actor    string // empty when nobody was signed in
	Action   string // login, user.create, edge.revoke...
	Target   string // what it was done to, when that is a name
	ClientIP string
}

// RecordAudit adds an event to the trail.
func (s *Store) RecordAudit(ctx context.Context, e AuditEvent) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_events (at, actor, action, target, client_ip) VALUES ($1, $2, $3, $4, $5)`,
		e.At, truncate(e.Actor, 128), truncate(e.Action, 64), truncate(e.Target, 128), truncate(e.ClientIP, 64))
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

// ListAudit returns the latest events, newest first. At most 500 are returned
// whatever limit asks.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEvent, error) {
	limit = min(max(limit, 1), maxAudit)
	rows, err := s.pool.Query(ctx,
		`SELECT id, at, actor, action, target, client_ip FROM audit_events ORDER BY at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("read audit events: %w", err)
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &e.Target, &e.ClientIP); err != nil {
			return nil, fmt.Errorf("read audit events: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PurgeAudit deletes the events older than a time and returns how many there were.
func (s *Store) PurgeAudit(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM audit_events WHERE at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("purge audit events: %w", err)
	}
	return tag.RowsAffected(), nil
}
