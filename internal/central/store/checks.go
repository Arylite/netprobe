package store

import (
	"context"
	"fmt"

	"github.com/Arylite/netprobe/internal/api"
)

// AddCheck registers a check that every edge will run.
func (s *Store) AddCheck(ctx context.Context, c api.Check) error {
	if err := c.Validate(); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO checks (id, kind, target, expect, interval_seconds) VALUES ($1, $2, $3, $4, $5)`,
		c.ID, c.Kind, c.Target, c.Expect, c.IntervalSeconds)
	if isUniqueViolation(err) {
		return fmt.Errorf("check %q: %w", c.ID, ErrExists)
	}
	if err != nil {
		return fmt.Errorf("add check: %w", err)
	}
	return nil
}

// RemoveCheck stops assigning a check. Its results are kept.
func (s *Store) RemoveCheck(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM checks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("remove check: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("check %q: %w", id, ErrNotFound)
	}
	return nil
}

// ListChecks returns the checks ordered by id; never nil.
func (s *Store) ListChecks(ctx context.Context) ([]api.Check, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, kind, target, expect, interval_seconds FROM checks ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list checks: %w", err)
	}
	defer rows.Close()
	checks := []api.Check{}
	for rows.Next() {
		var c api.Check
		if err := rows.Scan(&c.ID, &c.Kind, &c.Target, &c.Expect, &c.IntervalSeconds); err != nil {
			return nil, fmt.Errorf("list checks: %w", err)
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}
