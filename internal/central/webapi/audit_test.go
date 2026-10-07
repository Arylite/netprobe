package webapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/auth"
)

func auditEvents(t *testing.T, f *fixture, token string) []auditJSON {
	t.Helper()
	res, raw := f.do(t, http.MethodGet, "/api/v1/audit?limit=200", token, nil, nil)
	status(t, res, raw, http.StatusOK)
	var body struct {
		Events []auditJSON `json:"events"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Events == nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return body.Events
}

func has(events []auditJSON, actor, action, target string) bool {
	for _, e := range events {
		if e.Actor == actor && e.Action == action && e.Target == target {
			return true
		}
	}
	return false
}

func TestOnlyAdministratorsReadTheAuditTrail(t *testing.T) {
	f := newFixture(t, Config{})
	if res, _ := f.do(t, http.MethodGet, "/api/v1/audit", "", nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", res.StatusCode)
	}
	viewer := f.login(t, "bob", viewerPassword)
	if res, _ := f.do(t, http.MethodGet, "/api/v1/audit", viewer, nil, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("as a viewer: %d", res.StatusCode)
	}
	admin := f.login(t, "alice", adminPassword)
	for _, bad := range []string{"?limit=0", "?limit=x"} {
		if res, _ := f.do(t, http.MethodGet, "/api/v1/audit"+bad, admin, nil, nil); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, res.StatusCode)
		}
	}
}

func TestSigningInAndOutAreRecorded(t *testing.T) {
	f := newFixture(t, Config{})
	token := f.login(t, "alice", adminPassword)
	if res, _ := f.do(t, http.MethodPost, "/api/v1/logout", token, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", res.StatusCode)
	}
	admin := f.login(t, "alice", adminPassword)

	events := auditEvents(t, f, admin)
	if !has(events, "alice", "login", "") || !has(events, "alice", "logout", "") {
		t.Fatalf("events: %+v", events)
	}
}

func TestARefusedSignInIsRecordedWithoutWhatWasTyped(t *testing.T) {
	f := newFixture(t, Config{})
	f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "alice", Password: "the wrong password"}, nil)
	// A password typed in the username field.
	f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "my-real-password-1", Password: "x"}, nil)
	f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "nobody", Password: "x"}, nil)

	events := auditEvents(t, f, f.login(t, "alice", adminPassword))
	if !has(events, "", "login.failed", "alice") {
		t.Fatalf("the refusal of a real account is not there: %+v", events)
	}
	failed := 0
	for _, e := range events {
		if e.Action == "login.failed" {
			failed++
			if e.Target != "" && e.Target != "alice" {
				t.Errorf("a refusal kept what was typed: %+v", e)
			}
		}
	}
	if failed != 3 {
		t.Fatalf("%d refusals recorded", failed)
	}
}

func TestEveryChangeIsRecordedWithWhoMadeIt(t *testing.T) {
	sender := &fakeSender{}
	f := newFixture(t, Config{Sender: sender})
	admin := f.login(t, "alice", adminPassword)
	do := func(method, path string, body any, want int) {
		t.Helper()
		res, raw := f.do(t, method, path, admin, body, nil)
		status(t, res, raw, want)
	}

	do(http.MethodPost, "/api/v1/edges", createEdgeRequest{Name: "paris"}, http.StatusCreated)
	do(http.MethodDelete, "/api/v1/edges/paris", nil, http.StatusNoContent)
	do(http.MethodPost, "/api/v1/checks", api.Check{ID: "web", Kind: api.KindTCP, Target: "a:1", IntervalSeconds: 5}, http.StatusCreated)
	do(http.MethodDelete, "/api/v1/checks/web", nil, http.StatusNoContent)
	do(http.MethodPost, "/api/v1/channels", createChannelRequest{Name: "ops", URL: "https://example.com/very-secret-path", Secret: "s3cret"}, http.StatusCreated)
	do(http.MethodPost, "/api/v1/channels/ops/test", nil, http.StatusNoContent)
	do(http.MethodDelete, "/api/v1/channels/ops", nil, http.StatusNoContent)
	do(http.MethodPost, "/api/v1/users", createUserRequest{Username: "carol", Role: "viewer", Password: "a long enough password"}, http.StatusCreated)
	do(http.MethodDelete, "/api/v1/users/carol", nil, http.StatusNoContent)

	events := auditEvents(t, f, admin)
	for _, want := range [][2]string{
		{"edge.create", "paris"}, {"edge.revoke", "paris"}, {"check.create", "web"}, {"check.remove", "web"},
		{"channel.create", "ops"}, {"channel.test", "ops"}, {"channel.remove", "ops"}, {"user.create", "carol"}, {"user.delete", "carol"},
	} {
		if !has(events, "alice", want[0], want[1]) {
			t.Errorf("%s %s is not in the trail", want[0], want[1])
		}
	}
	// No secret, address or password is in what is kept.
	raw, _ := json.Marshal(events)
	for _, secret := range []string{"s3cret", "very-secret-path", "a long enough password", adminPassword} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the trail holds %q", secret)
		}
	}
}

func TestAChangeThatFailedIsNotRecorded(t *testing.T) {
	f := newFixture(t, Config{})
	admin := f.login(t, "alice", adminPassword)
	f.do(t, http.MethodDelete, "/api/v1/edges/nobody", admin, nil, nil)
	f.do(t, http.MethodPost, "/api/v1/checks", admin, api.Check{ID: "bad"}, nil)
	for _, e := range auditEvents(t, f, admin) {
		if strings.HasPrefix(e.Action, "edge.") || strings.HasPrefix(e.Action, "check.") {
			t.Fatalf("recorded: %+v", e)
		}
	}
}

func TestThePasswordChangeAndTheSetupAreRecorded(t *testing.T) {
	f := emptyFixture(t)
	res, raw := f.do(t, http.MethodPost, "/api/v1/setup", "", setupRequest{Code: f.srv.SetupCode(), Username: "alice", Password: "an administrator password"}, nil)
	status(t, res, raw, http.StatusCreated)
	var session loginResponse
	_ = json.Unmarshal(raw, &session)
	res, raw = f.do(t, http.MethodPost, "/api/v1/me/password", session.Token, passwordRequest{CurrentPassword: "an administrator password", NewPassword: "another administrator password"}, nil)
	status(t, res, raw, http.StatusNoContent)

	admin := f.login(t, "alice", "another administrator password")
	events := auditEvents(t, f, admin)
	if !has(events, "alice", "setup", "") || !has(events, "alice", "password.change", "") {
		t.Fatalf("events: %+v", events)
	}
}

func TestTheAddressRecordedIsTheClientBehindATrustedProxy(t *testing.T) {
	proxies, err := auth.ParseProxies([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, Config{TrustedProxies: proxies})
	f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "alice", Password: adminPassword}, map[string]string{"X-Forwarded-For": "198.51.100.77"})
	for _, e := range auditEvents(t, f, f.login(t, "alice", adminPassword)) {
		if e.Action == "login" && e.ClientIP == "198.51.100.77" {
			return
		}
	}
	t.Fatal("the address of the client is not in the trail")
}
