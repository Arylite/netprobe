package webapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Arylite/netprobe/internal/central/auth"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

const (
	adminPassword  = "an administrator password"
	viewerPassword = "a viewer password here"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fixture struct {
	store *store.Store
	srv   *Server
	url   string
}

func newFixture(t *testing.T, cfg Config) *fixture {
	t.Helper()
	st := storetest.Open(t)
	ctx := context.Background()
	for _, u := range []struct{ name, role, password string }{
		{"alice", store.RoleAdmin, adminPassword},
		{"bob", store.RoleViewer, viewerPassword},
	} {
		hash, err := auth.HashPassword(u.password)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddUser(ctx, u.name, u.role, hash); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := New(quiet, st, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &fixture{store: st, srv: srv, url: ts.URL}
}

func (f *fixture) do(t *testing.T, method, path, token string, body any, header map[string]string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, f.url+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res, raw
}

// login returns the session token of a user.
func (f *fixture) login(t *testing.T, username, password string) string {
	t.Helper()
	res, raw := f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: username, Password: password}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login %s: %d %s", username, res.StatusCode, raw)
	}
	var out loginResponse
	if err := json.Unmarshal(raw, &out); err != nil || out.Token == "" {
		t.Fatalf("login response %s: %v", raw, err)
	}
	return out.Token
}

func TestLoginMeAndLogout(t *testing.T) {
	f := newFixture(t, Config{})
	token := f.login(t, "alice", adminPassword)

	res, raw := f.do(t, http.MethodGet, "/api/v1/me", token, nil, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"username":"alice"`) || !strings.Contains(string(raw), `"role":"admin"`) {
		t.Fatalf("me: %d %s", res.StatusCode, raw)
	}
	if res, _ := f.do(t, http.MethodPost, "/api/v1/logout", token, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", res.StatusCode)
	}
	if res, _ := f.do(t, http.MethodGet, "/api/v1/me", token, nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", res.StatusCode)
	}
}

func TestLoginFailuresAllLookTheSame(t *testing.T) {
	f := newFixture(t, Config{})
	var bodies []string
	for _, c := range []loginRequest{
		{Username: "alice", Password: "the wrong password"},
		{Username: "nobody", Password: adminPassword},
	} {
		res, raw := f.do(t, http.MethodPost, "/api/v1/login", "", c, nil)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status %d", c.Username, res.StatusCode)
		}
		bodies = append(bodies, string(raw))
	}
	if bodies[0] != bodies[1] {
		t.Fatalf("the answer tells which usernames exist: %q vs %q", bodies[0], bodies[1])
	}
}

func TestLoginRefusesBadRequests(t *testing.T) {
	f := newFixture(t, Config{})
	for name, body := range map[string]any{
		"no username":   loginRequest{Password: adminPassword},
		"long username": loginRequest{Username: strings.Repeat("a", 65), Password: adminPassword},
		"long password": loginRequest{Username: "alice", Password: strings.Repeat("a", 129)},
		"unknown field": map[string]string{"username": "alice", "password": adminPassword, "extra": "x"},
	} {
		if res, _ := f.do(t, http.MethodPost, "/api/v1/login", "", body, nil); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, res.StatusCode)
		}
	}
}

func TestLoginIsThrottledEvenForTheRightPassword(t *testing.T) {
	f := newFixture(t, Config{})
	for i := 0; i < maxByUserIP; i++ {
		f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "alice", Password: "guess number one"}, nil)
	}
	res, _ := f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "alice", Password: adminPassword}, nil)
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Fatalf("status %d, Retry-After %q", res.StatusCode, res.Header.Get("Retry-After"))
	}
	// Another account from the same address is not locked by that.
	if res, _ := f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "bob", Password: viewerPassword}, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("another user: status %d", res.StatusCode)
	}
}

func TestASuccessfulLoginDoesNotResetTheAddressLimit(t *testing.T) {
	f := newFixture(t, Config{})
	for i := 0; i < maxByIP-1; i++ {
		f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "user" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Password: "guess number one"}, nil)
	}
	f.login(t, "bob", viewerPassword) // a success of an account of one's own
	f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "zed", Password: "guess number one"}, nil)
	if res, _ := f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "alice", Password: adminPassword}, nil); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status %d: a success reset the limit of the address", res.StatusCode)
	}
}

func TestProtectedRoutesNeedAValidToken(t *testing.T) {
	f := newFixture(t, Config{})
	for name, token := range map[string]string{"none": "", "unknown": "ns_unknown", "an edge token": "np_something"} {
		res, _ := f.do(t, http.MethodGet, "/api/v1/me", token, nil, nil)
		if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("%s: status %d", name, res.StatusCode)
		}
	}
}

func TestRolesAreEnforced(t *testing.T) {
	f := newFixture(t, Config{})
	admin := f.srv.require(store.RoleAdmin, func(w http.ResponseWriter, _ *http.Request, _ caller) { w.WriteHeader(http.StatusNoContent) })
	handler := httptest.NewServer(admin)
	defer handler.Close()

	for user, password := range map[string]string{"alice": adminPassword, "bob": viewerPassword} {
		token := f.login(t, user, password)
		req, _ := http.NewRequest(http.MethodGet, handler.URL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		want := http.StatusForbidden
		if user == "alice" {
			want = http.StatusNoContent
		}
		if res.StatusCode != want {
			t.Errorf("%s: status %d, want %d", user, res.StatusCode, want)
		}
	}
}

func TestCORS(t *testing.T) {
	f := newFixture(t, Config{AllowedOrigins: []string{"https://ui.example.com"}})

	res, _ := f.do(t, http.MethodGet, "/api/v1/me", "", nil, map[string]string{"Origin": "https://ui.example.com"})
	if res.Header.Get("Access-Control-Allow-Origin") != "https://ui.example.com" || res.Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("allowed origin: %v", res.Header)
	}
	if !strings.Contains(res.Header.Get("Vary"), "Origin") {
		t.Fatal("answers differ by origin but Vary does not say so")
	}

	res, _ = f.do(t, http.MethodGet, "/api/v1/me", "", nil, map[string]string{"Origin": "https://evil.example.com"})
	if res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("an origin that is not allowed got CORS headers")
	}

	preflight := map[string]string{"Origin": "https://ui.example.com", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization"}
	res, _ = f.do(t, http.MethodOptions, "/api/v1/login", "", nil, preflight)
	if res.StatusCode != http.StatusNoContent || !strings.Contains(res.Header.Get("Access-Control-Allow-Headers"), "Authorization") || res.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Fatalf("preflight: %d %v", res.StatusCode, res.Header)
	}
	preflight["Origin"] = "https://evil.example.com"
	res, _ = f.do(t, http.MethodOptions, "/api/v1/login", "", nil, preflight)
	if res.Header.Get("Access-Control-Allow-Origin") != "" || res.Header.Get("Access-Control-Allow-Methods") != "" {
		t.Fatalf("preflight of a foreign origin: %v", res.Header)
	}
}

func TestNoOriginIsAllowedByDefault(t *testing.T) {
	f := newFixture(t, Config{})
	res, _ := f.do(t, http.MethodGet, "/api/v1/me", "", nil, map[string]string{"Origin": "https://ui.example.com"})
	if res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("a browser origin was allowed without being named")
	}
}

func TestParseOrigins(t *testing.T) {
	got, err := ParseOrigins([]string{"https://ui.example.com", " http://localhost:5173 "})
	if err != nil || len(got) != 2 || got[1] != "http://localhost:5173" {
		t.Fatalf("ParseOrigins() = %v, %v", got, err)
	}
	for _, bad := range []string{"*", "", "ui.example.com", "ftp://x", "https://x/path", "https://x?y=1", "https://u:p@x", "https://"} {
		if _, err := ParseOrigins([]string{bad}); err == nil {
			t.Errorf("ParseOrigins(%q) succeeded", bad)
		}
	}
	if _, err := New(quiet, nil, Config{AllowedOrigins: []string{"*"}}); err == nil {
		t.Fatal("New accepted a wildcard origin")
	}
}

func TestAnswersAreNeverCachedOrSniffed(t *testing.T) {
	f := newFixture(t, Config{})
	res, _ := f.do(t, http.MethodGet, "/api/v1/me", "", nil, nil)
	if res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers %v", res.Header)
	}
}

func TestADatabaseFailureIsA503NotA401(t *testing.T) {
	f := newFixture(t, Config{})
	token := f.login(t, "alice", adminPassword)
	f.store.Close()
	if res, _ := f.do(t, http.MethodGet, "/api/v1/me", token, nil, nil); res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("me: status %d, want 503", res.StatusCode)
	}
	if res, _ := f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "alice", Password: adminPassword}, nil); res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("login: status %d, want 503", res.StatusCode)
	}
}

func TestAnswersCarryTheSecurityHeaders(t *testing.T) {
	for _, hsts := range []bool{false, true} {
		f := newFixture(t, Config{HSTS: hsts})
		res, _ := f.do(t, http.MethodGet, "/api/v1/me", "", nil, nil) // even a refusal
		for header, want := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"Cache-Control":           "no-store",
			"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
			"Referrer-Policy":         "no-referrer",
		} {
			if got := res.Header.Get(header); got != want {
				t.Errorf("%s = %q, want %q", header, got, want)
			}
		}
		if got := res.Header.Get("Strict-Transport-Security"); (got != "") != hsts {
			t.Errorf("HSTS %v: header %q", hsts, got)
		}
	}
}
