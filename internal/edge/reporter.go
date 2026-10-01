package edge

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

const (
	maxQueued    = 10_000
	finalFlushIn = 5 * time.Second
)

type poster interface {
	PostResults(ctx context.Context, results []api.Result) error
}

// Reporter queues results and sends them to the central in batches. While the
// central is unreachable the results wait in memory, up to maxQueued; the
// oldest are dropped first.
type Reporter struct {
	post     poster
	interval time.Duration
	log      *slog.Logger

	mu      sync.Mutex
	queue   []api.Result
	dropped int
}

// NewReporter sends what was queued every interval.
func NewReporter(post poster, interval time.Duration, log *slog.Logger) *Reporter {
	return &Reporter{post: post, interval: interval, log: log}
}

// Add queues one result; it is safe to call from several goroutines.
func (r *Reporter) Add(res api.Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queue = append(r.queue, res)
	r.trim()
}

// Run flushes on every tick, and a last time when ctx ends.
func (r *Reporter) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), finalFlushIn)
			defer cancel()
			r.flush(final)
			return
		case <-ticker.C:
			r.flush(ctx)
		}
	}
}

func (r *Reporter) flush(ctx context.Context) {
	for {
		batch := r.take()
		if len(batch) == 0 {
			return
		}
		err := r.post.PostResults(ctx, batch)
		if err == nil {
			continue
		}
		if refused(err) {
			r.log.Warn("batch refused by the central and dropped", "results", len(batch), "err", err)
			continue
		}
		r.requeue(batch)
		r.log.Warn("report failed, will retry", "queued", r.queued(), "err", err)
		return
	}
}

// refused reports whether the central rejected the batch itself, so that
// sending it again can only fail again.
func refused(err error) bool {
	var se *StatusError
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code {
	case http.StatusUnauthorized, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return false
	}
	return se.Code >= 400 && se.Code < 500
}

func (r *Reporter) take() []api.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dropped > 0 {
		r.log.Warn("results dropped, the central has been unreachable for too long", "dropped", r.dropped)
		r.dropped = 0
	}
	n := min(len(r.queue), api.MaxResultsPerBatch)
	batch := append([]api.Result(nil), r.queue[:n]...)
	r.queue = r.queue[n:]
	return batch
}

func (r *Reporter) requeue(batch []api.Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queue = append(append([]api.Result(nil), batch...), r.queue...)
	r.trim()
}

func (r *Reporter) queued() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queue)
}

// trim drops the oldest results beyond the limit; the caller holds the lock.
func (r *Reporter) trim() {
	if over := len(r.queue) - maxQueued; over > 0 {
		r.queue = r.queue[over:]
		r.dropped += over
	}
}
