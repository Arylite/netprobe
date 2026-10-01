package main

import (
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/central/store"
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

func TestAnyActive(t *testing.T) {
	if anyActive(nil) {
		t.Fatal("no edge is not an active edge")
	}
	now := time.Now()
	revoked := store.Edge{RevokedAt: &now}
	if anyActive([]store.Edge{revoked}) {
		t.Fatal("a revoked edge counted as active")
	}
	if !anyActive([]store.Edge{revoked, {}}) {
		t.Fatal("an active edge was not seen")
	}
}
