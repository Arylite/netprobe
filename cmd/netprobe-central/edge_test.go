package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := dispatch(args, &out)
	return out.String(), err
}

func TestEdgeAddListRevoke(t *testing.T) {
	st, url := storetest.OpenURL(t)

	out, err := runCLI(t, "edge", "add", "--database-url", url, "--name", "paris")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	token := lines[len(lines)-1]
	if !strings.HasPrefix(token, "np_") {
		t.Fatalf("the last line is not the token: %q", out)
	}
	if _, ok, err := st.AuthenticateEdge(context.Background(), token); !ok || err != nil {
		t.Fatalf("the printed token does not authenticate: %v, %v", ok, err)
	}

	out, err = runCLI(t, "edge", "list", "--database-url", url)
	if err != nil || !strings.Contains(out, "paris") || !strings.Contains(out, "active") || strings.Contains(out, token) {
		t.Fatalf("list: %q, %v", out, err)
	}

	if _, err := runCLI(t, "edge", "revoke", "--database-url", url, "--name", "paris"); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, "edge", "list", "--database-url", url)
	if !strings.Contains(out, "revoked") {
		t.Fatalf("list after revoke: %q", out)
	}
	if _, err := runCLI(t, "edge", "revoke", "--database-url", url, "--name", "paris"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
}

func TestEdgeCommandsRefuseBadInput(t *testing.T) {
	_, url := storetest.OpenURL(t)
	for name, args := range map[string][]string{
		"no name":        {"edge", "add", "--database-url", url},
		"bad name":       {"edge", "add", "--database-url", url, "--name", "Paris"},
		"no database":    {"edge", "list", "--database-url", ""},
		"unknown":        {"edge", "frobnicate"},
		"no edge action": {"edge"},
		"no command":     {},
		"unknown top":    {"frobnicate"},
	} {
		t.Setenv("NETPROBE_DATABASE_URL", "")
		if _, err := runCLI(t, args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTheDatabaseURLIsNeverEchoed(t *testing.T) {
	t.Setenv("NETPROBE_DATABASE_URL", "")
	_, err := runCLI(t, "edge", "list", "--database-url", "postgres://user:hunter2@127.0.0.1:1/none?connect_timeout=1")
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error %v", err)
	}
}

func TestVersionAndHelp(t *testing.T) {
	if out, err := runCLI(t, "version"); err != nil || !strings.HasPrefix(out, "netprobe-central ") {
		t.Fatalf("version: %q, %v", out, err)
	}
	if out, err := runCLI(t, "help"); err != nil || !strings.Contains(out, "edge add") {
		t.Fatalf("help: %q, %v", out, err)
	}
}

func TestDoctorCommand(t *testing.T) {
	st, url := storetest.OpenURL(t)
	if _, _, err := st.AddEdge(context.Background(), "paris"); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, "doctor", "--database-url", url, "--edge-listen", "127.0.0.1:8080")
	if err != nil || !strings.Contains(out, "ok    database") || !strings.Contains(out, "never reported") {
		t.Fatalf("doctor: %v\n%s", err, out)
	}

	t.Setenv("NETPROBE_DATABASE_URL", "")
	out, err = runCLI(t, "doctor", "--edge-listen", "127.0.0.1:8080")
	if !errors.Is(err, errChecksFailed) || !strings.Contains(out, "FAIL  database") {
		t.Fatalf("doctor without a database: %v\n%s", err, out)
	}
}
