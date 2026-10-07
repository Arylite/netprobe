package cli

import (
	"testing"
	"time"
)

func TestGetenv(t *testing.T) {
	t.Setenv("NETPROBE_TEST_SET", "value")
	t.Setenv("NETPROBE_TEST_EMPTY", "")
	if got := Getenv("NETPROBE_TEST_SET", "x"); got != "value" {
		t.Fatalf("set: %q", got)
	}
	if got := Getenv("NETPROBE_TEST_EMPTY", "x"); got != "x" {
		t.Fatalf("empty: %q", got)
	}
	if got := Getenv("NETPROBE_TEST_UNSET", "x"); got != "x" {
		t.Fatalf("unset: %q", got)
	}
}

func TestNewLogger(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		if _, err := NewLogger(level); err != nil {
			t.Errorf("%s: %v", level, err)
		}
	}
	if _, err := NewLogger("loud"); err == nil {
		t.Fatal("accepted an unknown level")
	}
}

func TestGetenvDuration(t *testing.T) {
	t.Setenv("NETPROBE_TEST_DURATION", "45s")
	if got, err := GetenvDuration("NETPROBE_TEST_DURATION", time.Second); err != nil || got != 45*time.Second {
		t.Fatalf("set: %v, %v", got, err)
	}
	if got, err := GetenvDuration("NETPROBE_TEST_DURATION_UNSET", time.Minute); err != nil || got != time.Minute {
		t.Fatalf("unset: %v, %v", got, err)
	}
	t.Setenv("NETPROBE_TEST_DURATION", "soon")
	if _, err := GetenvDuration("NETPROBE_TEST_DURATION", time.Second); err == nil {
		t.Fatal("accepted a malformed duration")
	}
}

func TestGetenvInt(t *testing.T) {
	t.Setenv("NETPROBE_TEST_INT", "7")
	if got, err := GetenvInt("NETPROBE_TEST_INT", 1); err != nil || got != 7 {
		t.Fatalf("set: %v, %v", got, err)
	}
	if got, err := GetenvInt("NETPROBE_TEST_INT_UNSET", 3); err != nil || got != 3 {
		t.Fatalf("unset: %v, %v", got, err)
	}
	t.Setenv("NETPROBE_TEST_INT", "many")
	if _, err := GetenvInt("NETPROBE_TEST_INT", 1); err == nil {
		t.Fatal("accepted a malformed integer")
	}
}
