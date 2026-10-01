package auth

import (
	"sync"
	"time"
)

const maxTrackedKeys = 50_000

// Limiter refuses a key once it failed too many times within a window.
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]entry
}

type entry struct {
	failures int
	since    time.Time
}

// NewLimiter allows max failures per key in each window.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, now: time.Now, entries: map[string]entry{}}
}

// Blocked reports whether the key used up its failures.
func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok || l.now().Sub(e.since) >= l.window {
		return false
	}
	return e.failures >= l.max
}

// Fail records a failure of the key.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.since) >= l.window {
		if !ok && len(l.entries) >= maxTrackedKeys {
			l.purge(now)
			if len(l.entries) >= maxTrackedKeys {
				return // full of live entries: keep the memory bounded
			}
		}
		e = entry{since: now}
	}
	e.failures++
	l.entries[key] = e
}

// Reset forgets the failures of the key, after a success.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *Limiter) purge(now time.Time) {
	for k, e := range l.entries {
		if now.Sub(e.since) >= l.window {
			delete(l.entries, k)
		}
	}
}
