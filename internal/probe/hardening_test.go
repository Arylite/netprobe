package probe

import (
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

// The target and the expectation of a check come from the central: whatever they
// hold, reading them must give an error, never a panic.
func FuzzParse(f *testing.F) {
	for _, kind := range api.Kinds {
		f.Add(kind, "example.com:443", "")
		f.Add(kind, "MX example.com@1.1.1.1", "200;contains:ok")
		f.Add(kind, "[::1]:53", "250ms")
		f.Add(kind, "https://user:pw@example.com/a?b=c", "2xx;absent:")
		f.Add(kind, "@", ";;;")
		f.Add(kind, "\xff\x00:", "-1")
		f.Add(kind, strings.Repeat("a.", 300), "9999999999999999999999")
	}
	f.Fuzz(func(t *testing.T, kind, target, expect string) {
		_ = Validate(api.Check{ID: "x", Kind: kind, Target: target, Expect: expect, IntervalSeconds: 1 << 40})
	})
}

// A service that never ends its line must not make the edge hold what it sends.
func TestBannerReadsAtMostOneBufferOfAnEndlessLine(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		chunk := []byte(strings.Repeat("a", 64<<10))
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				for range 4096 { // 256 MiB at most
					if _, err := c.Write(chunk); err != nil {
						return
					}
				}
			}()
		}
	}()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	p := New(Policy{}, time.Second)
	wantOK(t, p.Run(ctx(), api.Check{Kind: api.KindBanner, Target: ln.Addr().String(), Expect: "aaaa"}))
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > 8<<20 {
		t.Fatalf("the banner check allocated %d MiB reading a line that never ends", got>>20)
	}
}
