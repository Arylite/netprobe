package edge

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/probe"
)

type collector struct {
	mu      sync.Mutex
	results []api.Result
}

func (c *collector) add(r api.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results = append(c.results, r)
}

func (c *collector) count(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, r := range c.results {
		if r.CheckID == id {
			n++
		}
	}
	return n
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func fastScheduler(ctx context.Context, m Measurer, sink func(api.Result)) *Scheduler {
	s := NewScheduler(ctx, m, sink)
	s.unit = 5 * time.Millisecond
	s.jitter = func(time.Duration) time.Duration { return time.Millisecond }
	return s
}

func okMeasurer(_ context.Context, c api.Check) api.Result {
	return api.Result{CheckID: c.ID, At: time.Now(), OK: true}
}

func check(id string) api.Check {
	return api.Check{ID: id, Kind: api.KindTCP, Target: "127.0.0.1:1", IntervalSeconds: 1}
}

func TestSchedulerRunsChecksRepeatedly(t *testing.T) {
	var c collector
	s := fastScheduler(context.Background(), okMeasurer, c.add)
	defer s.Stop()
	s.Apply([]api.Check{check("a"), check("b")})
	eventually(t, "three results of each check", func() bool { return c.count("a") >= 3 && c.count("b") >= 3 })
}

func TestSchedulerStopsRemovedChecks(t *testing.T) {
	var c collector
	s := fastScheduler(context.Background(), okMeasurer, c.add)
	defer s.Stop()
	s.Apply([]api.Check{check("a"), check("b")})
	eventually(t, "both checks to run", func() bool { return c.count("a") > 0 && c.count("b") > 0 })

	s.Apply([]api.Check{check("a")})
	time.Sleep(30 * time.Millisecond) // lets a run that was already measuring end
	before := c.count("b")
	eventually(t, "a to keep running", func() bool { return c.count("a") > before })
	if after := c.count("b"); after > before {
		t.Fatalf("b kept running: %d then %d", before, after)
	}
}

func TestSchedulerRestartsChangedChecks(t *testing.T) {
	var mu sync.Mutex
	targets := map[string]bool{}
	m := func(_ context.Context, c api.Check) api.Result {
		mu.Lock()
		targets[c.Target] = true
		mu.Unlock()
		return api.Result{CheckID: c.ID, At: time.Now(), OK: true}
	}
	s := fastScheduler(context.Background(), m, func(api.Result) {})
	defer s.Stop()

	a := check("a")
	s.Apply([]api.Check{a})
	eventually(t, "the first target", func() bool { mu.Lock(); defer mu.Unlock(); return targets[a.Target] })
	a.Target = "127.0.0.1:2"
	s.Apply([]api.Check{a})
	eventually(t, "the new target", func() bool { mu.Lock(); defer mu.Unlock(); return targets[a.Target] })
}

func TestSchedulerKeepsUnchangedChecksRunning(t *testing.T) {
	var mu sync.Mutex
	starts := 0
	release := make(chan struct{})
	m := func(ctx context.Context, c api.Check) api.Result {
		mu.Lock()
		starts++
		mu.Unlock()
		select {
		case <-release:
		case <-ctx.Done():
		}
		return api.Result{CheckID: c.ID, At: time.Now(), OK: true}
	}
	s := fastScheduler(context.Background(), m, func(api.Result) {})
	defer s.Stop()
	defer close(release)

	a := check("a")
	s.Apply([]api.Check{a})
	eventually(t, "a measurement to start", func() bool { mu.Lock(); defer mu.Unlock(); return starts == 1 })
	for i := 0; i < 5; i++ {
		s.Apply([]api.Check{a})
	}
	mu.Lock()
	defer mu.Unlock()
	if starts != 1 {
		t.Fatalf("an unchanged check was restarted: %d starts", starts)
	}
}

func TestStopWaitsAndSilencesTheSink(t *testing.T) {
	var c collector
	s := fastScheduler(context.Background(), okMeasurer, c.add)
	s.Apply([]api.Check{check("a")})
	eventually(t, "a result", func() bool { return c.count("a") > 0 })
	s.Stop()
	before := c.count("a")
	time.Sleep(30 * time.Millisecond)
	if c.count("a") != before {
		t.Fatal("results arrived after Stop")
	}
	s.Apply([]api.Check{check("c")})
	time.Sleep(20 * time.Millisecond)
	if c.count("c") != 0 {
		t.Fatal("a check started after Stop")
	}
}

func TestResultsOfAnInterruptedMeasurementAreDropped(t *testing.T) {
	var c collector
	started := make(chan struct{})
	m := func(ctx context.Context, chk api.Check) api.Result {
		close(started)
		<-ctx.Done()
		return api.Result{CheckID: chk.ID, At: time.Now(), Error: "context canceled"}
	}
	s := fastScheduler(context.Background(), m, c.add)
	s.Apply([]api.Check{check("a")})
	<-started
	s.Stop()
	if c.count("a") != 0 {
		t.Fatal("the artefact of an interrupted measurement was reported")
	}
}

func TestProbeMeasurer(t *testing.T) {
	m := ProbeMeasurer(probe.New(probe.Policy{}, time.Second))

	res := m(context.Background(), api.Check{ID: "x", Kind: "dns", Target: "example.com", IntervalSeconds: 1})
	if res.OK || !strings.Contains(res.Error, "unsupported") || res.CheckID != "x" || res.At.IsZero() {
		t.Fatalf("unsupported kind: %+v", res)
	}

	res = m(context.Background(), api.Check{ID: "t", Kind: api.KindTCP, Target: "no-port", IntervalSeconds: 1})
	if res.OK || res.Error == "" {
		t.Fatalf("bad tcp target: %+v", res)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("a failed measurement must still be a valid result: %v", err)
	}
}

func TestClip(t *testing.T) {
	long := strings.Repeat("\xc3\xa9", 400)
	got := clip(long, api.MaxErrorLength)
	if len(got) > api.MaxErrorLength || got != strings.Repeat("\xc3\xa9", len(got)/2) {
		t.Fatalf("clip cut a character: %d bytes", len(got))
	}
	if clip("a\xffb", 10) != "ab" {
		t.Fatal("invalid UTF-8 was kept")
	}
	if clip("short", 10) != "short" {
		t.Fatal("a short string changed")
	}
}
