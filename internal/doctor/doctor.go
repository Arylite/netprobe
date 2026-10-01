package doctor

import (
	"context"
	"fmt"
	"io"
	"time"
)

const stepTimeout = 15 * time.Second

// Status is how a step went.
type Status int

// The possible statuses.
const (
	OK Status = iota
	Warn
	Fail
	Skip
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Fail:
		return "FAIL"
	default:
		return "skip"
	}
}

// Outcome is what a step reports: what it found, and for a problem what to try.
type Outcome struct {
	Status Status
	Detail string
	Hint   string
}

// Pass reports a step that found nothing wrong.
func Pass(detail string) Outcome { return Outcome{Status: OK, Detail: detail} }

// Warning reports something worth knowing that does not stop the next steps.
func Warning(detail, hint string) Outcome { return Outcome{Status: Warn, Detail: detail, Hint: hint} }

// Failure reports a problem; the steps after it are skipped, since they depend
// on it.
func Failure(detail, hint string) Outcome { return Outcome{Status: Fail, Detail: detail, Hint: hint} }

// Skipped reports a step that does not apply.
func Skipped(detail string) Outcome { return Outcome{Status: Skip, Detail: detail} }

// Step is one check.
type Step struct {
	Name string
	Run  func(ctx context.Context) Outcome
}

// Line is the outcome of a named step.
type Line struct {
	Name string
	Outcome
}

// Run executes the steps in order, each with a time limit. After a failure the
// remaining steps are skipped.
func Run(ctx context.Context, steps []Step) []Line {
	lines := make([]Line, 0, len(steps))
	failed := false
	for _, s := range steps {
		if failed {
			lines = append(lines, Line{Name: s.Name, Outcome: Skipped("not run: an earlier step failed")})
			continue
		}
		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
		o := s.Run(stepCtx)
		cancel()
		if o.Status == Fail {
			failed = true
		}
		lines = append(lines, Line{Name: s.Name, Outcome: o})
	}
	return lines
}

// Failed reports whether any step failed.
func Failed(lines []Line) bool {
	for _, l := range lines {
		if l.Status == Fail {
			return true
		}
	}
	return false
}

// Print writes one line per step, and the hint of a problem under it.
func Print(w io.Writer, lines []Line) {
	width := 0
	for _, l := range lines {
		width = max(width, len(l.Name))
	}
	for _, l := range lines {
		fmt.Fprintf(w, "%-4s  %-*s  %s\n", l.Status, width, l.Name, l.Detail)
		if l.Hint != "" {
			fmt.Fprintf(w, "      %-*s  -> %s\n", width, "", l.Hint)
		}
	}
}
