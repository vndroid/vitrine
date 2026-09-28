package auth

import (
	"sync"
	"time"
)

// Throttle limits failed admin logins per client like h5fs: after
// MaxFailures failures within Window further attempts are rejected for
// Lock. The state is kept in memory with a bounded number of entries.
type Throttle struct {
	MaxFailures int
	Window      time.Duration
	Lock        time.Duration
	MaxEntries  int

	mu      sync.Mutex
	clients map[string]*throttleEntry
	now     func() time.Time
}

type throttleEntry struct {
	first, last time.Time
	count       int
	lockedUntil time.Time
}

// NewThrottle returns a throttle with the h5fs limits.
func NewThrottle() *Throttle {
	return &Throttle{
		MaxFailures: 5,
		Window:      15 * time.Minute,
		Lock:        15 * time.Minute,
		MaxEntries:  10000,
		clients:     map[string]*throttleEntry{},
		now:         time.Now,
	}
}

// RetryAfter returns how long the client still has to wait, 0 if it may
// try to log in.
func (t *Throttle) RetryAfter(client string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.clients[client]
	if !ok {
		return 0
	}
	if d := e.lockedUntil.Sub(t.now()); d > 0 {
		return d
	}
	return 0
}

// Failure records a failed login.
func (t *Throttle) Failure(client string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.prune(now)
	e, ok := t.clients[client]
	if !ok || now.Sub(e.first) > t.Window {
		e = &throttleEntry{first: now}
		t.clients[client] = e
	}
	e.count++
	e.last = now
	if e.count >= t.MaxFailures {
		e.lockedUntil = now.Add(t.Lock)
		e.first = now
		e.count = 0
	}
}

// Reset forgets the failures of a client after a successful login.
func (t *Throttle) Reset(client string) {
	t.mu.Lock()
	delete(t.clients, client)
	t.mu.Unlock()
}

func (t *Throttle) prune(now time.Time) {
	if len(t.clients) < t.MaxEntries {
		return
	}
	var oldestKey string
	var oldest time.Time
	for k, e := range t.clients {
		if !now.Before(e.lockedUntil) && now.Sub(e.last) > t.Window {
			delete(t.clients, k)
			continue
		}
		if oldestKey == "" || e.last.Before(oldest) {
			oldestKey, oldest = k, e.last
		}
	}
	if len(t.clients) >= t.MaxEntries {
		delete(t.clients, oldestKey)
	}
}
