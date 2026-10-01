package cli

import "testing"

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
