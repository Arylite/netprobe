package logsafe

import "testing"

func TestLine(t *testing.T) {
	if got := Line("a\r\nb\nc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
}
