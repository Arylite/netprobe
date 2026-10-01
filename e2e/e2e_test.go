package e2e

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central"
	"github.com/Arylite/netprobe/internal/edge"
)

var checks = []api.Check{
	{ID: "web", Kind: api.KindHTTP, Target: "https://example.com", IntervalSeconds: 30},
	{ID: "ssh", Kind: api.KindTCP, Target: "example.com:22", IntervalSeconds: 10},
}

func start(t *testing.T) (*central.Server, *edge.Client) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := central.New(log, checks)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	client, err := edge.NewClient(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	return srv, client
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
	if len(got) != 2 || got[0].CheckID != "web" || got[0].RTTMillis != 42.5 || got[1].Error != "connection refused" {
		t.Fatalf("stored %+v", got)
	}
}

func TestAgentKeepsPollingTheCentral(t *testing.T) {
	_, client := start(t)
	agent := &edge.Agent{Client: client, Interval: 10 * time.Millisecond, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { agent.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the agent did not stop with its context")
	}
}
