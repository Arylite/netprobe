package edge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

type fakePoster struct {
	mu      sync.Mutex
	batches [][]api.Result
	calls   int
	fail    func(call int) error
}

func (f *fakePoster) PostResults(_ context.Context, results []api.Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail != nil {
		if err := f.fail(f.calls); err != nil {
			return err
		}
	}
	f.batches = append(f.batches, results)
	return nil
}

func (f *fakePoster) sent() []api.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []api.Result
	for _, b := range f.batches {
		all = append(all, b...)
	}
	return all
}

func newReporter(p poster) *Reporter {
	return NewReporter(p, 10*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func numbered(n int) []api.Result {
	out := make([]api.Result, n)
	for i := range out {
		out[i] = api.Result{CheckID: fmt.Sprint(i), At: time.Now()}
	}
	return out
}

func TestFlushSendsInBatches(t *testing.T) {
	f := &fakePoster{}
	r := newReporter(f)
	for _, res := range numbered(2*api.MaxResultsPerBatch + 5) {
		r.Add(res)
	}
	r.flush(context.Background())
	if len(f.batches) != 3 || len(f.batches[0]) != api.MaxResultsPerBatch || len(f.batches[2]) != 5 {
		t.Fatalf("batches: %d", len(f.batches))
	}
	if r.queued() != 0 {
		t.Fatalf("%d results left", r.queued())
	}
}

func TestFailedBatchesAreKeptInOrderAndRetried(t *testing.T) {
	f := &fakePoster{fail: func(call int) error {
		if call == 1 {
			return errors.New("connection refused")
		}
		return nil
	}}
	r := newReporter(f)
	for _, res := range numbered(3) {
		r.Add(res)
	}
	r.flush(context.Background())
	if r.queued() != 3 || len(f.sent()) != 0 {
		t.Fatalf("queued %d, sent %d", r.queued(), len(f.sent()))
	}
	r.Add(api.Result{CheckID: "later", At: time.Now()})
	r.flush(context.Background())
	got := f.sent()
	if len(got) != 4 || got[0].CheckID != "0" || got[3].CheckID != "later" {
		t.Fatalf("sent %+v", got)
	}
}

func TestRefusedBatchesAreDroppedButUnauthorizedOnesAreKept(t *testing.T) {
	refusal := &StatusError{Op: "post results", Code: http.StatusBadRequest, Status: "400 Bad Request"}
	f := &fakePoster{fail: func(call int) error {
		if call == 1 {
			return refusal
		}
		return nil
	}}
	r := newReporter(f)
	r.Add(api.Result{CheckID: "poison", At: time.Now()})
	r.flush(context.Background())
	if r.queued() != 0 {
		t.Fatal("a refused batch was kept, it would block the queue forever")
	}

	denied := newReporter(&fakePoster{fail: func(int) error { return fmt.Errorf("post results: %w", ErrUnauthorized) }})
	denied.Add(api.Result{CheckID: "x", At: time.Now()})
	denied.flush(context.Background())
	if denied.queued() != 1 {
		t.Fatal("results were dropped on an authentication failure")
	}

	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		if refused(&StatusError{Code: code}) {
			t.Errorf("status %d must be retried", code)
		}
	}
}

func TestQueueIsBoundedAndDropsTheOldest(t *testing.T) {
	r := newReporter(&fakePoster{})
	for _, res := range numbered(maxQueued + 10) {
		r.Add(res)
	}
	if r.queued() != maxQueued {
		t.Fatalf("queued %d", r.queued())
	}
	if r.queue[0].CheckID != "10" {
		t.Fatalf("oldest kept: %s", r.queue[0].CheckID)
	}
	if r.dropped != 10 {
		t.Fatalf("dropped %d", r.dropped)
	}
}

func TestRunFlushesPeriodicallyAndOnShutdown(t *testing.T) {
	f := &fakePoster{}
	r := newReporter(f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	r.Add(api.Result{CheckID: "first", At: time.Now()})
	eventually(t, "the periodic flush", func() bool { return len(f.sent()) == 1 })

	r.Add(api.Result{CheckID: "last", At: time.Now()})
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if got := f.sent(); len(got) != 2 || got[1].CheckID != "last" {
		t.Fatalf("the final flush did not happen: %+v", got)
	}
}
