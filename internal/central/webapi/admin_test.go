package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Arylite/netprobe/internal/api"
)

func status(t *testing.T, res *http.Response, raw []byte, want int) {
	t.Helper()
	if res.StatusCode != want {
		t.Fatalf("status %d, want %d: %s", res.StatusCode, want, raw)
	}
}

func TestOnlyAdministratorsMayChangeThings(t *testing.T) {
	f := newFixture(t, Config{})
	viewer := f.login(t, "bob", viewerPassword)
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/edges", createEdgeRequest{Name: "paris"}},
		{http.MethodDelete, "/api/v1/edges/paris", nil},
		{http.MethodPost, "/api/v1/checks", api.Check{ID: "web", Kind: api.KindTCP, Target: "a:1", IntervalSeconds: 5}},
		{http.MethodDelete, "/api/v1/checks/web", nil},
		{http.MethodGet, "/api/v1/users", nil},
		{http.MethodPost, "/api/v1/users", createUserRequest{Username: "carol", Role: "viewer", Password: "a long enough password"}},
		{http.MethodDelete, "/api/v1/users/alice", nil},
	} {
		res, raw := f.do(t, call.method, call.path, viewer, call.body, nil)
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a viewer: status %d (%s), want 403", call.method, call.path, res.StatusCode, raw)
		}
	}
	if edges, _ := f.store.ListEdges(context.Background()); len(edges) != 0 {
		t.Fatal("a viewer changed something")
	}
}

func TestAdministratorsManageEdges(t *testing.T) {
	f := newFixture(t, Config{})
	admin := f.login(t, "alice", adminPassword)

	res, raw := f.do(t, http.MethodPost, "/api/v1/edges", admin, createEdgeRequest{Name: "paris"}, nil)
	status(t, res, raw, http.StatusCreated)
	var created createEdgeResponse
	if err := json.Unmarshal(raw, &created); err != nil || !strings.HasPrefix(created.Token, "np_") || created.Name != "paris" {
		t.Fatalf("created: %s", raw)
	}
	if _, ok, _ := f.store.AuthenticateEdge(context.Background(), created.Token); !ok {
		t.Fatal("the token returned does not authenticate the edge")
	}

	res, raw = f.do(t, http.MethodPost, "/api/v1/edges", admin, createEdgeRequest{Name: "paris"}, nil)
	status(t, res, raw, http.StatusConflict)
	res, raw = f.do(t, http.MethodPost, "/api/v1/edges", admin, createEdgeRequest{Name: "Not Valid"}, nil)
	status(t, res, raw, http.StatusBadRequest)

	_, listed := f.do(t, http.MethodGet, "/api/v1/edges", admin, nil, nil)
	if strings.Contains(string(listed), created.Token) {
		t.Fatal("the token is shown again after creation")
	}

	res, raw = f.do(t, http.MethodDelete, "/api/v1/edges/paris", admin, nil, nil)
	status(t, res, raw, http.StatusNoContent)
	if _, ok, _ := f.store.AuthenticateEdge(context.Background(), created.Token); ok {
		t.Fatal("a revoked edge still authenticates")
	}
	res, raw = f.do(t, http.MethodDelete, "/api/v1/edges/paris", admin, nil, nil)
	status(t, res, raw, http.StatusNotFound)
}

func TestAdministratorsManageChecks(t *testing.T) {
	f := newFixture(t, Config{})
	admin := f.login(t, "alice", adminPassword)
	check := api.Check{ID: "web", Kind: api.KindHTTP, Target: "https://example.com", Expect: "200;contains:ok", IntervalSeconds: 30}

	res, raw := f.do(t, http.MethodPost, "/api/v1/checks", admin, check, nil)
	status(t, res, raw, http.StatusCreated)
	if stored, _ := f.store.ListChecks(context.Background()); len(stored) != 1 || stored[0] != check {
		t.Fatalf("stored %+v", stored)
	}
	res, raw = f.do(t, http.MethodPost, "/api/v1/checks", admin, check, nil)
	status(t, res, raw, http.StatusConflict)
	for name, bad := range map[string]any{
		"unknown kind": api.Check{ID: "x", Kind: "carrier-pigeon", Target: "a", IntervalSeconds: 5},
		"bad target":   api.Check{ID: "x", Kind: api.KindTCP, Target: "no-port", IntervalSeconds: 5},
		"bad expect":   api.Check{ID: "x", Kind: api.KindTLS, Target: "example.com", Expect: "soon", IntervalSeconds: 60},
		"too frequent": api.Check{ID: "x", Kind: api.KindTLS, Target: "example.com", IntervalSeconds: 5},
		"no interval":  api.Check{ID: "x", Kind: api.KindTCP, Target: "a:1"},
		"extra field":  map[string]any{"id": "x", "kind": "tcp", "target": "a:1", "interval_seconds": 5, "extra": 1},
	} {
		if res, raw := f.do(t, http.MethodPost, "/api/v1/checks", admin, bad, nil); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d (%s), want 400", name, res.StatusCode, raw)
		}
	}

	res, raw = f.do(t, http.MethodDelete, "/api/v1/checks/web", admin, nil, nil)
	status(t, res, raw, http.StatusNoContent)
	res, raw = f.do(t, http.MethodDelete, "/api/v1/checks/web", admin, nil, nil)
	status(t, res, raw, http.StatusNotFound)
}

