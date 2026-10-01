package registry

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	tokenPrefix   = "np_"
	tokenByteSize = 32
	reloadEvery   = time.Second
	registryFile  = "edges.json"
	fileMode      = 0o600
	dirMode       = 0o700
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ErrNotFound is returned when no active edge has the given name.
var ErrNotFound = errors.New("no active edge with that name")

// Edge is a machine allowed to talk to the central. Only the hash of its token
// is kept.
type Edge struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	TokenHash string     `json:"token_hash"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// Active reports whether the edge may still authenticate.
func (e Edge) Active() bool { return e.RevokedAt == nil }

// Registry is the list of edges, stored in dir/edges.json. Several processes
// may use the same directory: changes made by another one are picked up within
// a second. Two writers at the same instant are not arbitrated.
type Registry struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	edges   []Edge
	byHash  map[string]int
	modTime time.Time
	size    int64
	checked time.Time
}

// Open loads the registry of dir. A directory without a file is an empty
// registry; the file is created by the first Add.
func Open(dir string) (*Registry, error) {
	r := &Registry{path: filepath.Join(dir, registryFile), now: time.Now}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Add registers an edge and returns its token, which is not stored and cannot
// be shown again.
func (r *Registry) Add(name string) (Edge, string, error) {
	if !namePattern.MatchString(name) {
		return Edge{}, "", fmt.Errorf("edge name %q: use 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.reload(); err != nil {
		return Edge{}, "", err
	}
	for _, e := range r.edges {
		if e.Name == name && e.Active() {
			return Edge{}, "", fmt.Errorf("edge %q already exists", name)
		}
	}
	token, err := newToken()
	if err != nil {
		return Edge{}, "", err
	}
	id, err := randomHex(8)
	if err != nil {
		return Edge{}, "", err
	}
	e := Edge{ID: id, Name: name, TokenHash: hash(token), CreatedAt: r.now().UTC()}
	if err := r.save(append(append([]Edge(nil), r.edges...), e)); err != nil {
		return Edge{}, "", err
	}
	return e, token, nil
}

// Revoke stops the active edge of that name from authenticating.
func (r *Registry) Revoke(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.reload(); err != nil {
		return err
	}
	next := append([]Edge(nil), r.edges...)
	for i, e := range next {
		if e.Name == name && e.Active() {
			t := r.now().UTC()
			next[i].RevokedAt = &t
			return r.save(next)
		}
	}
	return ErrNotFound
}

// List returns every edge, revoked ones included, oldest first.
func (r *Registry) List() ([]Edge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.reload(); err != nil {
		return nil, err
	}
	return append([]Edge(nil), r.edges...), nil
}

// Authenticate returns the active edge that owns the token. The file is
// looked at once per second at most.
func (r *Registry) Authenticate(token string) (Edge, bool) {
	if !strings.HasPrefix(token, tokenPrefix) {
		return Edge{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.now().Sub(r.checked) >= reloadEvery {
		if err := r.reload(); err != nil {
			return Edge{}, false
		}
	}
	i, ok := r.byHash[hash(token)]
	if !ok || !r.edges[i].Active() {
		return Edge{}, false
	}
	return r.edges[i], true
}

// reload reads the file again when it changed since the last read.
func (r *Registry) reload() error {
	r.checked = r.now()
	info, err := os.Stat(r.path)
	if errors.Is(err, os.ErrNotExist) {
		r.install(nil, time.Time{}, 0)
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat registry: %w", err)
	}
	if r.byHash != nil && info.ModTime().Equal(r.modTime) && info.Size() == r.size {
		return nil
	}
	raw, err := os.ReadFile(r.path)
	if err != nil {
		return fmt.Errorf("read registry: %w", err)
	}
	var edges []Edge
	if err := json.Unmarshal(raw, &edges); err != nil {
		return fmt.Errorf("parse registry %s: %w", r.path, err)
	}
	r.install(edges, info.ModTime(), info.Size())
	return nil
}

func (r *Registry) install(edges []Edge, mod time.Time, size int64) {
	r.edges, r.modTime, r.size = edges, mod, size
	r.byHash = make(map[string]int, len(edges))
	for i, e := range edges {
		r.byHash[e.TokenHash] = i
	}
}

// save replaces the file atomically, then adopts the new list.
func (r *Registry) save(edges []Edge) error {
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create registry directory: %w", err)
	}
	raw, err := json.MarshalIndent(edges, "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	tmp, err := os.CreateTemp(dir, registryFile+".*")
	if err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write registry: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write registry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	if err := os.Rename(tmp.Name(), r.path); err != nil {
		return fmt.Errorf("replace registry: %w", err)
	}
	info, err := os.Stat(r.path)
	if err != nil {
		return fmt.Errorf("stat registry: %w", err)
	}
	r.install(edges, info.ModTime(), info.Size())
	return nil
}

func newToken() (string, error) {
	b := make([]byte, tokenByteSize)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
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
