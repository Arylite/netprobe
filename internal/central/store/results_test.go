package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func TestChecks(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()

	if got, err := s.ListChecks(ctx); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty list: %v, %v", got, err)
	}
	b := api.Check{ID: "b", Kind: api.KindHTTP, Target: "https://example.com", IntervalSeconds: 30}
	a := api.Check{ID: "a", Kind: api.KindTCP, Target: "example.com:22", IntervalSeconds: 10}
	for _, c := range []api.Check{b, a} {
		if err := s.AddCheck(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.ListChecks(ctx); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("list: %+v", got)
	}

	if err := s.AddCheck(ctx, a); !errors.Is(err, store.ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := s.AddCheck(ctx, api.Check{ID: "bad"}); err == nil {
		t.Fatal("an invalid check was stored")
	}
	if err := s.RemoveCheck(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCheck(ctx, "a"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestResultsRoundTripNewestFirst(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	edge, _, err := s.AddEdge(ctx, "paris")
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC().Truncate(time.Millisecond)
	batch := []api.Result{
		{CheckID: "web", At: base.Add(-2 * time.Minute), OK: true, RTTMillis: 12.5},
		{CheckID: "web", At: base.Add(-1 * time.Minute), OK: false, RTTMillis: 3, Error: "connection refused"},
		{CheckID: "web", At: base, OK: true, RTTMillis: 7},
		{CheckID: "other", At: base, OK: true},
	}
	if err := s.InsertResults(ctx, edge.ID, batch); err != nil {
		t.Fatal(err)
	}

	got, err := s.RecentResults(ctx, "web", 10)
	if err != nil || len(got) != 3 {
		t.Fatalf("RecentResults() = %+v, %v", got, err)
	}
	if got[0].RTTMillis != 7 || got[2].RTTMillis != 12.5 || got[1].OK || got[1].Error != "connection refused" {
		t.Fatalf("order or values: %+v", got)
	}
	if got[0].EdgeID != edge.ID || !got[0].At.Equal(base) {
		t.Fatalf("edge or time: %+v", got[0])
	}
	if limited, _ := s.RecentResults(ctx, "web", 1); len(limited) != 1 || limited[0].RTTMillis != 7 {
		t.Fatalf("limit: %+v", limited)
	}
	if none, err := s.RecentResults(ctx, "unknown", 10); err != nil || len(none) != 0 {
		t.Fatalf("unknown check: %+v, %v", none, err)
	}
}

func TestResultsOutliveTheirCheck(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	edge, _, _ := s.AddEdge(ctx, "paris")
	_ = s.AddCheck(ctx, api.Check{ID: "web", Kind: api.KindTCP, Target: "example.com:80", IntervalSeconds: 5})
	_ = s.InsertResults(ctx, edge.ID, []api.Result{{CheckID: "web", At: time.Now(), OK: true}})
	if err := s.RemoveCheck(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.RecentResults(ctx, "web", 10); len(got) != 1 {
		t.Fatalf("results of a removed check: %+v", got)
	}
}

func TestInsertResultsIsAllOrNothingAndNeedsAKnownEdge(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	r := api.Result{CheckID: "web", At: time.Now(), OK: true}

	if err := s.InsertResults(ctx, "no-such-edge", []api.Result{r}); err == nil {
		t.Fatal("results of an unknown edge were stored")
	}
	if err := s.InsertResults(ctx, "no-such-edge", nil); err != nil {
		t.Fatalf("an empty batch must be a no-op: %v", err)
	}
	if got, _ := s.RecentResults(ctx, "web", 10); len(got) != 0 {
		t.Fatalf("a failed batch left rows: %+v", got)
	}
}

func TestRecentResultsLimitIsBounded(t *testing.T) {
	s := storetest.Open(t)
	edge, _, _ := s.AddEdge(context.Background(), "paris")
	batch := make([]api.Result, 1200)
	now := time.Now()
	for i := range batch {
		batch[i] = api.Result{CheckID: "web", At: now.Add(time.Duration(i) * time.Second), OK: true}
	}
	if err := s.InsertResults(context.Background(), edge.ID, batch); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.RecentResults(context.Background(), "web", 100_000); len(got) != 1000 {
		t.Fatalf("limit not applied: %d rows", len(got))
	}
	if got, _ := s.RecentResults(context.Background(), "web", -5); len(got) != 1 {
		t.Fatalf("a negative limit returned %d rows", len(got))
	}
}

func TestLastResultPerEdge(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	paris, _, _ := s.AddEdge(ctx, "paris")
	lyon, _, _ := s.AddEdge(ctx, "lyon")
	_, _, _ = s.AddEdge(ctx, "silent")

	base := time.Now().UTC().Truncate(time.Millisecond)
	_ = s.InsertResults(ctx, paris.ID, []api.Result{
		{CheckID: "web", At: base.Add(-time.Hour), OK: true},
		{CheckID: "web", At: base.Add(-time.Minute), OK: true},
	})
	_ = s.InsertResults(ctx, lyon.ID, []api.Result{{CheckID: "web", At: base.Add(-time.Second), OK: true}})

	last, err := s.LastResultPerEdge(ctx)
	if err != nil || len(last) != 2 {
		t.Fatalf("LastResultPerEdge() = %v, %v", last, err)
	}
	if !last[paris.ID].Equal(base.Add(-time.Minute)) || !last[lyon.ID].Equal(base.Add(-time.Second)) {
		t.Fatalf("times: %v", last)
	}
}
