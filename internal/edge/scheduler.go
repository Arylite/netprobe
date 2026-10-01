package edge

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

const maxProbeTime = 30 * time.Second

// Scheduler runs each assigned check on its own interval and hands the results
// to a sink, which is called from several goroutines.
type Scheduler struct {
	measure Measurer
	sink    func(api.Result)
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

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
		ctx:     ctx,
		cancel:  cancel,
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
	interval := time.Duration(c.IntervalSeconds) * s.unit
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
	ctx, cancel := context.WithTimeout(ctx, min(interval, maxProbeTime))
	defer cancel()
	res := s.measure(ctx, c)
	if ctx.Err() == context.Canceled {
		return // stopped mid-measurement: the outcome would be an artefact
	}
	s.sink(res)
}
