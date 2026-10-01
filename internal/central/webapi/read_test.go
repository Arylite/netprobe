package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

func TestReadEndpointsNeedASession(t *testing.T) {
	f := newFixture(t, Config{})
	for _, path := range []string{"/api/v1/edges", "/api/v1/checks", "/api/v1/checks/web/results"} {
		if res, _ := f.do(t, http.MethodGet, path, "", nil, nil); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", path, res.StatusCode)
		}
	}
}

func TestEdgesShowWhenTheyLastReported(t *testing.T) {
	f := newFixture(t, Config{})
	ctx := context.Background()
	paris, _, _ := f.store.AddEdge(ctx, "paris")
	_, _, _ = f.store.AddEdge(ctx, "quiet")
	at := time.Now().UTC().Truncate(time.Millisecond)
	_ = f.store.InsertResults(ctx, paris.ID, []api.Result{{CheckID: "web", At: at, OK: true}})
	_ = f.store.RevokeEdge(ctx, "quiet")

	token := f.login(t, "bob", viewerPassword) // a viewer may read
	res, raw := f.do(t, http.MethodGet, "/api/v1/edges", token, nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, raw)
	}
	var body struct{ Edges []edgeJSON }
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Edges) != 2 {
		t.Fatalf("body %s: %v", raw, err)
	}
	if body.Edges[0].Name != "paris" || body.Edges[0].LastSeen == nil || !body.Edges[0].LastSeen.Equal(at) || body.Edges[0].RevokedAt != nil {
		t.Fatalf("paris: %+v", body.Edges[0])
	}
	if body.Edges[1].Name != "quiet" || body.Edges[1].LastSeen != nil || body.Edges[1].RevokedAt == nil {
		t.Fatalf("quiet: %+v", body.Edges[1])
	}
	if strings.Contains(string(raw), "token") {
		t.Fatalf("the listing leaks token data: %s", raw)
	}
}

func TestChecksAreListed(t *testing.T) {
	f := newFixture(t, Config{})
	token := f.login(t, "bob", viewerPassword)

	_, raw := f.do(t, http.MethodGet, "/api/v1/checks", token, nil, nil)
	if !strings.Contains(string(raw), `"checks":[]`) {
		t.Fatalf("no checks must be an empty array: %s", raw)
	}
	check := api.Check{ID: "web", Kind: api.KindHTTP, Target: "https://example.com", IntervalSeconds: 30}
	_ = f.store.AddCheck(context.Background(), check)
	_, raw = f.do(t, http.MethodGet, "/api/v1/checks", token, nil, nil)
	var body struct{ Checks []api.Check }
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Checks) != 1 || body.Checks[0] != check {
		t.Fatalf("body %s: %v", raw, err)
	}
}

func TestResultsNewestFirstWithALimit(t *testing.T) {
	f := newFixture(t, Config{})
	ctx := context.Background()
	edge, _, _ := f.store.AddEdge(ctx, "paris")
	base := time.Now().UTC().Truncate(time.Millisecond)
	_ = f.store.InsertResults(ctx, edge.ID, []api.Result{
		{CheckID: "web", At: base.Add(-2 * time.Minute), OK: true, RTTMillis: 10},
		{CheckID: "web", At: base.Add(-1 * time.Minute), OK: false, RTTMillis: 3, Error: "refused"},
		{CheckID: "web", At: base, OK: true, RTTMillis: 20},
	})
	token := f.login(t, "bob", viewerPassword)

	res, raw := f.do(t, http.MethodGet, "/api/v1/checks/web/results", token, nil, nil)
	var body struct{ Results []resultJSON }
	if res.StatusCode != http.StatusOK || json.Unmarshal(raw, &body) != nil || len(body.Results) != 3 {
		t.Fatalf("status %d body %s", res.StatusCode, raw)
	}
	if body.Results[0].RTTMillis != 20 || body.Results[1].Error != "refused" || body.Results[0].EdgeID != edge.ID {
		t.Fatalf("results %+v", body.Results)
	}

	_, raw = f.do(t, http.MethodGet, "/api/v1/checks/web/results?limit=1", token, nil, nil)
	body.Results = nil
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Results) != 1 || body.Results[0].RTTMillis != 20 {
		t.Fatalf("limit: %s", raw)
	}

	for _, bad := range []string{"0", "-3", "many"} {
		if res, _ := f.do(t, http.MethodGet, "/api/v1/checks/web/results?limit="+bad, token, nil, nil); res.StatusCode != http.StatusBadRequest {
			t.Errorf("limit=%s: status %d, want 400", bad, res.StatusCode)
		}
	}

	_, raw = f.do(t, http.MethodGet, "/api/v1/checks/unknown/results", token, nil, nil)
	if !strings.Contains(string(raw), `"results":[]`) {
		t.Fatalf("a check without results must be an empty array: %s", raw)
	}
}
