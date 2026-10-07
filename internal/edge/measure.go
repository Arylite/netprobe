package edge

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/probe"
)

// Measurer runs one check and describes the outcome.
type Measurer func(ctx context.Context, c api.Check) api.Result

// ProbeMeasurer runs each check with the probe of its kind.
func ProbeMeasurer(p *probe.Prober) Measurer {
	return func(ctx context.Context, c api.Check) api.Result {
		at := time.Now().UTC()
		o := p.Run(ctx, c)
		return api.Result{
			CheckID:   c.ID,
			At:        at,
			OK:        o.OK,
			RTTMillis: float64(o.RTT) / float64(time.Millisecond),
			Error:     clip(o.Err, api.MaxErrorLength),
		}
	}
}

// clip cuts s to at most n bytes without splitting a character.
func clip(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
