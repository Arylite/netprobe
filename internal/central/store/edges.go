package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	tokenPrefix   = "np_"
	tokenByteSize = 32
)

var (
	namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

	// ErrNotFound is returned when the row asked for does not exist.
	ErrNotFound = errors.New("not found")
	// ErrExists is returned when a row with that name or id already exists.
	ErrExists = errors.New("already exists")
)

// Edge is a machine allowed to talk to the central. Only the hash of its token
// is kept.
type Edge struct {
	ID        string
	Name      string
	CreatedAt time.Time
	RevokedAt *time.Time
}

// Active reports whether the edge may still authenticate.
func (e Edge) Active() bool { return e.RevokedAt == nil }

// AddEdge registers an edge and returns its token, which is not stored and
// cannot be shown again.
func (s *Store) AddEdge(ctx context.Context, name string) (Edge, string, error) {
	if !namePattern.MatchString(name) {
		return Edge{}, "", fmt.Errorf("edge name %q: use 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit", name)
	}
	token, err := newToken()
	if err != nil {
		return Edge{}, "", err
	}
	id, err := randomHex(8)
	if err != nil {
		return Edge{}, "", err
	}
	e := Edge{ID: id, Name: name, CreatedAt: time.Now().UTC()}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO edges (id, name, token_hash, created_at) VALUES ($1, $2, $3, $4)`,
		e.ID, e.Name, hash(token), e.CreatedAt)
	if isUniqueViolation(err) {
		return Edge{}, "", fmt.Errorf("edge %q: %w", name, ErrExists)
	}
	if err != nil {
		return Edge{}, "", fmt.Errorf("add edge: %w", err)
	}
	return e, token, nil
}

// RevokeEdge stops the active edge of that name from authenticating.
func (s *Store) RevokeEdge(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE edges SET revoked_at = $2 WHERE name = $1 AND revoked_at IS NULL`,
		name, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("revoke edge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("edge %q: %w", name, ErrNotFound)
	}
	return nil
}

// ListEdges returns every edge, revoked ones included, oldest first.
func (s *Store) ListEdges(ctx context.Context) ([]Edge, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, created_at, revoked_at FROM edges ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list edges: %w", err)
	}
	defer rows.Close()
	var edges []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.ID, &e.Name, &e.CreatedAt, &e.RevokedAt); err != nil {
			return nil, fmt.Errorf("list edges: %w", err)
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// AuthenticateEdge returns the active edge that owns the token. A token nobody
// owns is ok=false with no error; an error means the database failed.
func (s *Store) AuthenticateEdge(ctx context.Context, token string) (edge Edge, ok bool, err error) {
	if !strings.HasPrefix(token, tokenPrefix) {
		return Edge{}, false, nil
	}
	err = s.pool.QueryRow(ctx,
		`SELECT id, name, created_at FROM edges WHERE token_hash = $1 AND revoked_at IS NULL`,
		hash(token)).Scan(&edge.ID, &edge.Name, &edge.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Edge{}, false, nil
	}
	if err != nil {
		return Edge{}, false, fmt.Errorf("authenticate edge: %w", err)
	}
	return edge, true, nil
}

func newToken() (string, error) { return newTokenWith(tokenPrefix) }

// newTokenWith returns 256 random bits behind a prefix that tells what the
// token is for.
func newTokenWith(prefix string) (string, error) {
	b := make([]byte, tokenByteSize)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// hash is SHA-256: a token carries 256 bits of randomness, a slow hash would
// add nothing.
func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// isUniqueViolation reports a duplicate key.
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}
