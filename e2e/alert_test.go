package e2e

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central"
	"github.com/Arylite/netprobe/internal/central/alert"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
	"github.com/Arylite/netprobe/internal/edge"
	"github.com/Arylite/netprobe/internal/probe"
)

type delivery struct {
	header  http.Header
	payload alert.Payload
	body    []byte
}

// TestAServiceThatGoesDownAndComesBackIsToldToTheWebhook follows an outage
// from end to end: a real edge measures a real listener, the listener goes
// away, the central opens an incident and posts it, signed, to a webhook, and
// posts the recovery when the listener is back.
func TestAServiceThatGoesDownAndComesBackIsToldToTheWebhook(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	defer func() { _ = ln.Close() }()

	st := storetest.Open(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e, token, err := st.AddEdge(ctx, "paris")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddCheck(ctx, api.Check{ID: "service", Kind: api.KindTCP, Target: addr, IntervalSeconds: 1}); err != nil {
		t.Fatal(err)
	}

	received := make(chan delivery, 10)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p alert.Payload
		_ = json.Unmarshal(body, &p)
		received <- delivery{r.Header.Clone(), p, body}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hook.Close()
	if _, err := st.AddChannel(ctx, "ops", hook.URL+"/in", "s3cret"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // the incident must start after the channel

	edgeServer := httptest.NewServer(central.New(quiet, st).Handler())
	defer edgeServer.Close()
	client, err := edge.NewClient(edgeServer.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	agent := &edge.Agent{
		Client:         client,
		Measure:        edge.ProbeMeasurer(probe.New(probe.Policy{}, 500*time.Millisecond)),
		Interval:       50 * time.Millisecond,
		ReportInterval: 50 * time.Millisecond,
		Log:            quiet,
	}
	engine, err := alert.New(quiet, st, alert.Config{Failures: 3, Silence: time.Hour, Interval: 100 * time.Millisecond, Policy: &probe.Policy{}})
	if err != nil {
		t.Fatal(err)
	}
	var running [2]chan struct{}
	for i, run := range []func(context.Context){agent.Run, engine.Run} {
		running[i] = make(chan struct{})
		go func() { run(ctx); close(running[i]) }()
	}
	defer func() {
		cancel()
		<-running[0]
		<-running[1]
	}()

	// It works: nothing is said while it does.
	waitFor(t, "the first successful result", func() bool {
		rs, _ := st.RecentResults(ctx, "service", 5)
		return len(rs) > 0 && rs[0].OK
	})
	select {
	case d := <-received:
		t.Fatalf("a notification while all is well: %s", d.body)
	case <-time.After(300 * time.Millisecond):
	}

	// It goes down.
	_ = ln.Close()
	opened := next(t, received, "the incident to open")
	if opened.payload.Event != store.EventOpened || opened.payload.Incident == nil || opened.payload.Incident.CheckID != "service" || opened.payload.Incident.Edge != "paris" || opened.payload.Incident.EdgeID != e.ID {
		t.Fatalf("opened: %s", opened.body)
	}
	if opened.header.Get("X-Netprobe-Event") != "opened" {
		t.Fatalf("headers: %v", opened.header)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(opened.header.Get("X-Netprobe-Timestamp") + "."))
	mac.Write(opened.body)
	if got, want := opened.header.Get("X-Netprobe-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Fatalf("signature %q, want %q", got, want)
	}

	// It comes back.
	ln, err = net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("the port was taken in the meantime: %v", err)
	}
	resolved := next(t, received, "the incident to resolve")
	if resolved.payload.Event != store.EventResolved || resolved.payload.Incident == nil || resolved.payload.Incident.Resolution != store.ResolutionRecovered || resolved.payload.Incident.ID != opened.payload.Incident.ID {
		t.Fatalf("resolved: %s", resolved.body)
	}
	select {
	case d := <-received:
		t.Fatalf("a notification more than expected: %s", d.body)
	case <-time.After(500 * time.Millisecond):
	}
	if open, _ := st.OpenIncidents(ctx); len(open) != 0 {
		t.Fatalf("still open: %+v", open)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func next(t *testing.T, ch <-chan delivery, what string) delivery {
	t.Helper()
	select {
	case d := <-ch:
		return d
	case <-time.After(20 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return delivery{}
	}
}
