package edge

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

func TestSchedulerSurvivesAPanickingMeasurement(t *testing.T) {
	var c collector
	m := func(_ context.Context, chk api.Check) api.Result {
		if chk.ID == "bad" {
			panic("index out of range [5] with length 3")
		}
		return api.Result{CheckID: chk.ID, At: time.Now(), OK: true}
	}
	s := fastScheduler(context.Background(), m, c.add)
	defer s.Stop()
	s.Apply([]api.Check{check("bad"), check("good")})

	eventually(t, "the panicking check to be reported, more than once", func() bool { return c.count("bad") >= 2 })
	eventually(t, "the other check to keep running", func() bool { return c.count("good") >= 3 })
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.results {
		if r.CheckID != "bad" {
			continue
		}
		if r.OK || !strings.Contains(r.Error, "internal error") {
			t.Fatalf("a panic must be a failed result: %+v", r)
		}
		if err := r.Validate(); err != nil {
			t.Fatalf("the result of a panic must be valid: %v", err)
		}
	}
}

func TestSchedulerSurvivesAnAbsurdInterval(t *testing.T) {
	var started atomic.Int32
	m := func(_ context.Context, chk api.Check) api.Result {
		started.Add(1)
		return api.Result{CheckID: chk.ID, At: time.Now(), OK: true}
	}
	// The real unit: an interval of 1e10 seconds does not fit a time.Duration.
	s := NewScheduler(context.Background(), m, func(api.Result) {})
	s.jitter = func(time.Duration) time.Duration { return time.Millisecond }
	defer s.Stop()

	for _, seconds := range []int{10_000_000_000, math.MaxInt} {
		c := check("far")
		c.IntervalSeconds = seconds
		s.Apply([]api.Check{c}) // must not panic, in this goroutine or in the one it starts
	}
	eventually(t, "the check to run once", func() bool { return started.Load() >= 1 })
}

func TestSchedulerBoundsConcurrentMeasurements(t *testing.T) {
	var running, peak atomic.Int32
	release := make(chan struct{})
	m := func(ctx context.Context, chk api.Check) api.Result {
		n := running.Add(1)
		defer running.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return api.Result{CheckID: chk.ID, At: time.Now(), OK: true}
	}
	s := fastScheduler(context.Background(), m, func(api.Result) {})
	s.slots = make(chan struct{}, 3)
	defer s.Stop()
	defer close(release)

	var checks []api.Check
	for i := range 20 {
		checks = append(checks, check(string(rune('a'+i))))
	}
	s.Apply(checks)
	eventually(t, "the slots to fill", func() bool { return running.Load() == 3 })
	time.Sleep(50 * time.Millisecond) // time for a fourth to start, if it could
	if got := peak.Load(); got != 3 {
		t.Fatalf("%d measurements ran at once, the bound is 3", got)
	}
}

func TestStopDoesNotWaitForAFullQueueOfMeasurements(t *testing.T) {
	m := func(ctx context.Context, chk api.Check) api.Result {
		<-ctx.Done()
		return api.Result{CheckID: chk.ID, At: time.Now()}
	}
	s := fastScheduler(context.Background(), m, func(api.Result) {})
	s.slots = make(chan struct{}, 1)
	var checks []api.Check
	for i := range 10 {
		checks = append(checks, check(string(rune('a'+i))))
	}
	s.Apply(checks)
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop is stuck behind measurements that are waiting for a slot")
	}
}

func TestReporterNeverQueuesAResultTheCentralWouldRefuse(t *testing.T) {
	f := &fakePoster{}
	r := newReporter(f)
	bad := []api.Result{
		{CheckID: "negative", At: time.Now(), RTTMillis: -1},
		{CheckID: "nan", At: time.Now(), RTTMillis: math.NaN()},
		{CheckID: "inf", At: time.Now(), RTTMillis: math.Inf(1)},
		{CheckID: "", At: time.Now()},
		{CheckID: "no-time"},
		{CheckID: "long", At: time.Now(), Error: strings.Repeat("x", api.MaxErrorLength+1)},
	}
	for _, res := range bad {
		r.Add(res)
	}
	r.Add(api.Result{CheckID: "fine", At: time.Now(), OK: true})
	r.flush(context.Background())

	got := f.sent()
	if len(got) != 1 || got[0].CheckID != "fine" {
		t.Fatalf("sent %+v, want only the valid result", got)
	}
	if r.queued() != 0 {
		t.Fatalf("%d results stuck in the queue", r.queued())
	}
}

// A batch that cannot even be built stays out of the way of the ones behind it.
func TestReporterDoesNotRetryABatchThatFailsLocally(t *testing.T) {
	f := &fakePoster{fail: func(call int) error {
		if call == 1 {
			return errLocal
		}
		return nil
	}}
	r := newReporter(f)
	r.Add(api.Result{CheckID: "a", At: time.Now()})
	r.flush(context.Background())
	if r.queued() != 0 {
		t.Fatal("a batch that fails before it is sent was kept: it would be retried for ever")
	}
}

var errLocal = &BatchError{Err: errors.New("encode results: unsupported value")}

func TestPostResultsSaysWhenTheBatchCannotBeBuilt(t *testing.T) {
	c, err := NewClient("https://central.example.com", "tok")
	if err != nil {
		t.Fatal(err)
	}
	err = c.PostResults(context.Background(), []api.Result{{CheckID: "x", At: time.Now(), RTTMillis: -1}})
	var be *BatchError
	if !errors.As(err, &be) || !refused(err) {
		t.Fatalf("got %v, want a BatchError that is never retried", err)
	}
}

func TestProbeMeasurerClampsANegativeTime(t *testing.T) {
	if clampRTT(-5*time.Second) != 0 {
		t.Fatal("a negative round trip must become 0")
	}
	if clampRTT(2*time.Second) != 2*time.Second {
		t.Fatal("a valid round trip changed")
	}
}
