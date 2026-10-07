package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/central/alert"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

type testSender struct {
	got []store.Channel
	err error
}

func (s *testSender) Send(_ context.Context, ch store.Channel, p alert.Payload) error {
	if p.Event != alert.EventTest {
		return errors.New("not a test")
	}
	s.got = append(s.got, ch)
	return s.err
}

func TestChannelAddListTestRemove(t *testing.T) {
	st, url := storetest.OpenURL(t)
	ctx := context.Background()

	withStdin(t, "the secret\n")
	out, err := runCLI(t, "channel", "add", "--database-url", url, "--name", "ops", "--url", "https://hooks.example.com/T0/very-secret-path", "--secret-stdin")
	if err != nil || strings.Contains(out, "very-secret-path") {
		t.Fatalf("add: %q, %v", out, err)
	}
	if _, err := runCLI(t, "channel", "add", "--database-url", url, "--name", "chat", "--url", "http://127.0.0.1:9000/in"); err != nil {
		t.Fatal(err)
	}
	stored, _ := st.ListChannels(ctx)
	if len(stored) != 2 || stored[1].Secret != "the secret" || stored[0].Secret != "" {
		t.Fatalf("stored %+v", stored)
	}

	// The listing says where each goes and whether it is signed, no more.
	out, err = runCLI(t, "channel", "list", "--database-url", url)
	if err != nil || !strings.Contains(out, "hooks.example.com") || !strings.Contains(out, "yes") || strings.Contains(out, "very-secret-path") || strings.Contains(out, "the secret") {
		t.Fatalf("list: %q, %v", out, err)
	}

	fake := &testSender{}
	old := sender
	sender = fake
	t.Cleanup(func() { sender = old })
	out, err = runCLI(t, "channel", "test", "--database-url", url, "--name", "ops")
	if err != nil || !strings.Contains(out, "accepted") || len(fake.got) != 1 || fake.got[0].Name != "ops" {
		t.Fatalf("test: %q, %v, %+v", out, err, fake.got)
	}
	fake.err = errors.New("the channel answered 500 Internal Server Error")
	if _, err := runCLI(t, "channel", "test", "--database-url", url, "--name", "ops"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("failing test: %v", err)
	}
	if _, err := runCLI(t, "channel", "test", "--database-url", url, "--name", "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown channel: %v", err)
	}

	if _, err := runCLI(t, "channel", "add", "--database-url", url, "--name", "ops", "--url", "https://example.com"); !errors.Is(err, store.ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := runCLI(t, "channel", "remove", "--database-url", url, "--name", "ops"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "channel", "remove", "--database-url", url, "--name", "ops"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestChannelCommandsRefuseBadInput(t *testing.T) {
	_, url := storetest.OpenURL(t)
	t.Setenv("NETPROBE_DATABASE_URL", "")
	withStdin(t, "")
	for name, args := range map[string][]string{
		"no name":          {"channel", "add", "--database-url", url, "--url", "https://example.com"},
		"bad name":         {"channel", "add", "--database-url", url, "--name", "Bad Name", "--url", "https://example.com"},
		"no url":           {"channel", "add", "--database-url", url, "--name", "a"},
		"bad scheme":       {"channel", "add", "--database-url", url, "--name", "a", "--url", "ftp://example.com"},
		"credentials":      {"channel", "add", "--database-url", url, "--name", "a", "--url", "https://u:p@example.com"},
		"empty secret":     {"channel", "add", "--database-url", url, "--name", "a", "--url", "https://example.com", "--secret-stdin"},
		"remove no name":   {"channel", "remove", "--database-url", url},
		"test no name":     {"channel", "test", "--database-url", url},
		"unknown":          {"channel", "frobnicate"},
		"no action":        {"channel"},
		"incident":         {"incident"},
		"incident unknown": {"incident", "frobnicate"},
	} {
		if _, err := runCLI(t, args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestIncidentList(t *testing.T) {
	st, url := storetest.OpenURL(t)
	ctx := context.Background()
	edge, _, _ := st.AddEdge(ctx, "paris")
	now := time.Now().UTC()
	if _, err := st.OpenIncident(ctx, store.IncidentCheck, "web", edge.ID, "connection refused", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.OpenIncident(ctx, store.IncidentEdge, "", edge.ID, "no result for 6m0s", now); err != nil {
		t.Fatal(err)
	}
	open, _ := st.OpenIncidents(ctx)
	for _, i := range open {
		if i.Kind == store.IncidentCheck {
			if err := st.ResolveIncident(ctx, i.ID, now.Add(-30*time.Minute), store.ResolutionRecovered); err != nil {
				t.Fatal(err)
			}
		}
	}

	out, err := runCLI(t, "incident", "list", "--database-url", url)
	if err != nil || !strings.Contains(out, "connection refused") || !strings.Contains(out, "recovered after 30m0s") || !strings.Contains(out, "no result for 6m0s") {
		t.Fatalf("list: %q, %v", out, err)
	}
	out, err = runCLI(t, "incident", "list", "--database-url", url, "--open")
	if err != nil || strings.Contains(out, "connection refused") || !strings.Contains(out, "open") {
		t.Fatalf("list --open: %q, %v", out, err)
	}
}

func TestAuditList(t *testing.T) {
	st, url := storetest.OpenURL(t)
	ctx := context.Background()
	for _, e := range []store.AuditEvent{
		{At: time.Now().Add(-time.Hour), Actor: "alice", Action: "edge.create", Target: "paris", ClientIP: "203.0.113.7"},
		{At: time.Now(), Action: "login.failed", ClientIP: "198.51.100.2"},
	} {
		if err := st.RecordAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	out, err := runCLI(t, "audit", "list", "--database-url", url)
	if err != nil || !strings.Contains(out, "alice") || !strings.Contains(out, "edge.create") || !strings.Contains(out, "paris") || !strings.Contains(out, "198.51.100.2") {
		t.Fatalf("list: %q, %v", out, err)
	}
	if strings.Index(out, "login.failed") > strings.Index(out, "edge.create") {
		t.Fatalf("the newest is not first: %q", out)
	}
	if _, err := runCLI(t, "audit"); err == nil {
		t.Fatal("no action was accepted")
	}
}
