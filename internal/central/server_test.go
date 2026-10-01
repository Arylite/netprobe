package central

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

var (
	testCheck = api.Check{ID: "c1", Kind: api.KindTCP, Target: "example.com:443", IntervalSeconds: 10}
	quiet     = slog.New(slog.NewTextHandler(io.Discard, nil))
)

type fixture struct {
	store *store.Store
	url   string
	edge  store.Edge
	token string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st := storetest.Open(t)
	ctx := context.Background()
	edge, token, err := st.AddEdge(ctx, "paris")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddCheck(ctx, testCheck); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(quiet, st).Handler())
	t.Cleanup(ts.Close)
	return &fixture{store: st, url: ts.URL, edge: edge, token: token}
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

func (f *fixture) stored(t *testing.T) []store.Result {
	t.Helper()
	got, err := f.store.RecentResults(context.Background(), "c1", 100)
	if err != nil {
		t.Fatal(err)
	}
	return got
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
	if n := len(f.stored(t)); n != 0 {
		t.Fatalf("%d results stored without a token", n)
	}
}

func TestRevokedTokenIsRefusedImmediately(t *testing.T) {
	f := newFixture(t)
	if res := f.do(t, http.MethodGet, api.PathAssignments, "", f.bearer()); res.StatusCode != http.StatusOK {
		t.Fatalf("before revoke: %d", res.StatusCode)
	}
	if err := f.store.RevokeEdge(context.Background(), "paris"); err != nil {
		t.Fatal(err)
	}
	if res := f.do(t, http.MethodGet, api.PathAssignments, "", f.bearer()); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after revoke: %d", res.StatusCode)
	}
}

func TestAssignmentsComeFromTheStoreWithAnETag(t *testing.T) {
	f := newFixture(t)
	res := f.do(t, http.MethodGet, api.PathAssignments, "", f.bearer())
	var got api.Assignments
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || len(got.Checks) != 1 || got.Checks[0] != testCheck {
		t.Fatalf("assignments %+v, %v", got, err)
	}
	etag := res.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	h := f.bearer()
	h["If-None-Match"] = etag
	if again := f.do(t, http.MethodGet, api.PathAssignments, "", h); again.StatusCode != http.StatusNotModified {
		t.Fatalf("unchanged: status %d, want 304", again.StatusCode)
	}

	next := api.Check{ID: "c2", Kind: api.KindHTTP, Target: "https://example.com", IntervalSeconds: 30}
	if err := f.store.AddCheck(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	changed := f.do(t, http.MethodGet, api.PathAssignments, "", h)
	if changed.StatusCode != http.StatusOK || changed.Header.Get("ETag") == etag {
		t.Fatalf("after adding a check: status %d, etag %q", changed.StatusCode, changed.Header.Get("ETag"))
	}
}

func TestNoChecksIsAnEmptyArray(t *testing.T) {
	f := newFixture(t)
	if err := f.store.RemoveCheck(context.Background(), "c1"); err != nil {
		t.Fatal(err)
	}
	res := f.do(t, http.MethodGet, api.PathAssignments, "", f.bearer())
	raw, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(raw), `"checks":[]`) {
		t.Fatalf("body %s", raw)
	}
}

func TestPostResultsAreStoredWithTheEdge(t *testing.T) {
	f := newFixture(t)
	body := `{"results":[{"check_id":"c1","at":"` + time.Now().UTC().Format(time.RFC3339) + `","ok":true,"rtt_millis":3.5}]}`
	if res := f.do(t, http.MethodPost, api.PathResults, body, f.bearer()); res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}
	got := f.stored(t)
	if len(got) != 1 || got[0].CheckID != "c1" || got[0].EdgeID != f.edge.ID || got[0].RTTMillis != 3.5 {
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
	if n := len(f.stored(t)); n != 0 {
		t.Fatalf("%d refused results were stored", n)
	}
}

func TestADatabaseFailureIsA503NotA401(t *testing.T) {
	f := newFixture(t)
	f.store.Close()
	for _, call := range []struct{ method, path string }{{http.MethodGet, api.PathAssignments}, {http.MethodPost, api.PathResults}, {http.MethodGet, api.PathHealth}} {
		res := f.do(t, call.method, call.path, `{"results":[]}`, f.bearer())
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status %d, want 503", call.method, call.path, res.StatusCode)
		}
	}
}
