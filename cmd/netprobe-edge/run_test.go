package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/probe"
)

func validConfig() config {
	return config{
		central: "http://127.0.0.1:1",
		deny:    strings.Join(probe.DefaultDeny, ","),
		poll:    time.Second,
		report:  time.Second,
		level:   "info",
	}
}

func TestRunRejectsBadSettings(t *testing.T) {
	t.Setenv("NETPROBE_TOKEN", "np_secret")
	tests := map[string]func(*config){
		"no central":            func(c *config) { c.central = "" },
		"poll too short":        func(c *config) { c.poll = time.Millisecond },
		"report too short":      func(c *config) { c.report = time.Millisecond },
		"bad level":             func(c *config) { c.level = "loud" },
		"bad url":               func(c *config) { c.central = "central" },
		"token over plain http": func(c *config) { c.central = "http://central.example.com" },
		"bad deny range":        func(c *config) { c.deny = "10.0.0.1" },
		"empty deny":            func(c *config) { c.deny = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := validConfig()
			mutate(&cfg)
			if err := run(cfg); err == nil {
				t.Fatal("run accepted the settings")
			}
		})
	}
}

func TestRunNeedsAToken(t *testing.T) {
	t.Setenv("NETPROBE_TOKEN", "")
	if err := run(validConfig()); err == nil {
		t.Fatal("run started without a token")
	}
}

func TestParseDeny(t *testing.T) {
	p, err := parseDeny(strings.Join(probe.DefaultDeny, ","))
	if err != nil || !p.Denies(netip.MustParseAddr("127.0.0.1")) || p.Denies(netip.MustParseAddr("10.0.0.1")) {
		t.Fatalf("default: %v", err)
	}
	p, err = parseDeny("10.0.0.0/8, 192.168.0.0/16")
	if err != nil || !p.Denies(netip.MustParseAddr("10.1.1.1")) || !p.Denies(netip.MustParseAddr("192.168.1.1")) || p.Denies(netip.MustParseAddr("127.0.0.1")) {
		t.Fatalf("custom: %v", err)
	}
	p, err = parseDeny("none")
	if err != nil || p.Denies(netip.MustParseAddr("127.0.0.1")) {
		t.Fatalf("none: %v", err)
	}
}

func TestReadToken(t *testing.T) {
	t.Setenv("NETPROBE_TOKEN", "  np_from_env\n")
	if got, err := readToken(""); err != nil || got != "np_from_env" {
		t.Fatalf("env: %q, %v", got, err)
	}

	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("np_from_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readToken(file); err != nil || got != "np_from_file" {
		t.Fatalf("file wins over env: %q, %v", got, err)
	}
	if _, err := readToken(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("accepted a missing file")
	}

	t.Setenv("NETPROBE_TOKEN", "")
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readToken(empty); err == nil {
		t.Fatal("accepted an empty token file")
	}
}
