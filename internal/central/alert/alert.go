package alert

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/probe"
)

// Defaults of the settings.
const (
	DefaultFailures = 3
	DefaultSilence  = 5 * time.Minute
	DefaultInterval = 30 * time.Second
)

const (
	// lookback bounds what is read from the results.
	lookback = 24 * time.Hour
	// notifyWithin is how long after an event it may still be told.
	notifyWithin = time.Hour
	// freshRuns is how many intervals of a check may pass before its outcomes
	// stop saying anything about it.
	freshRuns = 3
)

// Config sets when an incident opens.
type Config struct {
	// Failures is how many failures in a row open an incident; zero means 3.
	Failures int
	// Silence is how long an edge may go without a result before it is
	// reported; zero means 5 minutes.
	Silence time.Duration
	// Interval is how often the results are looked at; zero means 30 seconds.
	Interval time.Duration
	// Sender delivers the notifications; nil posts to the channels' webhooks.
	Sender Sender
	// Policy is where those webhooks may not connect; nil means the default
	// ranges, a pointer to the zero value nowhere.
	Policy *probe.Policy
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Engine evaluates the results and notifies the channels.
type Engine struct {
	log      *slog.Logger
	store    *store.Store
	failures int
	silence  time.Duration
	interval time.Duration
	sender   Sender
	now      func() time.Time
}

// New builds an engine on top of a store.
func New(log *slog.Logger, st *store.Store, cfg Config) (*Engine, error) {
	if cfg.Failures < 0 || cfg.Silence < 0 || cfg.Interval < 0 {
		return nil, errors.New("alert settings must not be negative")
	}
	e := &Engine{
		log: log, store: st,
		failures: cfg.Failures, silence: cfg.Silence, interval: cfg.Interval,
		sender: cfg.Sender, now: cfg.Now,
	}
	if e.failures == 0 {
		e.failures = DefaultFailures
	}
	if e.silence == 0 {
		e.silence = DefaultSilence
	}
	if e.interval == 0 {
		e.interval = DefaultInterval
	}
	if e.sender == nil {
		policy := probe.DefaultPolicy()
		if cfg.Policy != nil {
			policy = *cfg.Policy
		}
		e.sender = NewWebhook(policy)
	}
	if e.now == nil {
		e.now = time.Now
	}
	return e, nil
}

// Run evaluates until the context ends.
func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		if err := e.Tick(ctx); err != nil && ctx.Err() == nil {
			e.log.Warn("alert evaluation failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick looks at the results once and tells the channels what is new, unless
// another central is doing it.
func (e *Engine) Tick(ctx context.Context) error {
	_, err := e.store.WithAlertLock(ctx, func(ctx context.Context) error {
		// Telling the channels does not wait for a failed evaluation: what was
		// recorded must still go out.
		return errors.Join(e.evaluate(ctx), e.deliver(ctx))
	})
	return err
}

type subject struct{ kind, check, edge string }

func (e *Engine) evaluate(ctx context.Context) error {
	now := e.now().UTC()
	since := now.Add(-lookback)

	checks, err := e.store.ListChecks(ctx)
	if err != nil {
		return err
	}
	edges, err := e.store.ListEdges(ctx)
	if err != nil {
		return err
	}
	pairs, err := e.store.RecentOutcomes(ctx, e.failures, since)
	if err != nil {
		return err
	}
	last, err := e.store.LastResultSince(ctx, since)
	if err != nil {
		return err
	}
	incidents, err := e.store.OpenIncidents(ctx)
	if err != nil {
		return err
	}

	byCheck := make(map[string]api.Check, len(checks))
	for _, c := range checks {
		byCheck[c.ID] = c
	}
	active := make(map[string]store.Edge, len(edges))
	for _, ed := range edges {
		if ed.Active() {
			active[ed.ID] = ed
		}
	}
	open := make(map[subject]store.Incident, len(incidents))
	for _, i := range incidents {
		open[subject{i.Kind, i.CheckID, i.EdgeID}] = i
	}

	var errs []error
	record := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	for _, p := range pairs {
		latest := p.Newest[0]
		// An edge that stopped reporting says nothing about its checks: the
		// incident of the edge covers it, and theirs stay as they are.
		if now.Sub(latest.At) > max(e.silence, freshRuns*time.Duration(byCheck[p.CheckID].IntervalSeconds)*time.Second) {
			continue
		}
		s := subject{store.IncidentCheck, p.CheckID, p.EdgeID}
		inc, isOpen := open[s]
		switch {
		case latest.OK && isOpen:
			record(e.resolve(ctx, inc, now, store.ResolutionRecovered))
		case !latest.OK && !isOpen && len(p.Newest) >= e.failures && allFailed(p.Newest):
			detail := latest.Error
			if detail == "" {
				detail = "failed"
			}
			record(e.open(ctx, s, active[p.EdgeID].Name, fmt.Sprintf("%d failures in a row, last: %s", e.failures, detail), now))
		}
	}

	// An edge with nothing to run is not silent, it has nothing to say.
	for _, ed := range active {
		s := subject{store.IncidentEdge, "", ed.ID}
		inc, isOpen := open[s]
		seen := ed.CreatedAt
		if at := last[ed.ID]; at.After(seen) {
			seen = at
		}
		quiet := now.Sub(seen)
		switch silent := len(checks) > 0 && quiet > e.silence; {
		case silent && !isOpen:
			record(e.open(ctx, s, ed.Name, fmt.Sprintf("no result for %s", quiet.Round(time.Second)), now))
		case !silent && isOpen:
			record(e.resolve(ctx, inc, now, store.ResolutionRecovered))
		}
	}

	// What an incident was about may be gone since.
	for s, inc := range open {
		switch _, ok := active[s.edge]; {
		case !ok:
			record(e.resolve(ctx, inc, now, store.ResolutionEdgeRevoked))
		case s.kind == store.IncidentCheck:
			if _, ok := byCheck[s.check]; !ok {
				record(e.resolve(ctx, inc, now, store.ResolutionCheckRemoved))
			}
		}
	}
	return errors.Join(errs...)
}

func allFailed(outcomes []store.Outcome) bool {
	for _, o := range outcomes {
		if o.OK {
			return false
		}
	}
	return true
}

func (e *Engine) open(ctx context.Context, s subject, edgeName, detail string, at time.Time) error {
	opened, err := e.store.OpenIncident(ctx, s.kind, s.check, s.edge, detail, at)
	if err != nil {
		return err
	}
	if opened {
		e.log.Info("incident opened", "kind", s.kind, "check_id", s.check, "edge", edgeName, "detail", detail)
	}
	return nil
}

func (e *Engine) resolve(ctx context.Context, i store.Incident, at time.Time, resolution string) error {
	if err := e.store.ResolveIncident(ctx, i.ID, at, resolution); err != nil {
		return err
	}
	e.log.Info("incident resolved", "kind", i.Kind, "check_id", i.CheckID, "edge", i.EdgeName, "resolution", resolution)
	return nil
}

// deliver tells the channels what they have not heard yet. A channel that
// fails is left alone until the next round, so that one that hangs does not
// hold up the others.
func (e *Engine) deliver(ctx context.Context) error {
	pending, err := e.store.PendingNotifications(ctx, e.now().UTC().Add(-notifyWithin))
	if err != nil {
		return err
	}
	failed := map[string]bool{}
	var errs []error
	for _, n := range pending {
		if failed[n.Channel.Name] {
			continue
		}
		if err := send(ctx, e.sender, n.Channel, NewPayload(n)); err != nil {
			failed[n.Channel.Name] = true
			e.log.Warn("notification failed, it will be tried again", "channel", n.Channel.Name, "event", n.Event, "incident_id", n.Incident.ID, "err", err)
			continue
		}
		if err := e.store.RecordDelivery(ctx, n.Incident.ID, n.Event, n.Channel.Name, e.now().UTC()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
