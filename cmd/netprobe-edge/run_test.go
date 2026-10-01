package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunRejectsBadSettings(t *testing.T) {
	t.Setenv("NETPROBE_TOKEN", "np_secret")
	tests := []struct {
		name    string
		central string
		poll    time.Duration
		level   string
	}{
		{"no central", "", time.Second, "info"},
		{"poll too short", "http://127.0.0.1:1", time.Millisecond, "info"},
		{"bad level", "http://127.0.0.1:1", time.Second, "loud"},
		{"bad url", "central", time.Second, "info"},
		{"token over plain http", "http://central.example.com", time.Second, "info"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := run(tt.central, "", tt.poll, tt.level); err == nil {
				t.Fatal("run accepted the settings")
			}
		})
	}
}

func TestRunNeedsAToken(t *testing.T) {
	t.Setenv("NETPROBE_TOKEN", "")
	if err := run("http://127.0.0.1:1", "", time.Second, "info"); err == nil {
		t.Fatal("run started without a token")
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
