package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The roles of a user.
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

const sessionTokenPrefix = "ns_"

var (
	usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

	// ErrLastAdmin is returned when a change would leave no administrator.
	ErrLastAdmin = errors.New("this is the last administrator")
	// ErrSetupDone is returned when the first administrator exists already.
	ErrSetupDone = errors.New("the setup is already done")
)

// setupLock serialises the creation of the first administrator.
const setupLock = 7_304_203

// User is an account of the web UI. Its password hash never leaves the store
// except through PasswordHash.
type User struct {
	Username  string
	Role      string
	CreatedAt time.Time
}

// ValidRole reports whether role is one of the known roles.
func ValidRole(role string) bool { return role == RoleAdmin || role == RoleViewer }

// ValidateUsername reports why a name cannot be given to an account.
func ValidateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("username %q: use 1 to 64 lowercase letters, digits, dots, dashes or underscores, starting with a letter or digit", username)
	}
	return nil
}

// AddUser creates an account; the password is already hashed.
func (s *Store) AddUser(ctx context.Context, username, role, passwordHash string) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	if !ValidRole(role) {
		return fmt.Errorf("role %q: want %s or %s", role, RoleAdmin, RoleViewer)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (username, role, password_hash, created_at) VALUES ($1, $2, $3, $4)`,
		username, role, passwordHash, time.Now().UTC())
	if isUniqueViolation(err) {
		return fmt.Errorf("user %q: %w", username, ErrExists)
	}
	if err != nil {
		return fmt.Errorf("add user: %w", err)
	}
	return nil
}

// HasUsers reports whether any account exists.
func (s *Store) HasUsers(ctx context.Context) (bool, error) {
	var has bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&has); err != nil {
		return false, fmt.Errorf("read users: %w", err)
	}
	return has, nil
}

// AddFirstAdmin creates the administrator of a central that has no account yet.
// Two calls at once create one, and the other gets ErrSetupDone.
func (s *Store) AddFirstAdmin(ctx context.Context, username, passwordHash string) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("add first admin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, setupLock); err != nil {
		return fmt.Errorf("add first admin: %w", err)
	}
	var has bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&has); err != nil {
		return fmt.Errorf("add first admin: %w", err)
	}
	if has {
		return ErrSetupDone
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO users (username, role, password_hash, created_at) VALUES ($1, $2, $3, $4)`,
		username, RoleAdmin, passwordHash, time.Now().UTC()); err != nil {
		return fmt.Errorf("add first admin: %w", err)
	}
	return tx.Commit(ctx)
}

// PasswordHash returns the account and its stored hash.
func (s *Store) PasswordHash(ctx context.Context, username string) (User, string, error) {
	var u User
	var hash string
	err := s.pool.QueryRow(ctx,
		`SELECT username, role, created_at, password_hash FROM users WHERE username = $1`, username).
		Scan(&u.Username, &u.Role, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", fmt.Errorf("user %q: %w", username, ErrNotFound)
	}
	if err != nil {
		return User{}, "", fmt.Errorf("read user: %w", err)
	}
	return u, hash, nil
}

// ListUsers returns the accounts ordered by name.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT username, role, created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Username, &u.Role, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// DeleteUser removes an account and its sessions, unless it is the last
// administrator.
func (s *Store) DeleteUser(ctx context.Context, username string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the administrators so two deletions cannot both pass the check.
	rows, err := tx.Query(ctx, `SELECT username FROM users WHERE role = $1 FOR UPDATE`, RoleAdmin)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	admins := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("delete user: %w", err)
		}
		admins[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if admins[username] && len(admins) == 1 {
		return ErrLastAdmin
	}

	tag, err := tx.Exec(ctx, `DELETE FROM users WHERE username = $1`, username)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %q: %w", username, ErrNotFound)
	}
	return tx.Commit(ctx)
}

// SetPasswordHash replaces the hash and ends every session of the user.
func (s *Store) SetPasswordHash(ctx context.Context, username, passwordHash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE username = $1`, username, passwordHash)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %q: %w", username, ErrNotFound)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE username = $1`, username); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	return tx.Commit(ctx)
}

// CreateSession opens a session and returns its token, which is not stored.
func (s *Store) CreateSession(ctx context.Context, username string, ttl time.Duration) (string, time.Time, error) {
	token, err := newTokenWith(sessionTokenPrefix)
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)
	_, err = s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, username, created_at, expires_at) VALUES ($1, $2, $3, $4)`,
		hash(token), username, now, expires)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return token, expires, nil
}

// AuthenticateSession returns the user of a live session. A token nobody owns
// or that expired is ok=false with no error; an error means the database failed.
func (s *Store) AuthenticateSession(ctx context.Context, token string) (user User, ok bool, err error) {
	if !strings.HasPrefix(token, sessionTokenPrefix) {
		return User{}, false, nil
	}
	err = s.pool.QueryRow(ctx,
		`SELECT u.username, u.role, u.created_at
		   FROM sessions s JOIN users u ON u.username = s.username
		  WHERE s.token_hash = $1 AND s.expires_at > $2`,
		hash(token), time.Now().UTC()).Scan(&user.Username, &user.Role, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("authenticate session: %w", err)
	}
	return user, true, nil
}

// DeleteSession ends a session; an unknown token is not an error.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hash(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// PurgeSessions deletes the expired sessions and returns how many there were.
func (s *Store) PurgeSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("purge sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
