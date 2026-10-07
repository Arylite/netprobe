package store_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func TestChannels(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()

	if got, err := s.ListChannels(ctx); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty list: %v, %v", got, err)
	}
	if _, err := s.AddChannel(ctx, "ops", "https://hooks.example.com/x", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddChannel(ctx, "chat", "http://127.0.0.1:9000/in", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ListChannels(ctx)
	if len(got) != 2 || got[0].Name != "chat" || got[1].Secret != "s3cret" {
		t.Fatalf("list: %+v", got)
	}
	if c, err := s.GetChannel(ctx, "ops"); err != nil || c.URL != "https://hooks.example.com/x" {
		t.Fatalf("get: %+v, %v", c, err)
	}
	if _, err := s.GetChannel(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get unknown: %v", err)
	}
	if _, err := s.AddChannel(ctx, "ops", "https://other.example.com", ""); !errors.Is(err, store.ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}
	for _, bad := range []struct{ name, url string }{
		{"Bad Name", "https://example.com"},
		{"ok", "ftp://example.com"},
		{"ok", "https://user:pass@example.com"},
		{"ok", "not a url"},
		{"ok", "https://" + strings.Repeat("a", 3000)},
	} {
		if _, err := s.AddChannel(ctx, bad.name, bad.url, ""); err == nil {
			t.Fatalf("%q %q was accepted", bad.name, bad.url)
		}
	}
	if err := s.RemoveChannel(ctx, "chat"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveChannel(ctx, "chat"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestIncidentsOpenOnceAndResolve(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	edge, _, _ := s.AddEdge(ctx, "paris")
	now := time.Now().UTC().Truncate(time.Millisecond)

	if opened, err := s.OpenIncident(ctx, store.IncidentCheck, "web", edge.ID, "connection refused", now); err != nil || !opened {
		t.Fatalf("first open: %v, %v", opened, err)
	}
	if opened, err := s.OpenIncident(ctx, store.IncidentCheck, "web", edge.ID, "again", now.Add(time.Second)); err != nil || opened {
		t.Fatalf("second open: %v, %v", opened, err)
	}
	// Another subject opens its own.
	if opened, _ := s.OpenIncident(ctx, store.IncidentEdge, "", edge.ID, "silent", now); !opened {
		t.Fatal("an edge incident was refused next to a check incident")
	}

	open, err := s.OpenIncidents(ctx)
	if err != nil || len(open) != 2 {
		t.Fatalf("open: %+v, %v", open, err)
	}
	var check store.Incident
	for _, i := range open {
		if i.Kind == store.IncidentCheck {
			check = i
		}
	}
	if check.CheckID != "web" || check.EdgeName != "paris" || check.Detail != "connection refused" || !check.Open() {
		t.Fatalf("incident: %+v", check)
	}

	if err := s.ResolveIncident(ctx, check.ID, now.Add(time.Minute), store.ResolutionRecovered); err != nil {
		t.Fatal(err)
	}
	// Resolving twice keeps the first answer.
	if err := s.ResolveIncident(ctx, check.ID, now.Add(time.Hour), "later"); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListIncidents(ctx, false, 10)
	if len(all) != 2 {
		t.Fatalf("all: %+v", all)
	}
	for _, i := range all {
		if i.ID == check.ID && (i.Open() || i.Resolution != store.ResolutionRecovered || !i.ResolvedAt.Equal(now.Add(time.Minute))) {
			t.Fatalf("resolved incident: %+v", i)
		}
	}
	if only, _ := s.ListIncidents(ctx, true, 10); len(only) != 1 || only[0].Kind != store.IncidentEdge {
		t.Fatalf("open only: %+v", only)
	}
	if limited, _ := s.ListIncidents(ctx, false, 1); len(limited) != 1 {
		t.Fatalf("limit: %+v", limited)
	}

	// Once resolved, the same subject can fail again.
	if opened, _ := s.OpenIncident(ctx, store.IncidentCheck, "web", edge.ID, "again", now.Add(2*time.Minute)); !opened {
		t.Fatal("a resolved subject could not open a new incident")
	}
}

func TestOpenIncidentOnceWhenCalledTogether(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	edge, _, _ := s.AddEdge(ctx, "paris")

	var wg sync.WaitGroup
	var mu sync.Mutex
	opened := 0
	for range 8 {
		wg.Go(func() {
			ok, err := s.OpenIncident(ctx, store.IncidentCheck, "web", edge.ID, "down", time.Now())
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if ok {
				opened++
			}
		})
	}
	wg.Wait()
	if opened != 1 {
		t.Fatalf("%d incidents were opened for one subject", opened)
	}
}

func TestIncidentDetailIsTruncatedOnACharacter(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	edge, _, _ := s.AddEdge(ctx, "paris")
	detail := strings.Repeat("\u00e9", 400) // 800 bytes
	if _, err := s.OpenIncident(ctx, store.IncidentCheck, "web", edge.ID, detail, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := s.OpenIncidents(ctx)
	if len(got) != 1 || len(got[0].Detail) > 512 || strings.ContainsRune(got[0].Detail, '\uFFFD') {
		t.Fatalf("detail: %d bytes", len(got[0].Detail))
	}
}

func TestPendingNotifications(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	edge, _, _ := s.AddEdge(ctx, "paris")
	start := time.Now().UTC().Add(-10 * time.Minute)

	// Nobody to tell while there is no channel.
	if _, err := s.OpenIncident(ctx, store.IncidentCheck, "old", edge.ID, "down", start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.PendingNotifications(ctx, start.Add(-24*time.Hour)); err != nil || len(got) != 0 {
		t.Fatalf("without channel: %+v, %v", got, err)
	}

	if _, err := s.AddChannel(ctx, "ops", "https://example.com/hook", ""); err != nil {
		t.Fatal(err)
	}
	// An incident that began before the channel existed is not news to it.
	if got, _ := s.PendingNotifications(ctx, start.Add(-24*time.Hour)); len(got) != 0 {
		t.Fatalf("incident older than the channel: %+v", got)
	}

	now := time.Now().UTC()
	if _, err := s.OpenIncident(ctx, store.IncidentCheck, "web", edge.ID, "down", now); err != nil {
		t.Fatal(err)
	}
	got, err := s.PendingNotifications(ctx, now.Add(-time.Hour))
	if err != nil || len(got) != 1 {
		t.Fatalf("opened: %+v, %v", got, err)
	}
	n := got[0]
	if n.Event != store.EventOpened || n.Channel.Name != "ops" || n.Channel.URL != "https://example.com/hook" || n.Incident.CheckID != "web" || n.Incident.EdgeName != "paris" {
		t.Fatalf("notification: %+v", n)
	}

	if err := s.RecordDelivery(ctx, n.Incident.ID, n.Event, n.Channel.Name, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDelivery(ctx, n.Incident.ID, n.Event, n.Channel.Name, now); err != nil {
		t.Fatalf("recording twice: %v", err)
	}
	if got, _ := s.PendingNotifications(ctx, now.Add(-time.Hour)); len(got) != 0 {
		t.Fatalf("after delivery: %+v", got)
	}

	// Resolving makes a second event, for the same channel.
	if err := s.ResolveIncident(ctx, n.Incident.ID, now.Add(time.Minute), store.ResolutionRecovered); err != nil {
		t.Fatal(err)
	}
	got, _ = s.PendingNotifications(ctx, now.Add(-time.Hour))
	if len(got) != 1 || got[0].Event != store.EventResolved || !got[0].At().Equal(now.Add(time.Minute).Truncate(time.Microsecond)) {
		t.Fatalf("resolved: %+v", got)
	}
	// Too old to be worth sending.
	if got, _ := s.PendingNotifications(ctx, now.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("stale: %+v", got)
	}

	// An incident whose opening the channel never heard of is not announced as
	// resolved either.
	if _, err := s.OpenIncident(ctx, store.IncidentEdge, "", edge.ID, "silent", now); err != nil {
		t.Fatal(err)
	}
	var unheard store.Incident
	pending, _ := s.PendingNotifications(ctx, now.Add(-time.Hour))
	for _, p := range pending {
		if p.Incident.Kind == store.IncidentEdge {
			unheard = p.Incident
		}
	}
	if unheard.ID == 0 {
		t.Fatalf("the opening of the second incident is not pending: %+v", pending)
	}
	if err := s.ResolveIncident(ctx, unheard.ID, now.Add(time.Minute), store.ResolutionRecovered); err != nil {
		t.Fatal(err)
	}
	got, _ = s.PendingNotifications(ctx, now.Add(-time.Hour))
	if len(got) != 2 {
		t.Fatalf("pending: %+v", got)
	}
	for _, p := range got {
		if p.Incident.ID == unheard.ID && p.Event != store.EventOpened {
			t.Fatalf("resolved without having been opened: %+v", p)
		}
	}

	// Removing the channel forgets what it was told: a new one of that name
	// starts clean and only hears what comes after it.
	if err := s.RemoveChannel(ctx, "ops"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.PendingNotifications(ctx, now.Add(-time.Hour)); len(got) != 0 {
		t.Fatalf("without channel: %+v", got)
	}
}

func TestAlertLockIsExclusive(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()

	inside := false
	ran, err := s.WithAlertLock(ctx, func(ctx context.Context) error {
		inside = true
		other, err := s.WithAlertLock(ctx, func(context.Context) error {
			t.Error("the lock was taken twice")
			return nil
		})
		if err != nil || other {
			t.Errorf("second holder: %v, %v", other, err)
		}
		return nil
	})
	if err != nil || !ran || !inside {
		t.Fatalf("first holder: %v, %v", ran, err)
	}
	// Released when the first holder is done.
	ran, err = s.WithAlertLock(ctx, func(context.Context) error { return nil })
	if err != nil || !ran {
		t.Fatalf("after release: %v, %v", ran, err)
	}
}

func TestRecentOutcomesAndSummaries(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	paris, _, _ := s.AddEdge(ctx, "paris")
	lyon, _, _ := s.AddEdge(ctx, "lyon")
	gone, _, _ := s.AddEdge(ctx, "gone")
	if err := s.RevokeEdge(ctx, "gone"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"web", "db"} {
		if err := s.AddCheck(ctx, api.Check{ID: id, Kind: api.KindTCP, Target: "example.com:443", IntervalSeconds: 10}); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	at := func(ago time.Duration) time.Time { return now.Add(-ago) }
	report := func(edge string, rs ...api.Result) {
		t.Helper()
		if err := s.InsertResults(ctx, edge, rs); err != nil {
			t.Fatal(err)
		}
	}
	report(paris.ID,
		api.Result{CheckID: "web", At: at(4 * time.Minute), OK: true, RTTMillis: 10},
		api.Result{CheckID: "web", At: at(3 * time.Minute), OK: false, RTTMillis: 1, Error: "refused"},
		api.Result{CheckID: "web", At: at(2 * time.Minute), OK: false, RTTMillis: 1, Error: "timeout"},
		api.Result{CheckID: "web", At: at(1 * time.Minute), OK: true, RTTMillis: 30},
		api.Result{CheckID: "removed", At: at(1 * time.Minute), OK: true, RTTMillis: 5},
		api.Result{CheckID: "web", At: at(48 * time.Hour), OK: true, RTTMillis: 99},
	)
	report(lyon.ID, api.Result{CheckID: "web", At: at(time.Minute), OK: false, RTTMillis: 2, Error: "no route"})
	report(gone.ID, api.Result{CheckID: "web", At: at(time.Minute), OK: false, RTTMillis: 2, Error: "ignored"})

	pairs, err := s.RecentOutcomes(ctx, 3, at(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 {
		t.Fatalf("pairs: %+v", pairs)
	}
	var parisWeb, lyonWeb store.PairOutcomes
	for _, p := range pairs {
		switch p.EdgeID {
		case paris.ID:
			parisWeb = p
		case lyon.ID:
			lyonWeb = p
		default:
			t.Fatalf("an unexpected edge: %+v", p)
		}
		if p.CheckID != "web" {
			t.Fatalf("an unexpected check: %+v", p)
		}
	}
	// The 3 latest, newest first; the one of 48 hours ago is out of the window.
	if len(parisWeb.Newest) != 3 || !parisWeb.Newest[0].OK || parisWeb.Newest[1].Error != "timeout" || parisWeb.Newest[2].Error != "refused" {
		t.Fatalf("paris: %+v", parisWeb)
	}
	if len(lyonWeb.Newest) != 1 || lyonWeb.Newest[0].OK {
		t.Fatalf("lyon: %+v", lyonWeb)
	}

	last, err := s.LastResultSince(ctx, at(24*time.Hour))
	if err != nil || !last[paris.ID].Equal(at(time.Minute)) || len(last) != 3 {
		t.Fatalf("last: %+v, %v", last, err)
	}

	sums, err := s.Summaries(ctx, at(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range sums {
		if m.CheckID == "web" && m.EdgeID == paris.ID {
			found = true
			if m.Samples != 4 || m.Succeeded != 2 || !m.LastOK || m.LastRTT != 30 || !m.LastAt.Equal(at(time.Minute)) || m.P95RTT == nil || *m.P95RTT < 10 || *m.P95RTT > 30 {
				t.Fatalf("paris summary: %+v", m)
			}
		}
		if m.EdgeID == lyon.ID && (m.LastOK || m.LastError != "no route" || m.P95RTT != nil) {
			t.Fatalf("lyon summary: %+v", m)
		}
	}
	if !found {
		t.Fatalf("no summary for paris: %+v", sums)
	}
}

func TestRetention(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()

	if d, err := s.Retention(ctx); err != nil || d != 0 {
		t.Fatalf("default retention: %v, %v", d, err)
	}
	if err := s.SetRetention(ctx, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if d, err := s.Retention(ctx); err != nil || d != 30*24*time.Hour {
		t.Fatalf("retention: %v, %v", d, err)
	}
	// Changing it replaces the policy rather than stacking a second one.
	if err := s.SetRetention(ctx, 90*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Retention(ctx); d != 90*24*time.Hour {
		t.Fatalf("changed retention: %v", d)
	}
	if err := s.SetRetention(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Retention(ctx); d != 0 {
		t.Fatalf("kept for ever: %v", d)
	}
	if err := s.SetRetention(ctx, -time.Hour); err == nil {
		t.Fatal("a negative retention was accepted")
	}
}
