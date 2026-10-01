package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arylite/netprobe/internal/api"
)

func stubCentral(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+api.PathHealth, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET "+api.PathAssignments, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer np_good" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(api.Assignments{Checks: []api.Check{}})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestDoctorCommandReportsAHealthySetup(t *testing.T) {
	ts := stubCentral(t)
	t.Setenv("NETPROBE_TOKEN", "np_good")
	var out bytes.Buffer
	if err := doctorCommand([]string{"--central", ts.URL}, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "token accepted") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestDoctorCommandFailsWithAHint(t *testing.T) {
	ts := stubCentral(t)
	t.Setenv("NETPROBE_TOKEN", "np_revoked")
	var out bytes.Buffer
	err := doctorCommand([]string{"--central", ts.URL}, &out)
	if !errors.Is(err, errChecksFailed) || !strings.Contains(out.String(), "->") {
		t.Fatalf("err %v, output:\n%s", err, out.String())
	}
}

func TestDoctorCommandWithoutAToken(t *testing.T) {
	t.Setenv("NETPROBE_TOKEN", "")
	var out bytes.Buffer
	err := doctorCommand([]string{"--central", "http://127.0.0.1:1"}, &out)
	if !errors.Is(err, errChecksFailed) || !strings.Contains(out.String(), "no token") {
		t.Fatalf("err %v, output:\n%s", err, out.String())
	}
}

func TestDoctorCommandReadsTheTokenFile(t *testing.T) {
	ts := stubCentral(t)
	t.Setenv("NETPROBE_TOKEN", "")
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("np_good\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := doctorCommand([]string{"--central", ts.URL, "--token-file", file}, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if err := doctorCommand([]string{"--central", ts.URL, "--token-file", filepath.Join(t.TempDir(), "missing")}, &out); err == nil || errors.Is(err, errChecksFailed) {
		t.Fatalf("an unreadable token file must be its own error: %v", err)
	}
}
