package archive

import (
	"errors"
	"io"
	"sync"
	"time"
)

// maxSlots caps "download.maxConcurrent".
const maxSlots = 64

// Slots limits concurrent downloads globally and per client, so one slow
// client can't block everybody.
type Slots struct {
	mu      sync.Mutex
	active  int
	clients map[string]int
}

// NewSlots returns an empty limiter.
func NewSlots() *Slots { return &Slots{clients: map[string]int{}} }

// Acquire takes a slot without waiting; release must be called when done.
func (s *Slots) Acquire(client string, maxConcurrent, maxPerClient int) (release func(), ok bool) {
	maxConcurrent = min(max(maxConcurrent, 1), maxSlots)
	maxPerClient = min(max(maxPerClient, 1), maxConcurrent)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active >= maxConcurrent || s.clients[client] >= maxPerClient {
		return nil, false
	}
	s.active++
	s.clients[client]++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.active--
			if s.clients[client]--; s.clients[client] <= 0 {
				delete(s.clients, client)
			}
			s.mu.Unlock()
		})
	}, true
}

// ErrTooSlow aborts a download that takes too long or is read too slowly.
var ErrTooSlow = errors.New("archive: download too slow or too long")

// RateWriter enforces the maximum duration and the minimum rate of a
// download. Writes block while the client does not read, so the elapsed
// time reflects the client's reading speed.
type RateWriter struct {
	W           io.Writer
	MaxDuration time.Duration
	MinRate     int64 // bytes per second, 0 disables
	Grace       time.Duration

	start time.Time
	n     int64
	now   func() time.Time
}

func (r *RateWriter) Write(p []byte) (int, error) {
	if r.now == nil {
		r.now = time.Now
	}
	if r.start.IsZero() {
		r.start = r.now()
	}
	n, err := r.W.Write(p)
	r.n += int64(n)
	if err != nil {
		return n, err
	}
	elapsed := r.now().Sub(r.start)
	if elapsed > r.MaxDuration ||
		r.MinRate > 0 && elapsed > r.Grace && float64(r.n)/elapsed.Seconds() < float64(r.MinRate) {
		return n, ErrTooSlow
	}
	return n, nil
}
