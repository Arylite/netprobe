package main

import (
	"testing"
	"time"
)

func TestRunRejectsBadSettings(t *testing.T) {
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := run(tt.central, tt.poll, tt.level); err == nil {
				t.Fatal("run accepted the settings")
			}
		})
	}
}
