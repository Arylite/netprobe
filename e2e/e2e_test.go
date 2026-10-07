package e2e

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
	"github.com/Arylite/netprobe/internal/edge"
	"github.com/Arylite/netprobe/internal/probe"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type stack struct {
	store *store.Store
	url   string
	edge  store.Edge
	token string
}

func start(t *testing.T, checks ...api.Check) *stack {
	t.Helper()
	st := storetest.Open(t)
	ctx := context.Background()
	e, token, err := st.AddEdge(ctx, "paris")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if err := st.AddCheck(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(central.New(quiet, st).Handler())
	t.Cleanup(ts.Close)
	return &stack{store: st, url: ts.URL, edge: e, token: token}
}

func (s *stack) client(t *testing.T, token string) *edge.Client {
	t.Helper()
	c, err := edge.NewClient(s.url, token)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEdgeReceivesTheStoredChecks(t *testing.T) {
	web := api.Check{ID: "web", Kind: api.KindHTTP, Target: "https://example.com", IntervalSeconds: 30}
	ssh := api.Check{ID: "ssh", Kind: api.KindTCP, Target: "example.com:22", IntervalSeconds: 10}
	s := start(t, web, ssh)
	client := s.client(t, s.token)

	got, changed, err := client.Assignments(context.Background())
	if err != nil || !changed || len(got) != 2 || got[0] != ssh || got[1] != web {
		t.Fatalf("first poll: %+v changed=%v err=%v", got, changed, err)
	}
	if _, changed, err := client.Assignments(context.Background()); err != nil || changed {
		t.Fatalf("second poll: changed=%v err=%v", changed, err)
	}
	if err := s.store.RemoveCheck(context.Background(), "ssh"); err != nil {
		t.Fatal(err)
	}
	if got, changed, err := client.Assignments(context.Background()); err != nil || !changed || len(got) != 1 {
		t.Fatalf("after removing a check: %+v changed=%v err=%v", got, changed, err)
	}
}

func TestResultsReportedByTheEdgeAreStored(t *testing.T) {
	s := start(t)
	sent := []api.Result{
		{CheckID: "web", At: time.Now().UTC(), OK: true, RTTMillis: 42.5},
		{CheckID: "web", At: time.Now().UTC().Add(time.Second), Error: "connection refused"},
	}
	if err := s.client(t, s.token).PostResults(context.Background(), sent); err != nil {
		t.Fatal(err)
	}
	got, err := s.store.RecentResults(context.Background(), "web", 10)
	if err != nil || len(got) != 2 || got[0].EdgeID != s.edge.ID || got[0].Error != "connection refused" || got[1].RTTMillis != 42.5 {
		t.Fatalf("stored %+v, %v", got, err)
	}
}

func TestAgentMeasuresAndTheCentralStoresTheResults(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	s := start(t, api.Check{ID: "local", Kind: api.KindTCP, Target: ln.Addr().String(), IntervalSeconds: 1})

	agent := &edge.Agent{
		Client:         s.client(t, s.token),
		Measure:        edge.ProbeMeasurer(probe.New(probe.Policy{}, time.Second)),
		Interval:       50 * time.Millisecond,
		ReportInterval: 50 * time.Millisecond,
		Log:            quiet,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { agent.Run(ctx); close(done) }()

	var got []store.Result
	deadline := time.Now().Add(10 * time.Second)
	for len(got) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the central did not receive the measurements")
		}
		time.Sleep(50 * time.Millisecond)
		if got, err = s.store.RecentResults(context.Background(), "local", 10); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the agent did not stop")
	}
	if first := got[0]; first.EdgeID != s.edge.ID || !first.OK || first.RTTMillis < 0 {
		t.Fatalf("stored %+v", first)
	}
}

func TestChecksWithAnExpectationTravelToTheEdgeAndBack(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("SSH-2.0-test\r\n"))
			c.Close()
		}
	}()
	addr := ln.Addr().String()
	s := start(t,
		api.Check{ID: "greets", Kind: api.KindBanner, Target: addr, Expect: "SSH-2.0", IntervalSeconds: 1},
		api.Check{ID: "wrong", Kind: api.KindBanner, Target: addr, Expect: "220", IntervalSeconds: 1},
		api.Check{ID: "shut", Kind: api.KindClosed, Target: addr, IntervalSeconds: 1},
	)
	agent := &edge.Agent{
		Client:         s.client(t, s.token),
		Measure:        edge.ProbeMeasurer(probe.New(probe.Policy{}, time.Second)),
		Interval:       50 * time.Millisecond,
		ReportInterval: 50 * time.Millisecond,
		Log:            quiet,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agent.Run(ctx)

	want := map[string]struct {
		ok  bool
		err string
	}{"greets": {true, ""}, "wrong": {false, "does not hold"}, "shut": {false, "the port is open"}}
	deadline := time.Now().Add(10 * time.Second)
	for id, w := range want {
		var got []store.Result
		for len(got) == 0 {
			if time.Now().After(deadline) {
				t.Fatalf("no result for %s", id)
			}
			time.Sleep(50 * time.Millisecond)
			if got, err = s.store.RecentResults(context.Background(), id, 1); err != nil {
				t.Fatal(err)
			}
		}
		if got[0].OK != w.ok || !strings.Contains(got[0].Error, w.err) {
			t.Errorf("%s: %+v", id, got[0])
		}
	}
}

func TestAnEdgeWithoutAValidTokenIsRefused(t *testing.T) {
	s := start(t)
	stranger := s.client(t, "np_not-issued-by-this-central")
	if _, _, err := stranger.Assignments(context.Background()); !errors.Is(err, edge.ErrUnauthorized) {
		t.Fatalf("assignments: %v", err)
	}
	if err := stranger.PostResults(context.Background(), nil); !errors.Is(err, edge.ErrUnauthorized) {
		t.Fatalf("results: %v", err)
	}
}

func TestRevokingAnEdgeCutsItOff(t *testing.T) {
	s := start(t)
	client := s.client(t, s.token)
	if _, _, err := client.Assignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.store.RevokeEdge(context.Background(), "paris"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Assignments(context.Background()); !errors.Is(err, edge.ErrUnauthorized) {
		t.Fatalf("after revoke: %v", err)
	}
}
