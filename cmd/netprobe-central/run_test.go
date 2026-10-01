package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsLoopback(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:8080": true,
		"[::1]:8080":     true,
		"localhost:8080": true,
		":8080":          false,
		"0.0.0.0:8080":   false,
		"10.0.0.5:8080":  false,
		"nonsense":       false,
	}
	for addr, want := range tests {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestLoadChecks(t *testing.T) {
	if checks, err := loadChecks(""); err != nil || checks != nil {
		t.Fatalf("no file: %v %v", checks, err)
	}
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"checks":[{"id":"c1","kind":"tcp","target":"example.com:443","interval_seconds":10}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if checks, err := loadChecks(good); err != nil || len(checks) != 1 {
		t.Fatalf("good file: %v %v", checks, err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"checks":[],"typo":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadChecks(bad); err == nil {
		t.Fatal("accepted an unknown field")
	}
	if _, err := loadChecks(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("accepted a missing file")
	}
}
