package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func TestAddAndAuthenticateAnEdge(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()

	e, token, err := s.AddEdge(ctx, "paris")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "np_") || e.Name != "paris" || e.ID == "" || !e.Active() {
		t.Fatalf("edge %+v token %q", e, token)
	}

	got, ok, err := s.AuthenticateEdge(ctx, token)
	if err != nil || !ok || got.ID != e.ID || got.Name != "paris" {
		t.Fatalf("AuthenticateEdge() = %+v, %v, %v", got, ok, err)
	}
	for _, bad := range []string{"", "np_wrong", token + "x", strings.TrimPrefix(token, "np_")} {
		if _, ok, err := s.AuthenticateEdge(ctx, bad); ok || err != nil {
			t.Errorf("AuthenticateEdge(%q) = ok %v, err %v", bad, ok, err)
		}
	}
}

func TestEdgeNamesAreValidatedAndUniqueWhileActive(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	for _, bad := range []string{"", "Paris", "-a", "a b", strings.Repeat("a", 64), "a/b"} {
		if _, _, err := s.AddEdge(ctx, bad); err == nil {
			t.Errorf("AddEdge(%q) succeeded", bad)
		}
	}
	if _, _, err := s.AddEdge(ctx, "paris"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddEdge(ctx, "paris"); !errors.Is(err, store.ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestRevokeStopsTheTokenAndFreesTheName(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	_, token, _ := s.AddEdge(ctx, "paris")

	if err := s.RevokeEdge(ctx, "paris"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.AuthenticateEdge(ctx, token); ok {
		t.Fatal("a revoked token still authenticates")
	}
	if err := s.RevokeEdge(ctx, "paris"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	if _, _, err := s.AddEdge(ctx, "paris"); err != nil {
		t.Fatalf("the name was not freed: %v", err)
	}

	edges, err := s.ListEdges(ctx)
	if err != nil || len(edges) != 2 || edges[0].Active() || !edges[1].Active() {
		t.Fatalf("ListEdges() = %+v, %v", edges, err)
	}
}

func TestAuthenticateReportsDatabaseFailures(t *testing.T) {
	s := storetest.Open(t)
	_, token, _ := s.AddEdge(context.Background(), "paris")
	s.Close()
	if _, ok, err := s.AuthenticateEdge(context.Background(), token); ok || err == nil {
		t.Fatalf("ok=%v err=%v: a database failure must not look like a bad token", ok, err)
	}
}
