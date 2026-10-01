package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Arylite/netprobe/internal/central/registry"
)

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := dispatch(args, &out)
	return out.String(), err
}

func TestEdgeAddListRevoke(t *testing.T) {
	dir := t.TempDir()

	out, err := runCLI(t, "edge", "add", "--data-dir", dir, "--name", "paris")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	token := lines[len(lines)-1]
	if !strings.HasPrefix(token, "np_") {
		t.Fatalf("the last line is not the token: %q", out)
	}
	reg, err := registry.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Authenticate(token); !ok {
		t.Fatal("the printed token does not authenticate")
	}

	out, err = runCLI(t, "edge", "list", "--data-dir", dir)
	if err != nil || !strings.Contains(out, "paris") || !strings.Contains(out, "active") || strings.Contains(out, token) {
		t.Fatalf("list: %q, %v", out, err)
	}

	if _, err := runCLI(t, "edge", "revoke", "--data-dir", dir, "--name", "paris"); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, "edge", "list", "--data-dir", dir)
	if !strings.Contains(out, "revoked") {
		t.Fatalf("list after revoke: %q", out)
	}
	if _, err := runCLI(t, "edge", "revoke", "--data-dir", dir, "--name", "paris"); !errors.Is(err, registry.ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
}

func TestEdgeCommandsRefuseBadInput(t *testing.T) {
	dir := t.TempDir()
	for name, args := range map[string][]string{
		"no name":        {"edge", "add", "--data-dir", dir},
		"bad name":       {"edge", "add", "--data-dir", dir, "--name", "Paris"},
		"unknown":        {"edge", "frobnicate"},
		"no edge action": {"edge"},
		"no command":     {},
		"unknown top":    {"frobnicate"},
	} {
		if _, err := runCLI(t, args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
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
