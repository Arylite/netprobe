package store

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
)

var rolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// GrantReadOnly lets a database role read the results, checks and incidents, and
// the name and dates of the edges, never the hash of their token. It is safe to repeat.
func (s *Store) GrantReadOnly(ctx context.Context, role string) error {
	if !rolePattern.MatchString(role) {
		return fmt.Errorf("role %q: use lowercase letters, digits and underscores", role)
	}
	name := pgx.Identifier{role}.Sanitize()
	for _, stmt := range []string{
		"GRANT SELECT ON results, checks, incidents TO " + name,
		"GRANT SELECT (id, name, created_at, revoked_at) ON edges TO " + name,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("grant read access to %s: %w", role, err)
		}
	}
	return nil
}
