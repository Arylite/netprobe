package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central"
	"github.com/Arylite/netprobe/internal/central/auth"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
	"github.com/Arylite/netprobe/internal/central/webapi"
	"github.com/Arylite/netprobe/internal/edge"
	"github.com/Arylite/netprobe/internal/probe"
)

// call makes a request to the UI API and decodes the JSON answer into out.
func call(t *testing.T, method, url, token string, body, out any) int {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: %v", method, url, err)
		}
	}
	return res.StatusCode
}

// TestAdministratorSetsUpAndViewerReadsThroughTheUIAPI follows the whole life
// of the product across its two APIs: an administrator registers an edge and a
// check from the UI API, the edge measures and reports through the edge API,
// and a viewer reads the results back from the UI API.
func TestAdministratorSetsUpAndViewerReadsThroughTheUIAPI(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	st := storetest.Open(t)
	ctx := context.Background()
	for _, u := range []struct{ name, role, password string }{
		{"alice", store.RoleAdmin, "an administrator password"},
		{"bob", store.RoleViewer, "a viewer password here"},
	} {
		hash, err := auth.HashPassword(u.password)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddUser(ctx, u.name, u.role, hash); err != nil {
			t.Fatal(err)
		}
	}

	ui, err := webapi.New(quiet, st, webapi.Config{})
	if err != nil {
		t.Fatal(err)
	}
	uiServer := httptest.NewServer(ui.Handler())
	defer uiServer.Close()
	edgeServer := httptest.NewServer(central.New(quiet, st).Handler())
	defer edgeServer.Close()

	signIn := func(user, password string) string {
		var session struct{ Token string }
		if code := call(t, http.MethodPost, uiServer.URL+"/api/v1/login", "", map[string]string{"username": user, "password": password}, &session); code != http.StatusOK {
			t.Fatalf("login %s: %d", user, code)
		}
		return session.Token
	}
	admin := signIn("alice", "an administrator password")
	viewer := signIn("bob", "a viewer password here")

	// The administrator registers an edge and a check.
	var created struct{ Token string }
	if code := call(t, http.MethodPost, uiServer.URL+"/api/v1/edges", admin, map[string]string{"name": "paris"}, &created); code != http.StatusCreated {
		t.Fatalf("create edge: %d", code)
	}
	check := api.Check{ID: "local", Kind: api.KindTCP, Target: ln.Addr().String(), IntervalSeconds: 1}
	if code := call(t, http.MethodPost, uiServer.URL+"/api/v1/checks", admin, check, nil); code != http.StatusCreated {
		t.Fatalf("create check: %d", code)
	}

	// The edge, with the token it was given, measures and reports.
	client, err := edge.NewClient(edgeServer.URL, created.Token)
	if err != nil {
		t.Fatal(err)
	}
	agent := &edge.Agent{
		Client:         client,
		Measure:        edge.ProbeMeasurer(probe.New(probe.Policy{}, time.Second)),
		Interval:       50 * time.Millisecond,
		ReportInterval: 50 * time.Millisecond,
		Log:            quiet,
	}
	agentCtx, stopAgent := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { agent.Run(agentCtx); close(done) }()
	defer func() { stopAgent(); <-done }()

	// The viewer reads the results back.
	var results struct {
		Results []struct {
			EdgeID string `json:"edge_id"`
			OK     bool   `json:"ok"`
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for len(results.Results) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the viewer never saw the results")
		}
		time.Sleep(100 * time.Millisecond)
		results.Results = nil
		call(t, http.MethodGet, uiServer.URL+"/api/v1/checks/local/results", viewer, nil, &results)
	}
	if !results.Results[0].OK || results.Results[0].EdgeID == "" {
		t.Fatalf("results %+v", results.Results)
	}

	// The edge shows as seen, then the administrator cuts it off.
	var edges struct {
		Edges []struct {
			Name     string     `json:"name"`
			LastSeen *time.Time `json:"last_seen"`
		}
	}
	call(t, http.MethodGet, uiServer.URL+"/api/v1/edges", viewer, nil, &edges)
	if len(edges.Edges) != 1 || edges.Edges[0].LastSeen == nil {
		t.Fatalf("edges %+v", edges.Edges)
	}
	if code := call(t, http.MethodDelete, uiServer.URL+"/api/v1/edges/paris", admin, nil, nil); code != http.StatusNoContent {
		t.Fatalf("revoke: %d", code)
	}
	if _, _, err := client.Assignments(ctx); err == nil {
		t.Fatal("a revoked edge could still poll")
	}

	// A viewer may not change anything.
	if code := call(t, http.MethodDelete, uiServer.URL+"/api/v1/checks/local", viewer, nil, nil); code != http.StatusForbidden {
		t.Fatalf("a viewer removed a check: %d", code)
	}
}
