package edge

import (
	"context"
	"log/slog"
	"time"
)

// Agent polls the central on a fixed interval.
type Agent struct {
	Client   *Client
	Interval time.Duration
	Log      *slog.Logger
}

// Run polls until ctx is cancelled. A failed poll is logged and retried at the
// next tick.
func (a *Agent) Run(ctx context.Context) {
	ticker := time.NewTicker(a.Interval)
	defer ticker.Stop()
	for {
		a.poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *Agent) poll(ctx context.Context) {
	checks, changed, err := a.Client.Assignments(ctx)
	switch {
	case ctx.Err() != nil:
	case err != nil:
		a.Log.Warn("poll failed", "err", err)
	case changed:
		a.Log.Info("assignments updated", "checks", len(checks))
	}
}
