package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func TestCheckAddListRemove(t *testing.T) {
	st, url := storetest.OpenURL(t)

	if _, err := runCLI(t, "check", "add", "--database-url", url, "--id", "web", "--kind", "http", "--target", "https://example.com", "--interval", "60"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "check", "add", "--database-url", url, "--id", "ssh", "--kind", "tcp", "--target", "example.com:22"); err != nil {
		t.Fatal(err)
	}
	stored, _ := st.ListChecks(context.Background())
	if len(stored) != 2 || stored[0].ID != "ssh" || stored[0].IntervalSeconds != 30 || stored[1].IntervalSeconds != 60 {
		t.Fatalf("stored %+v", stored)
	}

	out, err := runCLI(t, "check", "list", "--database-url", url)
	if err != nil || !strings.Contains(out, "example.com:22") || !strings.Contains(out, "60s") {
		t.Fatalf("list: %q, %v", out, err)
	}

	if _, err := runCLI(t, "check", "add", "--database-url", url, "--id", "ssh", "--kind", "tcp", "--target", "example.com:22"); !errors.Is(err, store.ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := runCLI(t, "check", "remove", "--database-url", url, "--id", "ssh"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "check", "remove", "--database-url", url, "--id", "ssh"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestCheckCommandsRefuseBadInput(t *testing.T) {
	_, url := storetest.OpenURL(t)
	t.Setenv("NETPROBE_DATABASE_URL", "")
	for name, args := range map[string][]string{
		"no id":         {"check", "add", "--database-url", url, "--kind", "tcp", "--target", "a:1"},
		"bad kind":      {"check", "add", "--database-url", url, "--id", "x", "--kind", "carrier-pigeon", "--target", "a:1"},
		"no target":     {"check", "add", "--database-url", url, "--id", "x", "--kind", "tcp"},
		"bad expect":    {"check", "add", "--database-url", url, "--id", "x", "--kind", "tls", "--target", "a", "--expect", "soon", "--interval", "60"},
		"zero interval": {"check", "add", "--database-url", url, "--id", "x", "--kind", "tcp", "--target", "a:1", "--interval", "0"},
		"remove no id":  {"check", "remove", "--database-url", url},
		"unknown":       {"check", "frobnicate"},
		"no action":     {"check"},
	} {
		if _, err := runCLI(t, args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
