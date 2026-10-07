package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	maxChannelURLLength    = 2048
	maxChannelSecretLength = 256
)

// Channel is a webhook that is told when an incident opens or resolves.
type Channel struct {
	Name      string
	URL       string
	Secret    string // empty: what is sent is not signed
	CreatedAt time.Time
}

// ValidateChannelName reports why a name cannot be given to a channel.
func ValidateChannelName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("channel name %q: use 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit", name)
	}
	return nil
}

// ValidateChannelURL reports why a webhook address cannot be used. Credentials
// belong in the secret or the path, not in a user name: the address ends up in
// places where a password should not.
func ValidateChannelURL(raw string) error {
	u, err := url.Parse(raw)
	switch {
	case len(raw) > maxChannelURLLength:
		return fmt.Errorf("channel url is longer than %d bytes", maxChannelURLLength)
	case err != nil, u.Scheme != "http" && u.Scheme != "https", u.Host == "":
		return errors.New("channel url must be an http:// or https:// address")
	case u.User != nil:
		return errors.New("channel url must not carry a user name or a password")
	}
	return nil
}

// ValidateChannelSecret reports why a secret cannot be used to sign.
func ValidateChannelSecret(secret string) error {
	if len(secret) > maxChannelSecretLength {
		return fmt.Errorf("channel secret is longer than %d bytes", maxChannelSecretLength)
	}
	return nil
}

// AddChannel registers a webhook.
func (s *Store) AddChannel(ctx context.Context, name, rawURL, secret string) (Channel, error) {
	if err := ValidateChannelName(name); err != nil {
		return Channel{}, err
	}
	if err := ValidateChannelURL(rawURL); err != nil {
		return Channel{}, err
	}
	if err := ValidateChannelSecret(secret); err != nil {
		return Channel{}, err
	}
	c := Channel{Name: name, URL: rawURL, Secret: secret, CreatedAt: time.Now().UTC()}
	sealedURL, err := s.sealValue(c.URL, channelContext(name, "url"))
	if err != nil {
		return Channel{}, err
	}
	sealedSecret, err := s.sealValue(c.Secret, channelContext(name, "secret"))
	if err != nil {
		return Channel{}, err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO channels (name, url, secret, created_at) VALUES ($1, $2, $3, $4)`,
		c.Name, sealedURL, sealedSecret, c.CreatedAt)
	if isUniqueViolation(err) {
		return Channel{}, fmt.Errorf("channel %q: %w", name, ErrExists)
	}
	if err != nil {
		return Channel{}, fmt.Errorf("add channel: %w", err)
	}
	return c, nil
}

// RemoveChannel stops telling a webhook, and forgets what it was told.
func (s *Store) RemoveChannel(ctx context.Context, name string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("remove channel: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `DELETE FROM channels WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("remove channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("channel %q: %w", name, ErrNotFound)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM deliveries WHERE channel = $1`, name); err != nil {
		return fmt.Errorf("remove channel: %w", err)
	}
	return tx.Commit(ctx)
}

// ListChannels returns the webhooks ordered by name; never nil.
func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.pool.Query(ctx, `SELECT name, url, secret, created_at FROM channels ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()
	channels := []Channel{}
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.Name, &c.URL, &c.Secret, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("list channels: %w", err)
		}
		if err := s.openChannel(&c); err != nil {
			return nil, err
		}
		channels = append(channels, c)
	}
	return channels, rows.Err()
}

// GetChannel returns one webhook.
func (s *Store) GetChannel(ctx context.Context, name string) (Channel, error) {
	var c Channel
	err := s.pool.QueryRow(ctx, `SELECT name, url, secret, created_at FROM channels WHERE name = $1`, name).
		Scan(&c.Name, &c.URL, &c.Secret, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Channel{}, fmt.Errorf("channel %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return Channel{}, fmt.Errorf("read channel: %w", err)
	}
	return c, s.openChannel(&c)
}

// openChannel decrypts the address and the secret of a channel as read.
func (s *Store) openChannel(c *Channel) error {
	var err error
	if c.URL, err = s.openValue(c.URL, channelContext(c.Name, "url")); err != nil {
		return fmt.Errorf("channel %q: %w", c.Name, err)
	}
	if c.Secret, err = s.openValue(c.Secret, channelContext(c.Name, "secret")); err != nil {
		return fmt.Errorf("channel %q: %w", c.Name, err)
	}
	return nil
}

// EncryptChannels encrypts the channels written before there was a key, and
// returns how many it changed.
func (s *Store) EncryptChannels(ctx context.Context) (int, error) {
	if s.cipher == nil {
		return 0, errors.New("no key was given")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("encrypt channels: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT name, url, secret FROM channels FOR UPDATE`)
	if err != nil {
		return 0, fmt.Errorf("encrypt channels: %w", err)
	}
	var todo []Channel
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.Name, &c.URL, &c.Secret); err != nil {
			rows.Close()
			return 0, fmt.Errorf("encrypt channels: %w", err)
		}
		if !strings.HasPrefix(c.URL, sealedPrefix) {
			todo = append(todo, c)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("encrypt channels: %w", err)
	}
	for _, c := range todo {
		sealedURL, err := s.sealValue(c.URL, channelContext(c.Name, "url"))
		if err != nil {
			return 0, err
		}
		sealedSecret, err := s.sealValue(c.Secret, channelContext(c.Name, "secret"))
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `UPDATE channels SET url = $2, secret = $3 WHERE name = $1`, c.Name, sealedURL, sealedSecret); err != nil {
			return 0, fmt.Errorf("encrypt channels: %w", err)
		}
	}
	return len(todo), tx.Commit(ctx)
}

// ChannelEncryption counts the channels stored in clear and the ones encrypted.
func (s *Store) ChannelEncryption(ctx context.Context) (clear, sealed int, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE url NOT LIKE 'enc1:%'), count(*) FILTER (WHERE url LIKE 'enc1:%') FROM channels`).Scan(&clear, &sealed)
	if err != nil {
		return 0, 0, fmt.Errorf("read channels: %w", err)
	}
	return clear, sealed, nil
}
