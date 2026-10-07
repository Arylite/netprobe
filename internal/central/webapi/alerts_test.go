package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/alert"
	"github.com/Arylite/netprobe/internal/central/store"
)

// fakeSender records what the API sends, and fails when told to.
type fakeSender struct {
	mu   sync.Mutex
	sent []alert.Payload
	urls []string
	err  error
}

func (s *fakeSender) Send(_ context.Context, ch store.Channel, p alert.Payload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, p)
	s.urls = append(s.urls, ch.URL)
	return s.err
}

func TestEveryAlertRouteNeedsASession(t *testing.T) {
	f := newFixture(t, Config{})
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/status"},
		{http.MethodGet, "/api/v1/incidents"},
		{http.MethodGet, "/api/v1/channels"},
		{http.MethodPost, "/api/v1/channels"},
		{http.MethodDelete, "/api/v1/channels/ops"},
		{http.MethodPost, "/api/v1/channels/ops/test"},
	} {
		if res, _ := f.do(t, call.method, call.path, "", nil, nil); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", call.method, call.path, res.StatusCode)
		}
	}
}

func TestViewersReadTheDashboardButDoNotTouchChannels(t *testing.T) {
	f := newFixture(t, Config{})
	viewer := f.login(t, "bob", viewerPassword)
	for _, path := range []string{"/api/v1/status", "/api/v1/incidents"} {
		if res, raw := f.do(t, http.MethodGet, path, viewer, nil, nil); res.StatusCode != http.StatusOK {
			t.Errorf("GET %s as a viewer: %d %s", path, res.StatusCode, raw)
		}
	}
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/channels", nil},
		{http.MethodPost, "/api/v1/channels", createChannelRequest{Name: "ops", URL: "https://example.com/h"}},
		{http.MethodDelete, "/api/v1/channels/ops", nil},
		{http.MethodPost, "/api/v1/channels/ops/test", nil},
	} {
		if res, raw := f.do(t, call.method, call.path, viewer, call.body, nil); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a viewer: %d %s, want 403", call.method, call.path, res.StatusCode, raw)
		}
	}
}

