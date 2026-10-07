package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func TestAuditTrail(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	if got, err := s.ListAudit(ctx, 10); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty: %v, %v", got, err)
	}
	for i, e := range []store.AuditEvent{
		{At: now.Add(-48 * time.Hour), Actor: "alice", Action: "login", ClientIP: "203.0.113.7"},
		{At: now.Add(-time.Hour), Actor: "alice", Action: "edge.create", Target: "paris", ClientIP: "203.0.113.7"},
		{At: now, Actor: "", Action: "login.failed", Target: "", ClientIP: "198.51.100.2"},
	} {
		if err := s.RecordAudit(ctx, e); err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
	}
	got, err := s.ListAudit(ctx, 10)
	if err != nil || len(got) != 3 || got[0].Action != "login.failed" || got[1].Target != "paris" || got[2].ClientIP != "203.0.113.7" {
		t.Fatalf("list: %+v, %v", got, err)
	}
	if limited, _ := s.ListAudit(ctx, 1); len(limited) != 1 {
		t.Fatalf("limit: %+v", limited)
	}

	n, err := s.PurgeAudit(ctx, now.Add(-24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("purge: %d, %v", n, err)
	}
	if left, _ := s.ListAudit(ctx, 10); len(left) != 2 {
		t.Fatalf("left: %+v", left)
	}
}

func TestAuditValuesAreBounded(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	if err := s.RecordAudit(ctx, store.AuditEvent{At: time.Now(), Actor: strings.Repeat("a", 1000), Action: "login", Target: strings.Repeat("é", 500)}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ListAudit(ctx, 1)
	if len(got[0].Actor) > 128 || len(got[0].Target) > 128 || strings.ContainsRune(got[0].Target, '�') {
		t.Fatalf("%d, %d bytes", len(got[0].Actor), len(got[0].Target))
	}
}