func TestAdministratorsManageUsers(t *testing.T) {
	f := newFixture(t, Config{})
	admin := f.login(t, "alice", adminPassword)

	res, raw := f.do(t, http.MethodPost, "/api/v1/users", admin, createUserRequest{Username: "carol", Role: "viewer", Password: "carol has a long one"}, nil)
	status(t, res, raw, http.StatusCreated)
	if strings.Contains(string(raw), "password") || strings.Contains(string(raw), "argon2") {
		t.Fatalf("the answer leaks password data: %s", raw)
	}
	f.login(t, "carol", "carol has a long one")

	for name, body := range map[string]createUserRequest{
		"duplicate":      {Username: "carol", Role: "viewer", Password: "carol has a long one"},
		"bad username":   {Username: "Carol B", Role: "viewer", Password: "carol has a long one"},
		"bad role":       {Username: "dave", Role: "root", Password: "dave has a long one"},
		"short password": {Username: "dave", Role: "viewer", Password: "short"},
	} {
		want := http.StatusBadRequest
		if name == "duplicate" {
			want = http.StatusConflict
		}
		if res, raw := f.do(t, http.MethodPost, "/api/v1/users", admin, body, nil); res.StatusCode != want {
			t.Errorf("%s: status %d (%s), want %d", name, res.StatusCode, raw, want)
		}
	}

	_, raw = f.do(t, http.MethodGet, "/api/v1/users", admin, nil, nil)
	var listed struct{ Users []userJSON }
	if err := json.Unmarshal(raw, &listed); err != nil || len(listed.Users) != 3 {
		t.Fatalf("users %s", raw)
	}

	res, raw = f.do(t, http.MethodDelete, "/api/v1/users/carol", admin, nil, nil)
	status(t, res, raw, http.StatusNoContent)
	res, raw = f.do(t, http.MethodDelete, "/api/v1/users/carol", admin, nil, nil)
	status(t, res, raw, http.StatusNotFound)
}

func TestAnAdministratorCannotDeleteThemselvesOrTheLastAdmin(t *testing.T) {
	f := newFixture(t, Config{})
	admin := f.login(t, "alice", adminPassword)
	res, raw := f.do(t, http.MethodDelete, "/api/v1/users/alice", admin, nil, nil)
	status(t, res, raw, http.StatusConflict)

	// A second administrator may delete the first; the last one is protected.
	f.do(t, http.MethodPost, "/api/v1/users", admin, createUserRequest{Username: "carol", Role: "admin", Password: "carol has a long one"}, nil)
	carol := f.login(t, "carol", "carol has a long one")
	res, raw = f.do(t, http.MethodDelete, "/api/v1/users/alice", carol, nil, nil)
	status(t, res, raw, http.StatusNoContent)
	if res, _ := f.do(t, http.MethodGet, "/api/v1/me", admin, nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatal("the deleted administrator kept a working session")
	}
}

func TestChangingYourPassword(t *testing.T) {
	f := newFixture(t, Config{})
	token := f.login(t, "bob", viewerPassword)
	const next = "a brand new password"

	res, raw := f.do(t, http.MethodPost, "/api/v1/me/password", token, passwordRequest{CurrentPassword: "not the password", NewPassword: next}, nil)
	status(t, res, raw, http.StatusForbidden)
	res, raw = f.do(t, http.MethodPost, "/api/v1/me/password", token, passwordRequest{CurrentPassword: viewerPassword, NewPassword: "short"}, nil)
	status(t, res, raw, http.StatusBadRequest)

	res, raw = f.do(t, http.MethodPost, "/api/v1/me/password", token, passwordRequest{CurrentPassword: viewerPassword, NewPassword: next}, nil)
	status(t, res, raw, http.StatusNoContent)

	if res, _ := f.do(t, http.MethodGet, "/api/v1/me", token, nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatal("a session survived the password change")
	}
	if res, _ := f.do(t, http.MethodPost, "/api/v1/login", "", loginRequest{Username: "bob", Password: viewerPassword}, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatal("the old password still works")
	}
	f.login(t, "bob", next)
}

func TestPasswordChangeIsThrottled(t *testing.T) {
	f := newFixture(t, Config{})
	token := f.login(t, "bob", viewerPassword)
	for i := 0; i < maxByUserIP; i++ {
		f.do(t, http.MethodPost, "/api/v1/me/password", token, passwordRequest{CurrentPassword: "guess number one", NewPassword: "a brand new password"}, nil)
	}
	res, raw := f.do(t, http.MethodPost, "/api/v1/me/password", token, passwordRequest{CurrentPassword: viewerPassword, NewPassword: "a brand new password"}, nil)
	status(t, res, raw, http.StatusTooManyRequests)
}