func TestStatusSummarisesEachCheckOnEachActiveEdge(t *testing.T) {
	f := newFixture(t, Config{})
	ctx := context.Background()
	viewer := f.login(t, "bob", viewerPassword)

	paris, _, _ := f.store.AddEdge(ctx, "paris")
	gone, _, _ := f.store.AddEdge(ctx, "gone")
	if err := f.store.RevokeEdge(ctx, "gone"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"web", "db"} {
		if err := f.store.AddCheck(ctx, api.Check{ID: id, Kind: api.KindTCP, Target: "example.com:443", IntervalSeconds: 10}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := f.store.InsertResults(ctx, paris.ID, []api.Result{
		{CheckID: "web", At: now.Add(-3 * time.Minute), OK: true, RTTMillis: 10},
		{CheckID: "web", At: now.Add(-2 * time.Minute), OK: true, RTTMillis: 20},
		{CheckID: "web", At: now.Add(-1 * time.Minute), OK: false, RTTMillis: 1, Error: "refused"},
		{CheckID: "web", At: now.Add(-30 * time.Second), OK: true, RTTMillis: 3},
		{CheckID: "web", At: now.Add(-48 * time.Hour), OK: true, RTTMillis: 500},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.InsertResults(ctx, gone.ID, []api.Result{{CheckID: "web", At: now, OK: true, RTTMillis: 1}}); err != nil {
		t.Fatal(err)
	}

	res, raw := f.do(t, http.MethodGet, "/api/v1/status", viewer, nil, nil)
	status(t, res, raw, http.StatusOK)
	var body struct {
		WindowSeconds int `json:"window_seconds"`
		Checks        []struct {
			ID    string `json:"id"`
			Edges []struct {
				Edge         string   `json:"edge"`
				EdgeID       string   `json:"edge_id"`
				OK           bool     `json:"ok"`
				Samples      int      `json:"samples"`
				SuccessRatio float64  `json:"success_ratio"`
				P95          *float64 `json:"p95_rtt_millis"`
			} `json:"edges"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.WindowSeconds != 86400 || len(body.Checks) != 2 || body.Checks[0].ID != "db" || body.Checks[1].ID != "web" {
		t.Fatalf("status: %s", raw)
	}
	if edges := body.Checks[0].Edges; edges == nil || len(edges) != 0 || !strings.Contains(string(raw), `"edges":[]`) {
		t.Fatalf("a check nobody ran must list no edge, as an empty array: %s", raw)
	}
	web := body.Checks[1].Edges
	if len(web) != 1 || web[0].Edge != "paris" || web[0].EdgeID != paris.ID || !web[0].OK || web[0].Samples != 4 || web[0].SuccessRatio != 0.75 || web[0].P95 == nil {
		t.Fatalf("web on the edges: %+v (the revoked edge and the old result must not show)", web)
	}
}

func TestIncidentsCanBeFilteredAndLimited(t *testing.T) {
	f := newFixture(t, Config{})
	ctx := context.Background()
	viewer := f.login(t, "bob", viewerPassword)
	paris, _, _ := f.store.AddEdge(ctx, "paris")
	now := time.Now().UTC()
	if _, err := f.store.OpenIncident(ctx, store.IncidentCheck, "web", paris.ID, "refused", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.OpenIncident(ctx, store.IncidentEdge, "", paris.ID, "silent", now); err != nil {
		t.Fatal(err)
	}
	open, _ := f.store.OpenIncidents(ctx)
	for _, i := range open {
		if i.Kind == store.IncidentCheck {
			if err := f.store.ResolveIncident(ctx, i.ID, now.Add(-time.Minute), store.ResolutionRecovered); err != nil {
				t.Fatal(err)
			}
		}
	}

	list := func(query string) []alert.IncidentJSON {
		t.Helper()
		res, raw := f.do(t, http.MethodGet, "/api/v1/incidents"+query, viewer, nil, nil)
		status(t, res, raw, http.StatusOK)
		var body struct {
			Incidents []alert.IncidentJSON `json:"incidents"`
		}
		if err := json.Unmarshal(raw, &body); err != nil || body.Incidents == nil {
			t.Fatalf("%s: %v", raw, err)
		}
		return body.Incidents
	}
	all := list("")
	if len(all) != 2 || all[0].Kind != store.IncidentEdge || all[1].Resolution != store.ResolutionRecovered || all[1].ResolvedAt == nil || all[1].Edge != "paris" || all[1].CheckID != "web" {
		t.Fatalf("all: %+v", all)
	}
	if got := list("?state=open"); len(got) != 1 || got[0].Kind != store.IncidentEdge || got[0].ResolvedAt != nil {
		t.Fatalf("open: %+v", got)
	}
	if got := list("?limit=1"); len(got) != 1 {
		t.Fatalf("limit: %+v", got)
	}
	for _, bad := range []string{"?state=closed", "?limit=0", "?limit=x"} {
		if res, _ := f.do(t, http.MethodGet, "/api/v1/incidents"+bad, viewer, nil, nil); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, res.StatusCode)
		}
	}
}

func TestAdministratorsManageChannels(t *testing.T) {
	sender := &fakeSender{}
	f := newFixture(t, Config{Sender: sender})
	admin := f.login(t, "alice", adminPassword)

	res, raw := f.do(t, http.MethodGet, "/api/v1/channels", admin, nil, nil)
	status(t, res, raw, http.StatusOK)
	if !strings.Contains(string(raw), `"channels":[]`) {
		t.Fatalf("empty list: %s", raw)
	}

	res, raw = f.do(t, http.MethodPost, "/api/v1/channels", admin, createChannelRequest{Name: "ops", URL: "https://hooks.example.com/T0/secret-path", Secret: "s3cret"}, nil)
	status(t, res, raw, http.StatusCreated)
	if strings.Contains(string(raw), "s3cret") || !strings.Contains(string(raw), `"has_secret":true`) {
		t.Fatalf("created: %s", raw)
	}
	res, raw = f.do(t, http.MethodGet, "/api/v1/channels", admin, nil, nil)
	status(t, res, raw, http.StatusOK)
	var list struct {
		Channels []channelJSON `json:"channels"`
	}
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Channels) != 1 || list.Channels[0].URL != "https://hooks.example.com/T0/secret-path" || !list.Channels[0].HasSecret || strings.Contains(string(raw), "s3cret") {
		t.Fatalf("list: %s (%v)", raw, err)
	}

	res, raw = f.do(t, http.MethodPost, "/api/v1/channels", admin, createChannelRequest{Name: "ops", URL: "https://other.example.com"}, nil)
	status(t, res, raw, http.StatusConflict)
	for name, body := range map[string]any{
		"bad name":       createChannelRequest{Name: "Ops!", URL: "https://example.com"},
		"bad scheme":     createChannelRequest{Name: "a", URL: "ftp://example.com"},
		"credentials":    createChannelRequest{Name: "a", URL: "https://user:pw@example.com"},
		"long secret":    createChannelRequest{Name: "a", URL: "https://example.com", Secret: strings.Repeat("x", 257)},
		"unknown fields": map[string]string{"name": "a", "url": "https://example.com", "extra": "x"},
	} {
		if res, raw := f.do(t, http.MethodPost, "/api/v1/channels", admin, body, nil); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, res.StatusCode, raw)
		}
	}

	// A test goes to the channel and says so.
	res, raw = f.do(t, http.MethodPost, "/api/v1/channels/ops/test", admin, nil, nil)
	status(t, res, raw, http.StatusNoContent)
	if len(sender.sent) != 1 || sender.sent[0].Event != alert.EventTest || sender.urls[0] != "https://hooks.example.com/T0/secret-path" {
		t.Fatalf("sent: %+v %v", sender.sent, sender.urls)
	}
	sender.err = errors.New("the channel answered 500 Internal Server Error")
	res, raw = f.do(t, http.MethodPost, "/api/v1/channels/ops/test", admin, nil, nil)
	status(t, res, raw, http.StatusBadGateway)
	if !strings.Contains(string(raw), "500 Internal Server Error") {
		t.Fatalf("the reason is missing: %s", raw)
	}
	res, raw = f.do(t, http.MethodPost, "/api/v1/channels/nope/test", admin, nil, nil)
	status(t, res, raw, http.StatusNotFound)

	res, raw = f.do(t, http.MethodDelete, "/api/v1/channels/ops", admin, nil, nil)
	status(t, res, raw, http.StatusNoContent)
	res, raw = f.do(t, http.MethodDelete, "/api/v1/channels/ops", admin, nil, nil)
	status(t, res, raw, http.StatusNotFound)
}

func TestTheAddressOfAChannelIsNotLogged(t *testing.T) {
	var logs strings.Builder
	f := newFixture(t, Config{Sender: &fakeSender{err: errors.New("refused")}})
	f.srv.log = slog.New(slog.NewTextHandler(&logs, nil))
	admin := f.login(t, "alice", adminPassword)
	f.do(t, http.MethodPost, "/api/v1/channels", admin, createChannelRequest{Name: "ops", URL: "https://hooks.example.com/very-secret-token"}, nil)
	f.do(t, http.MethodPost, "/api/v1/channels/ops/test", admin, nil, nil)
	if strings.Contains(logs.String(), "very-secret-token") || !strings.Contains(logs.String(), "channel added") {
		t.Fatalf("logs: %s", logs.String())
	}
}
