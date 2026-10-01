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
)

var testChecks = []api.Check{{ID: "c1", Kind: api.KindTCP, Target: "example.com:443", IntervalSeconds: 10}}

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)), testChecks)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestNewRejectsBadChecks(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dup := append(append([]api.Check(nil), testChecks...), testChecks...)
	if _, err := New(log, dup); err == nil {
		t.Fatal("accepted a duplicated check id")
	}
	if _, err := New(log, []api.Check{{ID: "x"}}); err == nil {
		t.Fatal("accepted an invalid check")
	}
}

func TestHealth(t *testing.T) {
	_, ts := newTestServer(t)
	res, err := http.Get(ts.URL + api.PathHealth)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestAssignmentsAndETag(t *testing.T) {
	_, ts := newTestServer(t)
	res, err := http.Get(ts.URL + api.PathAssignments)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got api.Assignments
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || len(got.Checks) != 1 || got.Checks[0] != testChecks[0] {
		t.Fatalf("assignments %+v, %v", got, err)
	}
	etag := res.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+api.PathAssignments, nil)
	req.Header.Set("If-None-Match", etag)
	again, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Body.Close()
	if again.StatusCode != http.StatusNotModified {
		t.Fatalf("status %d, want 304", again.StatusCode)
	}
}

func TestEmptyAssignmentsAreAnArray(t *testing.T) {
	s, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, api.PathAssignments, nil))
	if !strings.Contains(rec.Body.String(), `"checks":[]`) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestPostResults(t *testing.T) {
	s, ts := newTestServer(t)
	body := `{"results":[{"check_id":"c1","at":"` + time.Now().UTC().Format(time.RFC3339) + `","ok":true,"rtt_millis":3.5}]}`
	res, err := http.Post(ts.URL+api.PathResults, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}
	if got := s.Results(); len(got) != 1 || got[0].CheckID != "c1" {
		t.Fatalf("stored %+v", got)
	}
}

func TestPostResultsRefusals(t *testing.T) {
	_, ts := newTestServer(t)
	tests := map[string]string{
		"not json":      `nope`,
		"unknown field": `{"results":[],"extra":1}`,
		"invalid":       `{"results":[{"check_id":"","at":"2026-01-01T00:00:00Z"}]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			res, err := http.Post(ts.URL+api.PathResults, "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", res.StatusCode)
			}
		})
	}
}

func TestPostResultsBodyLimit(t *testing.T) {
	_, ts := newTestServer(t)
	huge := `{"results":[{"check_id":"c1","at":"2026-01-01T00:00:00Z","error":"` + strings.Repeat("x", maxBodyBytes) + `"}]}`
	res, err := http.Post(ts.URL+api.PathResults, "application/json", strings.NewReader(huge))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", res.StatusCode)
	}
}

func TestStoredResultsAreBounded(t *testing.T) {
	s, _ := newTestServer(t)
	batch := make([]api.Result, api.MaxResultsPerBatch)
	for i := range batch {
		batch[i] = api.Result{CheckID: "c1", At: time.Now()}
	}
	body, _ := json.Marshal(api.ResultsRequest{Results: batch})
	for i := 0; i < maxStoredResults/api.MaxResultsPerBatch+2; i++ {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, api.PathResults, strings.NewReader(string(body))))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d", rec.Code)
		}
	}
	if n := len(s.Results()); n != maxStoredResults {
		t.Fatalf("stored %d, want %d", n, maxStoredResults)
	}
}
