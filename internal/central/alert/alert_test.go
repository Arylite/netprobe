package alert_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/alert"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// recorder is a sender that remembers what it was asked and can be made to fail.
type recorder struct {
	mu   sync.Mutex
	sent []sent
	fail map[string]bool
}

type sent struct {
	channel string
	payload alert.Payload
}

func (r *recorder) Send(_ context.Context, ch store.Channel, p alert.Payload) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail[ch.Name] {
		return errors.New("connection refused")
	}
	r.sent = append(r.sent, sent{ch.Name, p})
	return nil
}

func (r *recorder) events(channel string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, s := range r.sent {
		if s.channel == channel {
			out = append(out, s.payload.Event)
		}
	}
	return out
}

type fixture struct {
	t      *testing.T
	store  *store.Store
	engine *alert.Engine
	sender *recorder
	now    time.Time
	paris  store.Edge
}

// newFixture has one edge, paris, one check, web, run every 10 seconds, and one
// channel, ops, already there before anything goes wrong. A clock the test
// moves stands for time.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	st := storetest.Open(t)
	ctx := context.Background()
	f := &fixture{t: t, store: st, sender: &recorder{fail: map[string]bool{}}}
	f.now = time.Now().UTC().Truncate(time.Second)

	var err error
	if f.paris, _, err = st.AddEdge(ctx, "paris"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddCheck(ctx, api.Check{ID: "web", Kind: api.KindTCP, Target: "example.com:443", IntervalSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddChannel(ctx, "ops", "https://example.com/hook", ""); err != nil {
		t.Fatal(err)
	}
	// Let a channel created "now" count as older than what the tests do next.
	f.advance(time.Second)
	f.engine, err = alert.New(quiet, st, alert.Config{
		Failures: 3, Silence: 5 * time.Minute, Sender: f.sender,
		Now: func() time.Time { return f.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) advance(d time.Duration) { f.now = f.now.Add(d) }

// report stores outcomes of check web on an edge, the newest at the current
// time and each earlier one 10 seconds before the next.
func (f *fixture) report(edgeID string, oks ...bool) {
	f.t.Helper()
	var rs []api.Result
	for i, ok := range oks {
		r := api.Result{CheckID: "web", At: f.now.Add(-time.Duration(len(oks)-1-i) * 10 * time.Second), OK: ok, RTTMillis: 5}
		if !ok {
			r.Error = "connection refused"
		}
		rs = append(rs, r)
	}
	if err := f.store.InsertResults(context.Background(), edgeID, rs); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) tick() {
	f.t.Helper()
	if err := f.engine.Tick(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) open() []store.Incident {
	f.t.Helper()
	got, err := f.store.OpenIncidents(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	return got
}

func (f *fixture) incidents() []store.Incident {
	f.t.Helper()
	got, err := f.store.ListIncidents(context.Background(), false, 100)
	if err != nil {
		f.t.Fatal(err)
	}
	return got
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestACheckThatKeepsFailingOpensAnIncidentOnceAndResolvesWhenItRecovers(t *testing.T) {
	f := newFixture(t)

	f.report(f.paris.ID, true, false, false)
	f.tick()
	if got := f.open(); len(got) != 0 {
		t.Fatalf("two failures opened an incident: %+v", got)
	}

	f.advance(10 * time.Second)
	f.report(f.paris.ID, false)
	f.tick()
	got := f.open()
	if len(got) != 1 || got[0].Kind != store.IncidentCheck || got[0].CheckID != "web" || got[0].EdgeName != "paris" {
		t.Fatalf("three failures in a row: %+v", got)
	}
	if got[0].Detail != "3 failures in a row, last: connection refused" {
		t.Fatalf("detail: %q", got[0].Detail)
	}
	if ev := f.sender.events("ops"); !equal(ev, []string{"opened"}) {
		t.Fatalf("events: %v", ev)
	}
	if text := f.sender.sent[0].payload.Text; text != "[netprobe] web on paris is failing: 3 failures in a row, last: connection refused" {
		t.Fatalf("text: %q", text)
	}

	// Still failing: nothing new, nothing sent again.
	f.advance(10 * time.Second)
	f.report(f.paris.ID, false)
	f.tick()
	f.tick()
	if len(f.open()) != 1 || len(f.incidents()) != 1 || len(f.sender.sent) != 1 {
		t.Fatalf("incidents %d, sent %d", len(f.incidents()), len(f.sender.sent))
	}

	f.advance(10 * time.Second)
	f.report(f.paris.ID, true)
	f.tick()
	if len(f.open()) != 0 {
		t.Fatalf("still open after a success: %+v", f.open())
	}
	if ev := f.sender.events("ops"); !equal(ev, []string{"opened", "resolved"}) {
		t.Fatalf("events: %v", ev)
	}
	last := f.sender.sent[1].payload
	if last.Incident == nil || last.Incident.Resolution != store.ResolutionRecovered || last.Text != "[netprobe] web on paris is back to normal" {
		t.Fatalf("resolved payload: %+v", last)
	}
}

func TestASuccessBetweenFailuresBreaksTheStreak(t *testing.T) {
	f := newFixture(t)
	f.report(f.paris.ID, false, false, true, false, false)
	f.tick()
	if len(f.incidents()) != 0 {
		t.Fatalf("incidents: %+v", f.incidents())
	}
}

func TestAnEdgeThatStopsReportingOpensAnIncidentAndResolvesWhenItReturns(t *testing.T) {
	f := newFixture(t)
	f.report(f.paris.ID, true)
	f.tick()
	if len(f.incidents()) != 0 {
		t.Fatalf("a fresh edge: %+v", f.incidents())
	}

	f.advance(6 * time.Minute)
	f.tick()
	got := f.open()
	if len(got) != 1 || got[0].Kind != store.IncidentEdge || got[0].CheckID != "" || got[0].EdgeName != "paris" {
		t.Fatalf("a silent edge: %+v", got)
	}
	if ev := f.sender.events("ops"); !equal(ev, []string{"opened"}) {
		t.Fatalf("events: %v", ev)
	}

	f.advance(time.Minute)
	f.report(f.paris.ID, true)
	f.tick()
	if len(f.open()) != 0 {
		t.Fatalf("still open: %+v", f.open())
	}
	if ev := f.sender.events("ops"); !equal(ev, []string{"opened", "resolved"}) {
		t.Fatalf("events: %v", ev)
	}
}

func TestAnEdgeThatNeverReportedIsSilentOnceItIsOldEnough(t *testing.T) {
	f := newFixture(t)
	f.tick()
	if len(f.incidents()) != 0 {
		t.Fatalf("a new edge was called silent: %+v", f.incidents())
	}
	f.advance(6 * time.Minute)
	f.tick()
	if got := f.open(); len(got) != 1 || got[0].Kind != store.IncidentEdge {
		t.Fatalf("an edge that never reported: %+v", got)
	}
}

func TestNoCheckMeansNothingToReport(t *testing.T) {
	f := newFixture(t)
	if err := f.store.RemoveCheck(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	f.advance(time.Hour)
	f.tick()
	if len(f.incidents()) != 0 {
		t.Fatalf("an edge with nothing to run was called silent: %+v", f.incidents())
	}
}

func TestAnIncidentOfASilentEdgeKeepsItsChecksIncidentsOpen(t *testing.T) {
	f := newFixture(t)
	f.report(f.paris.ID, false, false, false)
	f.tick()
	if len(f.open()) != 1 {
		t.Fatalf("setup: %+v", f.open())
	}
	// The edge goes quiet: the failures of long ago must neither resolve the
	// incident nor open another.
	f.advance(10 * time.Minute)
	f.tick()
	kinds := map[string]int{}
	for _, i := range f.open() {
		kinds[i.Kind]++
	}
	if kinds[store.IncidentCheck] != 1 || kinds[store.IncidentEdge] != 1 || len(kinds) != 2 {
		t.Fatalf("open incidents: %v", kinds)
	}
}

func TestRevokingAnEdgeOrRemovingACheckClosesTheirIncidents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	lyon, _, err := f.store.AddEdge(ctx, "lyon")
	if err != nil {
		t.Fatal(err)
	}
	f.report(f.paris.ID, false, false, false)
	f.report(lyon.ID, false, false, false)
	f.tick()
	if len(f.open()) != 2 {
		t.Fatalf("setup: %+v", f.open())
	}

	if err := f.store.RevokeEdge(ctx, "lyon"); err != nil {
		t.Fatal(err)
	}
	f.tick()
	got := f.open()
	if len(got) != 1 || got[0].EdgeName != "paris" {
		t.Fatalf("after revoking lyon: %+v", got)
	}

	if err := f.store.RemoveCheck(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if len(f.open()) != 0 {
		t.Fatalf("after removing the check: %+v", f.open())
	}
	resolutions := map[string]string{}
	for _, i := range f.incidents() {
		resolutions[i.EdgeName] = i.Resolution
	}
	if resolutions["lyon"] != store.ResolutionEdgeRevoked || resolutions["paris"] != store.ResolutionCheckRemoved {
		t.Fatalf("resolutions: %v", resolutions)
	}
	if got := f.sender.sent[len(f.sender.sent)-1].payload.Text; got == "" {
		t.Fatal("empty text")
	}
}

func TestAChannelThatFailsIsTriedAgainAndDoesNotHoldBackTheOthers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.store.AddChannel(ctx, "chat", "https://example.com/chat", ""); err != nil {
		t.Fatal(err)
	}
	f.advance(time.Second)
	f.sender.fail["ops"] = true

	f.report(f.paris.ID, false, false, false)
	f.tick()
	if ev := f.sender.events("chat"); !equal(ev, []string{"opened"}) {
		t.Fatalf("chat: %v", ev)
	}
	if ev := f.sender.events("ops"); len(ev) != 0 {
		t.Fatalf("ops should have failed: %v", ev)
	}

	// Back up: it hears of what it missed, once, and chat is not told again.
	f.sender.fail["ops"] = false
	f.advance(30 * time.Second)
	f.report(f.paris.ID, false)
	f.tick()
	f.tick()
	if ev := f.sender.events("ops"); !equal(ev, []string{"opened"}) {
		t.Fatalf("ops after recovery: %v", ev)
	}
	if ev := f.sender.events("chat"); !equal(ev, []string{"opened"}) {
		t.Fatalf("chat was told again: %v", ev)
	}
}

func TestNewsTooOldIsNotSent(t *testing.T) {
	f := newFixture(t)
	f.sender.fail["ops"] = true
	f.report(f.paris.ID, false, false, false)
	f.tick()

	// The webhook comes back two hours later, to an incident that is over.
	f.sender.fail["ops"] = false
	f.advance(2 * time.Hour)
	f.report(f.paris.ID, true)
	f.tick()
	if ev := f.sender.events("ops"); len(ev) != 0 {
		t.Fatalf("stale news was sent: %v", ev)
	}
}

func TestSeveralCentralsTellEachEventOnce(t *testing.T) {
	f := newFixture(t)
	f.report(f.paris.ID, false, false, false)

	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() { _ = f.engine.Tick(context.Background()) })
	}
	wg.Wait()
	f.tick()
	if ev := f.sender.events("ops"); !equal(ev, []string{"opened"}) {
		t.Fatalf("events: %v", ev)
	}
	if got := f.incidents(); len(got) != 1 {
		t.Fatalf("incidents: %+v", got)
	}
}

func TestTestSendsATestNotification(t *testing.T) {
	f := newFixture(t)
	ch, err := f.store.GetChannel(context.Background(), "ops")
	if err != nil {
		t.Fatal(err)
	}
	if err := alert.SendTest(context.Background(), f.sender, ch); err != nil {
		t.Fatal(err)
	}
	if ev := f.sender.events("ops"); !equal(ev, []string{alert.EventTest}) || f.sender.sent[0].payload.Incident != nil {
		t.Fatalf("sent: %+v", f.sender.sent)
	}
	f.sender.fail["ops"] = true
	if err := alert.SendTest(context.Background(), f.sender, ch); err == nil {
		t.Fatal("a failure was hidden")
	}
}

func TestNewRefusesNegativeSettings(t *testing.T) {
	st := storetest.Open(t)
	for _, cfg := range []alert.Config{{Failures: -1}, {Silence: -time.Second}, {Interval: -time.Second}} {
		if _, err := alert.New(quiet, st, cfg); err == nil {
			t.Fatalf("%+v was accepted", cfg)
		}
	}
	if _, err := alert.New(quiet, st, alert.Config{}); err != nil {
		t.Fatal(err)
	}
}
