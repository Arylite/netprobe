package central

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
	"github.com/Arylite/netprobe/internal/doctor"
)

func diagnose(t *testing.T, cfg DoctorConfig) map[string]doctor.Line {
	t.Helper()
	steps, closeFn := DoctorSteps(cfg)
	defer closeFn()
	out := map[string]doctor.Line{}
	for _, l := range doctor.Run(context.Background(), steps) {
		out[l.Name] = l
	}
	return out
}

func want(t *testing.T, lines map[string]doctor.Line, name string, status doctor.Status) doctor.Line {
	t.Helper()
	l, ok := lines[name]
	if !ok || l.Status != status {
		t.Fatalf("step %s = %v %q, want %v (all: %+v)", name, l.Status, l.Detail, status, lines)
	}
	return l
}

func TestDoctorHealthyCentral(t *testing.T) {
	st, url := storetest.OpenURL(t)
	ctx := context.Background()
	edge, _, _ := st.AddEdge(ctx, "paris")
	_ = st.AddCheck(ctx, api.Check{ID: "web", Kind: api.KindTCP, Target: "a:1", IntervalSeconds: 5})
	_ = st.InsertResults(ctx, edge.ID, []api.Result{{CheckID: "web", At: time.Now(), OK: true}})

	lines := diagnose(t, DoctorConfig{DatabaseURL: url, EdgeListen: "127.0.0.1:8080", APIListen: "127.0.0.1:8081"})
	for _, name := range []string{"database", "timescaledb", "schema", "edges", "checks", "activity", "edge-listen", "api-listen"} {
		want(t, lines, name, doctor.OK)
	}
}

func TestDoctorAlertingAndRetention(t *testing.T) {
	st, url := storetest.OpenURL(t)
	ctx := context.Background()
	edge, _, _ := st.AddEdge(ctx, "paris")
	cfg := DoctorConfig{DatabaseURL: url, EdgeListen: "127.0.0.1:8080", APIListen: "127.0.0.1:8081"}

	lines := diagnose(t, cfg)
	if l := want(t, lines, "alerting", doctor.Warn); !strings.Contains(l.Detail, "no notification channel") {
		t.Fatalf("no channel: %q", l.Detail)
	}
	if l := want(t, lines, "retention", doctor.Warn); !strings.Contains(l.Detail, "for ever") || l.Hint == "" {
		t.Fatalf("no retention: %q", l.Detail)
	}

	if _, err := st.AddChannel(ctx, "ops", "https://example.com/hook", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRetention(ctx, 90*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	lines = diagnose(t, cfg)
	want(t, lines, "alerting", doctor.OK)
	if l := want(t, lines, "retention", doctor.OK); !strings.Contains(l.Detail, "2160h") {
		t.Fatalf("retention: %q", l.Detail)
	}

	if _, err := st.OpenIncident(ctx, store.IncidentCheck, "web", edge.ID, "refused", time.Now()); err != nil {
		t.Fatal(err)
	}
	if l := want(t, diagnose(t, cfg), "alerting", doctor.Warn); !strings.Contains(l.Detail, "web on paris") {
		t.Fatalf("open incident: %q", l.Detail)
	}
}

func TestDoctorNeedsADatabaseURL(t *testing.T) {
	lines := diagnose(t, DoctorConfig{EdgeListen: "127.0.0.1:8080", APIListen: "127.0.0.1:8081"})
	want(t, lines, "database", doctor.Fail)
	want(t, lines, "schema", doctor.Skip)
}

func TestDoctorUnreachableDatabase(t *testing.T) {
	l := want(t, diagnose(t, DoctorConfig{DatabaseURL: "postgres://u:hunter2@127.0.0.1:1/none?connect_timeout=1", EdgeListen: "127.0.0.1:8080", APIListen: "127.0.0.1:8081"}), "database", doctor.Fail)
	if strings.Contains(l.Detail, "hunter2") || l.Hint == "" {
		t.Fatalf("detail %q hint %q", l.Detail, l.Hint)
	}
}

func TestDoctorEmptyDatabaseIsAWarningNotAFailure(t *testing.T) {
	lines := diagnose(t, DoctorConfig{DatabaseURL: storetest.EmptyDatabase(t), EdgeListen: "127.0.0.1:8080", APIListen: "127.0.0.1:8081"})
	want(t, lines, "database", doctor.OK)
	if lines["timescaledb"].Status == doctor.Fail {
		t.Fatalf("a fresh database must not fail on the extension: %+v", lines["timescaledb"])
	}
	want(t, lines, "schema", doctor.Warn)
	want(t, lines, "edges", doctor.Skip)
}

func TestDoctorSilentAndMissingEdges(t *testing.T) {
	st, url := storetest.OpenURL(t)
	ctx := context.Background()
	paris, _, _ := st.AddEdge(ctx, "paris")
	_, _, _ = st.AddEdge(ctx, "quiet")
	_ = st.AddCheck(ctx, api.Check{ID: "web", Kind: api.KindTCP, Target: "a:1", IntervalSeconds: 5})
	_ = st.InsertResults(ctx, paris.ID, []api.Result{{CheckID: "web", At: time.Now().Add(-3 * time.Hour), OK: true}})

	l := want(t, diagnose(t, DoctorConfig{DatabaseURL: url, EdgeListen: "127.0.0.1:8080", APIListen: "127.0.0.1:8081"}), "activity", doctor.Warn)
	if !strings.Contains(l.Detail, "paris (last report 3h0m0s ago)") || !strings.Contains(l.Detail, "quiet (never reported)") {
		t.Fatalf("detail %q", l.Detail)
	}
}

func TestDoctorNoEdgesAndNoChecks(t *testing.T) {
	_, url := storetest.OpenURL(t)
	lines := diagnose(t, DoctorConfig{DatabaseURL: url, EdgeListen: "127.0.0.1:8080", APIListen: "127.0.0.1:8081"})
	want(t, lines, "edges", doctor.Warn)
	want(t, lines, "checks", doctor.Warn)
	want(t, lines, "activity", doctor.Skip)
}

func TestDoctorListenAddresses(t *testing.T) {
	_, url := storetest.OpenURL(t)
	tests := map[string]doctor.Status{
		"127.0.0.1:8080": doctor.OK,
		"localhost:8080": doctor.OK,
		"[::1]:8080":     doctor.OK,
		":8080":          doctor.Warn,
		"0.0.0.0:8080":   doctor.Warn,
		"10.0.0.5:8080":  doctor.Warn,
	}
	for addr, status := range tests {
		lines := diagnose(t, DoctorConfig{DatabaseURL: url, EdgeListen: addr, APIListen: "127.0.0.1:8081"})
		want(t, lines, "edge-listen", status)
		want(t, lines, "api-listen", doctor.OK)
		lines = diagnose(t, DoctorConfig{DatabaseURL: url, EdgeListen: "127.0.0.1:8080", APIListen: addr})
		want(t, lines, "api-listen", status)
	}
	want(t, diagnose(t, DoctorConfig{DatabaseURL: url, EdgeListen: "nonsense", APIListen: "127.0.0.1:8081"}), "edge-listen", doctor.Fail)
}
