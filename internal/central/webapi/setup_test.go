package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/Arylite/netprobe/internal/central/auth"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

// emptyFixture is a central with no account, behind a proxy on this machine.
func emptyFixture(t *testing.T) *fixture {
	t.Helper()
	st := storetest.Open(t)
	proxies, err := auth.ParseProxies([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(quiet, st, Config{TrustedProxies: proxies})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &fixture{store: st, srv: srv, url: ts.URL}
}

func TestTheSetupCodeIsSixDigits(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		code, err := newSetupCode()
		if err != nil || !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
			t.Fatalf("code %q: %v", code, err)
		}
		seen[code] = true
	}
	if len(seen) < 40 {
		t.Fatalf("only %d different codes in 50", len(seen))
	}
}

func TestSetupIsRequiredUntilTheFirstAccountExists(t *testing.T) {
	f := emptyFixture(t)
	required := func() bool {
		res, raw := f.do(t, http.MethodGet, "/api/v1/setup", "", nil, nil)
		status(t, res, raw, http.StatusOK)
		var s setupStatus
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		return s.Required
	}
	if !required() {
		t.Fatal("a central without account does not ask for the setup")
	}
	if err := f.store.AddUser(context.Background(), "alice", store.RoleViewer, "$argon2id$x"); err != nil {
		t.Fatal(err)
	}
	if required() {
		t.Fatal("the setup is still asked for although an account exists")
	}
}

func TestSetupCreatesTheFirstAdministratorAndSignsThemIn(t *testing.T) {
	f := emptyFixture(t)
	req := setupRequest{Code: f.srv.SetupCode(), Username: "alice", Password: "an administrator password"}

	res, raw := f.do(t, http.MethodPost, "/api/v1/setup", "", req, nil)
	status(t, res, raw, http.StatusCreated)
	var session loginResponse
	if err := json.Unmarshal(raw, &session); err != nil || session.Token == "" || session.User.Role != store.RoleAdmin || session.User.Username != "alice" {
		t.Fatalf("session: %s (%v)", raw, err)
	}
	// The session works, and so does the password.
	if res, raw := f.do(t, http.MethodGet, "/api/v1/me", session.Token, nil, nil); res.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"role":"admin"`) {
		t.Fatalf("me: %d %s", res.StatusCode, raw)
	}
	f.login(t, "alice", "an administrator password")

	// It works once.
	res, raw = f.do(t, http.MethodPost, "/api/v1/setup", "", setupRequest{Code: f.srv.SetupCode(), Username: "mallory", Password: "another long password"}, nil)
	status(t, res, raw, http.StatusConflict)
	if users, _ := f.store.ListUsers(context.Background()); len(users) != 1 {
		t.Fatalf("users: %+v", users)
	}
}

func TestSetupAcceptsTheCodeAsPeopleWriteIt(t *testing.T) {
	f := emptyFixture(t)
	code := f.srv.SetupCode()
	spaced := " " + code[:3] + " " + code[3:] + " "
	res, raw := f.do(t, http.MethodPost, "/api/v1/setup", "", setupRequest{Code: spaced, Username: "alice", Password: "an administrator password"}, nil)
	status(t, res, raw, http.StatusCreated)
}

func TestSetupRefusesAWrongCodeAndCreatesNothing(t *testing.T) {
	f := emptyFixture(t)
	wrong := "000000"
	if f.srv.SetupCode() == wrong {
		wrong = "000001"
	}
	for _, code := range []string{wrong, "", "12345", f.srv.SetupCode() + "0"} {
		res, raw := f.do(t, http.MethodPost, "/api/v1/setup", "", setupRequest{Code: code, Username: "alice", Password: "an administrator password"}, nil)
		status(t, res, raw, http.StatusForbidden)
	}
	if has, _ := f.store.HasUsers(context.Background()); has {
		t.Fatal("an account was created with a wrong code")
	}
}

func TestSetupChecksTheAccountOnlyAfterTheCode(t *testing.T) {
	f := emptyFixture(t)
	for name, body := range map[string]setupRequest{
		"a bad username":       {Code: f.srv.SetupCode(), Username: "Alice Admin", Password: "an administrator password"},
		"a short password":     {Code: f.srv.SetupCode(), Username: "alice", Password: "short"},
		"a guessable password": {Code: f.srv.SetupCode(), Username: "alice", Password: "passwordpassword"},
	} {
		res, raw := f.do(t, http.MethodPost, "/api/v1/setup", "", body, nil)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, res.StatusCode, raw)
		}
	}
	// Refused for what is wrong with the account, the setup stays open.
	if has, _ := f.store.HasUsers(context.Background()); has {
		t.Fatal("an account was created")
	}
}

func TestTenWrongCodesFromAnyoneCloseTheSetupForAWhile(t *testing.T) {
	f := emptyFixture(t)
	wrong := "000000"
	if f.srv.SetupCode() == wrong {
		wrong = "000001"
	}
	// Each attempt from another address: the limit is on the setup, not on one client.
	for i := range maxSetupFailures {
		res, _ := f.do(t, http.MethodPost, "/api/v1/setup", "", setupRequest{Code: wrong, Username: "alice", Password: "an administrator password"}, map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i+1)})
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("attempt %d: %d", i, res.StatusCode)
		}
	}
	// Even the right code is refused now.
	res, raw := f.do(t, http.MethodPost, "/api/v1/setup", "", setupRequest{Code: f.srv.SetupCode(), Username: "alice", Password: "an administrator password"}, nil)
	status(t, res, raw, http.StatusTooManyRequests)
	if res.Header.Get("Retry-After") != "900" {
		t.Fatalf("Retry-After %q", res.Header.Get("Retry-After"))
	}
	if has, _ := f.store.HasUsers(context.Background()); has {
		t.Fatal("an account was created")
	}
}

func TestTwoSetupsAtOnceCreateOneAdministrator(t *testing.T) {
	f := emptyFixture(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for _, name := range []string{"alice", "bob", "carol", "dave", "erin", "frank"} {
		wg.Go(func() {
			res, _ := f.do(t, http.MethodPost, "/api/v1/setup", "", setupRequest{Code: f.srv.SetupCode(), Username: name, Password: "an administrator password " + name}, nil)
			mu.Lock()
			codes[res.StatusCode]++
			mu.Unlock()
		})
	}
	wg.Wait()
	if codes[http.StatusCreated] != 1 {
		t.Fatalf("answers: %v", codes)
	}
	if users, _ := f.store.ListUsers(context.Background()); len(users) != 1 {
		t.Fatalf("users: %+v", users)
	}
}
