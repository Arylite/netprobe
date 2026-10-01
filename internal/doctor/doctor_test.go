package doctor

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func step(name string, o Outcome, ran *[]string) Step {
	return Step{Name: name, Run: func(context.Context) Outcome {
		*ran = append(*ran, name)
		return o
	}}
}

func TestRunStopsAfterAFailureAndSkipsTheRest(t *testing.T) {
	var ran []string
	lines := Run(context.Background(), []Step{
		step("a", Pass("fine"), &ran),
		step("b", Warning("odd", "look"), &ran),
		step("c", Failure("broken", "fix it"), &ran),
		step("d", Pass("never"), &ran),
	})
	if strings.Join(ran, "") != "abc" {
		t.Fatalf("ran %v", ran)
	}
	if len(lines) != 4 || lines[1].Status != Warn || lines[2].Status != Fail || lines[3].Status != Skip {
		t.Fatalf("lines %+v", lines)
	}
	if !Failed(lines) {
		t.Fatal("Failed() = false")
	}
}

func TestWarningsDoNotFail(t *testing.T) {
	var ran []string
	lines := Run(context.Background(), []Step{step("a", Warning("odd", ""), &ran), step("b", Pass("ok"), &ran)})
	if Failed(lines) || len(ran) != 2 {
		t.Fatalf("lines %+v, ran %v", lines, ran)
	}
}

func TestAStepThatHangsIsCutOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	hang := Step{Name: "hang", Run: func(ctx context.Context) Outcome {
		<-ctx.Done()
		return Failure("timed out", "")
	}}
	time.AfterFunc(50*time.Millisecond, cancel)
	lines := Run(ctx, []Step{hang})
	if len(lines) != 1 || lines[0].Status != Fail {
		t.Fatalf("lines %+v", lines)
	}
}

func TestPrint(t *testing.T) {
	var out bytes.Buffer
	Print(&out, []Line{
		{Name: "dns", Outcome: Pass("resolved")},
		{Name: "tcp", Outcome: Failure("refused", "check the port")},
		{Name: "auth", Outcome: Skipped("not run")},
	})
	got := out.String()
	for _, want := range []string{"ok    dns   resolved", "FAIL  tcp   refused", "-> check the port", "skip  auth  not run"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
}

func TestStatusStrings(t *testing.T) {
	want := map[Status]string{OK: "ok", Warn: "warn", Fail: "FAIL", Skip: "skip"}
	for s, str := range want {
		if s.String() != str {
			t.Errorf("%d.String() = %q, want %q", s, s.String(), str)
		}
	}
}
