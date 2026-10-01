package edge

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/doctor"
)

const goodToken = "np_good"

// centralStub answers like a healthy central; options change one behaviour.
func centralStub(health int, date string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+api.PathHealth, func(w http.ResponseWriter, _ *http.Request) {
		if date != "" {
			w.Header().Set("Date", date)
		}
		w.WriteHeader(health)
	})
	mux.HandleFunc("GET "+api.PathAssignments, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+goodToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(api.Assignments{Checks: []api.Check{{ID: "c", Kind: api.KindTCP, Target: "a:1", IntervalSeconds: 5}}})
	})
	return mux
}

func diagnose(cfg DoctorConfig) map[string]doctor.Line {
	out := map[string]doctor.Line{}
	for _, l := range doctor.Run(context.Background(), DoctorSteps(cfg)) {
		out[l.Name] = l
	}
	return out
}

func status(t *testing.T, lines map[string]doctor.Line, name string, want doctor.Status) doctor.Line {
	t.Helper()
	l, ok := lines[name]
	if !ok || l.Status != want {
		t.Fatalf("step %s = %v %q, want %v (all: %+v)", name, l.Status, l.Detail, want, lines)
	}
	return l
}

func TestDoctorHealthyCentral(t *testing.T) {
	ts := httptest.NewServer(centralStub(http.StatusNoContent, ""))
	defer ts.Close()
	lines := diagnose(DoctorConfig{Central: ts.URL, Token: goodToken})
	for _, name := range []string{"config", "dns", "tcp", "health", "auth", "clock"} {
		status(t, lines, name, doctor.OK)
	}
	status(t, lines, "tls", doctor.Skip)
	if !strings.Contains(lines["auth"].Detail, "1 checks") {
		t.Fatalf("auth: %q", lines["auth"].Detail)
	}
}

func TestDoctorNeedsAToken(t *testing.T) {
	lines := diagnose(DoctorConfig{Central: "http://127.0.0.1:1"})
	status(t, lines, "config", doctor.Fail)
	status(t, lines, "dns", doctor.Skip)
}

func TestDoctorRefusesATokenOverPlainHTTPToARemoteCentral(t *testing.T) {
	l := status(t, diagnose(DoctorConfig{Central: "http://central.example.com", Token: goodToken}), "config", doctor.Fail)
	if !strings.Contains(l.Detail, "plain HTTP") {
		t.Fatalf("detail %q", l.Detail)
	}
}

func TestDoctorStopsAtAnUnreachablePort(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	url := ts.URL
	ts.Close()
	lines := diagnose(DoctorConfig{Central: url, Token: goodToken})
	status(t, lines, "dns", doctor.OK)
	l := status(t, lines, "tcp", doctor.Fail)
	if l.Hint == "" {
		t.Fatal("a failure must say what to try")
	}
	status(t, lines, "auth", doctor.Skip)
}

func TestDoctorUnknownHost(t *testing.T) {
	status(t, diagnose(DoctorConfig{Central: "https://no-such-host.invalid:8443", Token: goodToken}), "dns", doctor.Fail)
}

func TestDoctorWrongToken(t *testing.T) {
	ts := httptest.NewServer(centralStub(http.StatusNoContent, ""))
	defer ts.Close()
	l := status(t, diagnose(DoctorConfig{Central: ts.URL, Token: "np_revoked"}), "auth", doctor.Fail)
	if !strings.Contains(l.Hint, "revoked") {
		t.Fatalf("hint %q", l.Hint)
	}
}

func TestDoctorCentralWithoutDatabase(t *testing.T) {
	ts := httptest.NewServer(centralStub(http.StatusServiceUnavailable, ""))
	defer ts.Close()
	lines := diagnose(DoctorConfig{Central: ts.URL, Token: goodToken})
	l := status(t, lines, "health", doctor.Fail)
	if !strings.Contains(l.Hint, "netprobe-central doctor") {
		t.Fatalf("hint %q", l.Hint)
	}
	status(t, lines, "auth", doctor.Skip)
}

func TestDoctorSomethingThatIsNotACentral(t *testing.T) {
	ts := httptest.NewServer(centralStub(http.StatusOK, ""))
	defer ts.Close()
	status(t, diagnose(DoctorConfig{Central: ts.URL, Token: goodToken}), "health", doctor.Fail)
}

func TestDoctorClockSkew(t *testing.T) {
	ts := httptest.NewServer(centralStub(http.StatusNoContent, time.Now().Add(-10*time.Minute).UTC().Format(http.TimeFormat)))
	defer ts.Close()
	l := status(t, diagnose(DoctorConfig{Central: ts.URL, Token: goodToken}), "clock", doctor.Warn)
	if !strings.Contains(l.Hint, "NTP") {
		t.Fatalf("hint %q", l.Hint)
	}
}

func TestDoctorTLS(t *testing.T) {
	ts := httptest.NewTLSServer(centralStub(http.StatusNoContent, ""))
	defer ts.Close()

	// The certificate of the test server is not signed by a trusted authority.
	lines := diagnose(DoctorConfig{Central: ts.URL, Token: goodToken})
	l := status(t, lines, "tls", doctor.Fail)
	if !strings.Contains(l.Hint, "authority") {
		t.Fatalf("hint %q", l.Hint)
	}
	status(t, lines, "health", doctor.Skip)

	// Trusting it makes the whole chain pass.
	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	lines = diagnose(DoctorConfig{Central: ts.URL, Token: goodToken, TLS: &tls.Config{RootCAs: pool}})
	for _, name := range []string{"config", "tcp", "tls", "health", "auth"} {
		status(t, lines, name, doctor.OK)
	}
}

func TestDoctorPlainHTTPOnATLSPort(t *testing.T) {
	ts := httptest.NewServer(centralStub(http.StatusNoContent, ""))
	defer ts.Close()
	// https:// against a server that only speaks HTTP.
	l := status(t, diagnose(DoctorConfig{Central: strings.Replace(ts.URL, "http://", "https://", 1), Token: goodToken}), "tls", doctor.Fail)
	if !strings.Contains(l.Hint, "http://") {
		t.Fatalf("hint %q", l.Hint)
	}
}
