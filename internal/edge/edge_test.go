package edge

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

const stubETag = `"v1"`

func stubCentral(t *testing.T, polls *atomic.Int32, posted *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+api.PathAssignments, func(w http.ResponseWriter, r *http.Request) {
		polls.Add(1)
		if r.Header.Get("If-None-Match") == stubETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", stubETag)
		_ = json.NewEncoder(w).Encode(api.Assignments{Checks: []api.Check{{ID: "c1", Kind: api.KindTCP, Target: "example.com:443", IntervalSeconds: 5}}})
	})
	mux.HandleFunc("POST "+api.PathResults, func(w http.ResponseWriter, _ *http.Request) {
		posted.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestNewClientRejectsBadURLs(t *testing.T) {
	for _, u := range []string{"", "central", "ftp://central", "http://"} {
		if _, err := NewClient(u); err == nil {
			t.Errorf("NewClient(%q) succeeded", u)
		}
	}
}

func TestAssignmentsUseTheETag(t *testing.T) {
	var polls, posted atomic.Int32
	ts := stubCentral(t, &polls, &posted)
	c, err := NewClient(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	checks, changed, err := c.Assignments(context.Background())
	if err != nil || !changed || len(checks) != 1 {
		t.Fatalf("first poll: %v %v %v", checks, changed, err)
	}
	_, changed, err = c.Assignments(context.Background())
	if err != nil || changed {
		t.Fatalf("second poll: changed=%v err=%v", changed, err)
	}
}

func TestAssignmentsRefuseInvalidChecks(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"checks":[{"id":"","kind":"tcp"}]}`)
	}))
	defer ts.Close()
	c, _ := NewClient(ts.URL)
	if _, _, err := c.Assignments(context.Background()); err == nil {
		t.Fatal("accepted an invalid check")
	}
}

func TestPostResultsErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusBadRequest)
	}))
	defer ts.Close()
	c, _ := NewClient(ts.URL)
	if err := c.PostResults(context.Background(), []api.Result{{CheckID: "c1", At: time.Now()}}); err == nil {
		t.Fatal("a 400 was not reported")
	}
	if err := c.PostResults(context.Background(), []api.Result{{}}); err == nil {
		t.Fatal("an invalid result was sent")
	}
}

func TestAgentPollsUntilCancelled(t *testing.T) {
	var polls, posted atomic.Int32
	ts := stubCentral(t, &polls, &posted)
	c, _ := NewClient(ts.URL)
	a := &Agent{Client: c, Interval: 10 * time.Millisecond, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()

	deadline := time.After(5 * time.Second)
	for polls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatal("the agent did not poll repeatedly")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
