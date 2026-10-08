package edge

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Agent polls the central for checks, runs them and reports the results.
type Agent struct {
	Client         *Client
	Measure        Measurer
	Interval       time.Duration // between two polls
	ReportInterval time.Duration // between two reports
	Log            *slog.Logger
}

// Run works until ctx is cancelled. A failed poll is logged and retried at the
// next tick. On the way out the measurements stop first, then what is still
// queued is sent once.
func (a *Agent) Run(ctx context.Context) {
	reporter := NewReporter(a.Client, a.ReportInterval, a.Log)
	scheduler := NewScheduler(ctx, a.Measure, reporter.Add)
	scheduler.log = a.Log

	reporterCtx, stopReporter := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reporter.Run(reporterCtx)
	}()

	ticker := time.NewTicker(a.Interval)
	defer ticker.Stop()
	for {
		a.poll(ctx, scheduler)
		select {
		case <-ctx.Done():
			scheduler.Stop()
			stopReporter()
			wg.Wait()
			return
		case <-ticker.C:
		}
	}
}

// survive is deferred at the top of a step that must not take the edge down: a
// bug there costs one round, which is logged, and the next one starts afresh.
func survive(log *slog.Logger, step string) {
	if r := recover(); r != nil {
		log.Error("recovered from a panic", "step", step, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
	}
}

func (a *Agent) poll(ctx context.Context, scheduler *Scheduler) {
	defer survive(a.Log, "poll")
	checks, changed, err := a.Client.Assignments(ctx)
	switch {
	case ctx.Err() != nil:
	case err != nil:
		a.Log.Warn("poll failed", "err", err)
	case changed:
		a.Log.Info("assignments updated", "checks", len(checks))
		scheduler.Apply(checks)
	}
}
