package e2e

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central"
	"github.com/Arylite/netprobe/internal/central/registry"
	"github.com/Arylite/netprobe/internal/edge"
	"github.com/Arylite/netprobe/internal/probe"
)

var checks = []api.Check{
	{ID: "web", Kind: api.KindHTTP, Target: "https://example.com", IntervalSeconds: 30},
	{ID: "ssh", Kind: api.KindTCP, Target: "example.com:22", IntervalSeconds: 10},
}

func start(t *testing.T) (*central.Server, *edge.Client) {
	t.Helper()
	srv, _, client := startWithRegistry(t)
	return srv, client
}

func startWithRegistry(t *testing.T) (*central.Server, *registry.Registry, *edge.Client) {
	t.Helper()
	reg, err := registry.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := reg.Add("paris")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := central.New(slog.New(slog.NewTextHandler(io.Discard, nil)), checks, reg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	client, err := edge.NewClient(ts.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	return srv, reg, client
}

func TestEdgeReceivesTheAssignedChecks(t *testing.T) {
	_, client := start(t)
	got, changed, err := client.Assignments(context.Background())
	if err != nil || !changed {
		t.Fatalf("first poll: changed=%v err=%v", changed, err)
	}
	if len(got) != len(checks) || got[0] != checks[0] || got[1] != checks[1] {
		t.Fatalf("got %+v", got)
	}
	if _, changed, err := client.Assignments(context.Background()); err != nil || changed {
		t.Fatalf("second poll: changed=%v err=%v", changed, err)
	}
}

func TestCentralStoresWhatTheEdgeReports(t *testing.T) {
	srv, client := start(t)
	sent := []api.Result{
		{CheckID: "web", At: time.Now().UTC(), OK: true, RTTMillis: 42.5},
		{CheckID: "ssh", At: time.Now().UTC(), OK: false, Error: "connection refused"},
	}
	if err := client.PostResults(context.Background(), sent); err != nil {
		t.Fatal(err)
	}
	got := srv.Results()
	if len(got) != 2 || got[0].EdgeID == "" || got[0].CheckID != "web" || got[0].RTTMillis != 42.5 || got[1].Error != "connection refused" {
		t.Fatalf("stored %+v", got)
	}
}

func TestAgentMeasuresAndTheCentralStoresTheResults(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	reg, err := registry.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e, token, err := reg.Add("paris")
	if err != nil {
		t.Fatal(err)
	}
	assigned := []api.Check{{ID: "local", Kind: api.KindTCP, Target: ln.Addr().String(), IntervalSeconds: 1}}
	srv, err := central.New(slog.New(slog.NewTextHandler(io.Discard, nil)), assigned, reg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	client, err := edge.NewClient(ts.URL, token)
	if err != nil {
		t.Fatal(err)
	}

	agent := &edge.Agent{
		Client:         client,
		Measure:        edge.ProbeMeasurer(probe.New(probe.Policy{}, time.Second)),
		Interval:       50 * time.Millisecond,
		ReportInterval: 50 * time.Millisecond,
		Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { agent.Run(ctx); close(done) }()

	deadline := time.Now().Add(10 * time.Second)
	for len(srv.Results()) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the central did not receive the measurements")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the agent did not stop")
	}

	got := srv.Results()[0]
	if got.EdgeID != e.ID || got.CheckID != "local" || !got.OK || got.RTTMillis < 0 {
		t.Fatalf("stored %+v", got)
	}
}

func TestAnEdgeWithoutAValidTokenIsRefused(t *testing.T) {
	reg, err := registry.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv, err := central.New(slog.New(slog.NewTextHandler(io.Discard, nil)), checks, reg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	stranger, err := edge.NewClient(ts.URL, "np_not-issued-by-this-central")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := stranger.Assignments(context.Background()); !errors.Is(err, edge.ErrUnauthorized) {
		t.Fatalf("assignments: %v", err)
	}
	if err := stranger.PostResults(context.Background(), nil); !errors.Is(err, edge.ErrUnauthorized) {
		t.Fatalf("results: %v", err)
	}
}

func TestRevokingAnEdgeCutsItOff(t *testing.T) {
	_, reg, client := startWithRegistry(t)
	if _, _, err := client.Assignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reg.Revoke("paris"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Assignments(context.Background()); !errors.Is(err, edge.ErrUnauthorized) {
		t.Fatalf("after revoke: %v", err)
	}
}
