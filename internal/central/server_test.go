package central

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/registry"
)

var (
	testChecks = []api.Check{{ID: "c1", Kind: api.KindTCP, Target: "example.com:443", IntervalSeconds: 10}}
	quiet      = slog.New(slog.NewTextHandler(io.Discard, nil))
)

type fixture struct {
	srv   *Server
	reg   *registry.Registry
	url   string
	edge  registry.Edge
	token string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	reg, err := registry.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	edge, token, err := reg.Add("paris")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(quiet, testChecks, reg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &fixture{srv: srv, reg: reg, url: ts.URL, edge: edge, token: token}
}

func (f *fixture) do(t *testing.T, method, path, body string, header map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, f.url+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func (f *fixture) bearer() map[string]string {
	return map[string]string{"Authorization": "Bearer " + f.token}
}

func TestNewRejectsBadChecks(t *testing.T) {
	reg, _ := registry.Open(t.TempDir())
	dup := append(append([]api.Check(nil), testChecks...), testChecks...)
	if _, err := New(quiet, dup, reg); err == nil {
		t.Fatal("accepted a duplicated check id")
	}
	if _, err := New(quiet, []api.Check{{ID: "x"}}, reg); err == nil {
		t.Fatal("accepted an invalid check")
	}
}

func TestHealthNeedsNoToken(t *testing.T) {
	f := newFixture(t)
	if res := f.do(t, http.MethodGet, api.PathHealth, "", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestEdgeAPIRefusesMissingAndWrongTokens(t *testing.T) {
	f := newFixture(t)
	headers := map[string]map[string]string{
		"no header":    nil,
		"wrong token":  {"Authorization": "Bearer np_wrong"},
		"wrong scheme": {"Authorization": "Basic " + f.token},
		"empty bearer": {"Authorization": "Bearer "},
	}
	for name, h := range headers {
		for _, call := range []struct{ method, path string }{{http.MethodGet, api.PathAssignments}, {http.MethodPost, api.PathResults}} {
			res := f.do(t, call.method, call.path, `{"results":[]}`, h)
			if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("%s %s %s: status %d", name, call.method, call.path, res.StatusCode)
			}
		}
	}
	if n := len(f.srv.Results()); n != 0 {
		t.Fatalf("%d results stored without a token", n)
	}
}

func TestRevokedTokenIsRefused(t *testing.T) {
	f := newFixture(t)
	if err := f.reg.Revoke("paris"); err != nil {
		t.Fatal(err)
	}
	if res := f.do(t, http.MethodGet, api.PathAssignments, "", f.bearer()); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestAssignmentsAndETag(t *testing.T) {
	f := newFixture(t)
	res := f.do(t, http.MethodGet, api.PathAssignments, "", f.bearer())
	var got api.Assignments
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || len(got.Checks) != 1 || got.Checks[0] != testChecks[0] {
		t.Fatalf("assignments %+v, %v", got, err)
	}
	etag := res.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	h := f.bearer()
	h["If-None-Match"] = etag
	if again := f.do(t, http.MethodGet, api.PathAssignments, "", h); again.StatusCode != http.StatusNotModified {
		t.Fatalf("status %d, want 304", again.StatusCode)
	}
}

func TestEmptyAssignmentsAreAnArray(t *testing.T) {
	reg, _ := registry.Open(t.TempDir())
	_, token, _ := reg.Add("paris")
	s, err := New(quiet, nil, reg)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, api.PathAssignments, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"checks":[]`) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestPostResultsAreTaggedWithTheEdge(t *testing.T) {
	f := newFixture(t)
	body := `{"results":[{"check_id":"c1","at":"` + time.Now().UTC().Format(time.RFC3339) + `","ok":true,"rtt_millis":3.5}]}`
	if res := f.do(t, http.MethodPost, api.PathResults, body, f.bearer()); res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}
	got := f.srv.Results()
	if len(got) != 1 || got[0].CheckID != "c1" || got[0].EdgeID != f.edge.ID {
		t.Fatalf("stored %+v", got)
	}
}

func TestPostResultsRefusals(t *testing.T) {
	f := newFixture(t)
	tests := map[string]string{
		"not json":      `nope`,
		"unknown field": `{"results":[],"extra":1}`,
		"invalid":       `{"results":[{"check_id":"","at":"2026-01-01T00:00:00Z"}]}`,
		"too large":     `{"results":[{"check_id":"c1","at":"2026-01-01T00:00:00Z","error":"` + strings.Repeat("x", maxBodyBytes) + `"}]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if res := f.do(t, http.MethodPost, api.PathResults, body, f.bearer()); res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", res.StatusCode)
			}
		})
	}
}

func TestStoredResultsAreBounded(t *testing.T) {
	f := newFixture(t)
	batch := make([]api.Result, api.MaxResultsPerBatch)
	for i := range batch {
		batch[i] = api.Result{CheckID: "c1", At: time.Now()}
	}
	body, _ := json.Marshal(api.ResultsRequest{Results: batch})
	for i := 0; i < maxStoredResults/api.MaxResultsPerBatch+2; i++ {
		if res := f.do(t, http.MethodPost, api.PathResults, string(body), f.bearer()); res.StatusCode != http.StatusNoContent {
			t.Fatalf("status %d", res.StatusCode)
		}
	}
	if n := len(f.srv.Results()); n != maxStoredResults {
		t.Fatalf("stored %d, want %d", n, maxStoredResults)
	}
}
