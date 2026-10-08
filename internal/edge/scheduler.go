package edge

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

const (
	maxProbeTime = 30 * time.Second
	// maxIntervalSeconds keeps an interval a time.Duration can hold: the central
	// chooses it, and 1e10 seconds is already too much for the arithmetic.
	maxIntervalSeconds = 30 * 24 * 3600
	// maxConcurrent bounds the measurements that run at once, so that a long list
	// of checks that all time out cannot use up the sockets, the threads and the
	// memory of the edge. A check that waits for its turn runs late, not wrong.
	maxConcurrent = 64
)

// Scheduler runs each assigned check on its own interval and hands the results
// to a sink, which is called from several goroutines.
type Scheduler struct {
	measure Measurer
	sink    func(api.Result)
	log     *slog.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	slots   chan struct{}

	// unit is what an interval of 1 lasts; jitter spreads the first runs.
	unit   time.Duration
	jitter func(interval time.Duration) time.Duration

	mu   sync.Mutex
	jobs map[string]job
}

type job struct {
	check  api.Check
	cancel context.CancelFunc
}

// NewScheduler starts with no checks; the context ends every one of them.
func NewScheduler(ctx context.Context, measure Measurer, sink func(api.Result)) *Scheduler {
	ctx, cancel := context.WithCancel(ctx)
	return &Scheduler{
		measure: measure,
		sink:    sink,
		log:     slog.New(slog.DiscardHandler),
		ctx:     ctx,
		cancel:  cancel,
		slots:   make(chan struct{}, maxConcurrent),
		unit:    time.Second,
		jitter:  rand.N[time.Duration],
		jobs:    map[string]job{},
	}
}

// Apply makes the running checks match the assignment: new ones start, changed
// ones restart, missing ones stop.
func (s *Scheduler) Apply(checks []api.Check) {
	want := make(map[string]api.Check, len(checks))
	for _, c := range checks {
		want[c.ID] = c
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	for id, j := range s.jobs {
		if c, ok := want[id]; !ok || c != j.check {
			j.cancel()
			delete(s.jobs, id)
		}
	}
	for id, c := range want {
		if _, running := s.jobs[id]; running {
			continue
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.jobs[id] = job{check: c, cancel: cancel}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.loop(ctx, c)
		}()
	}
}

// Stop ends every check and waits until none is running.
func (s *Scheduler) Stop() {
	s.cancel()
	s.wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context, c api.Check) {
	interval := time.Duration(min(max(c.IntervalSeconds, 1), maxIntervalSeconds)) * s.unit
	timer := time.NewTimer(s.jitter(interval))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.run(ctx, c, interval)
		timer.Reset(interval)
	}
}

func (s *Scheduler) run(ctx context.Context, c api.Check, interval time.Duration) {
	// Wait for a turn before the clock of the measurement starts.
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return
	}
	ctx, cancel := context.WithTimeout(ctx, min(interval, maxProbeTime))
	defer cancel()
	res := s.measureSafely(ctx, c)
	if ctx.Err() == context.Canceled {
		return // stopped mid-measurement: the outcome would be an artefact
	}
	s.sink(res)
}

// measureSafely turns a panic of a probe into a failed result. A bug in one kind
// of check, triggered by what some server answered, must cost that check one
// run, and not take every other check, and the edge, down with it.
func (s *Scheduler) measureSafely(ctx context.Context, c api.Check) (res api.Result) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("a measurement panicked", "check", c.ID, "kind", c.Kind, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			res = api.Result{CheckID: c.ID, At: time.Now().UTC(), Error: clip(fmt.Sprintf("internal error: %v", r), api.MaxErrorLength)}
		}
	}()
	return s.measure(ctx, c)
}
