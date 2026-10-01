package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Arylite/netprobe/internal/central/auth"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func withStdin(t *testing.T, input string) {
	t.Helper()
	old := stdin
	stdin = strings.NewReader(input)
	t.Cleanup(func() { stdin = old })
}

func TestUserAddListPasswdDelete(t *testing.T) {
	st, url := storetest.OpenURL(t)
	ctx := context.Background()

	withStdin(t, "a long enough password\n")
	out, err := runCLI(t, "user", "add", "--database-url", url, "--username", "alice", "--role", "admin")
	if err != nil || !strings.Contains(out, "alice") {
		t.Fatalf("add: %q, %v", out, err)
	}
	user, hash, err := st.PasswordHash(ctx, "alice")
	if err != nil || user.Role != store.RoleAdmin || strings.Contains(hash, "a long enough password") {
		t.Fatalf("stored %+v %q %v", user, hash, err)
	}
	if ok, err := auth.VerifyPassword("a long enough password", hash); !ok || err != nil {
		t.Fatalf("the stored hash does not verify the password: %v, %v", ok, err)
	}

	withStdin(t, "another long password\n")
	if _, err := runCLI(t, "user", "add", "--database-url", url, "--username", "bob", "--role", "viewer"); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "user", "list", "--database-url", url)
	if err != nil || !strings.Contains(out, "alice") || !strings.Contains(out, "viewer") || strings.Contains(out, "argon2") {
		t.Fatalf("list: %q, %v", out, err)
	}

	withStdin(t, "a brand new password\r\n")
	if _, err := runCLI(t, "user", "passwd", "--database-url", url, "--username", "bob"); err != nil {
		t.Fatal(err)
	}
	_, hash, _ = st.PasswordHash(ctx, "bob")
	if ok, _ := auth.VerifyPassword("a brand new password", hash); !ok {
		t.Fatal("the password was not changed, or kept its carriage return")
	}

	if _, err := runCLI(t, "user", "delete", "--database-url", url, "--username", "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "user", "delete", "--database-url", url, "--username", "alice"); !errors.Is(err, store.ErrLastAdmin) {
		t.Fatalf("deleting the last administrator: %v", err)
	}
}

func TestUserCommandsRefuseBadInput(t *testing.T) {
	_, url := storetest.OpenURL(t)
	t.Setenv("NETPROBE_DATABASE_URL", "")
	for name, tc := range map[string]struct {
		input string
		args  []string
	}{
		"no username":    {"a long enough password\n", []string{"user", "add", "--database-url", url, "--role", "viewer"}},
		"no role":        {"a long enough password\n", []string{"user", "add", "--database-url", url, "--username", "alice"}},
		"bad role":       {"a long enough password\n", []string{"user", "add", "--database-url", url, "--username", "alice", "--role", "root"}},
		"bad username":   {"a long enough password\n", []string{"user", "add", "--database-url", url, "--username", "Alice B", "--role", "viewer"}},
		"short password": {"short\n", []string{"user", "add", "--database-url", url, "--username", "alice", "--role", "viewer"}},
		"no password":    {"", []string{"user", "add", "--database-url", url, "--username", "alice", "--role", "viewer"}},
		"empty line":     {"\n", []string{"user", "add", "--database-url", url, "--username", "alice", "--role", "viewer"}},
		"unknown user":   {"a long enough password\n", []string{"user", "passwd", "--database-url", url, "--username", "nobody"}},
		"unknown":        {"", []string{"user", "frobnicate"}},
		"no action":      {"", []string{"user"}},
	} {
		withStdin(t, tc.input)
		if _, err := runCLI(t, tc.args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if users, _ := storeUsers(t, url); len(users) != 0 {
		t.Fatalf("a refused command created %d users", len(users))
	}
}

func storeUsers(t *testing.T, url string) ([]store.User, error) {
	t.Helper()
	st, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	return st.ListUsers(context.Background())
}

func TestReadPasswordOnlyTakesTheFirstLineAndABoundedAmount(t *testing.T) {
	if got, err := readPassword(strings.NewReader("first\nsecond\n")); err != nil || got != "first" {
		t.Fatalf("readPassword() = %q, %v", got, err)
	}
	if got, _ := readPassword(strings.NewReader(strings.Repeat("a", 10_000))); len(got) > auth.MaxPasswordLength+2 {
		t.Fatalf("read %d bytes, the input must be bounded", len(got))
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" https://a.example.com, ,http://localhost:5173,")
	if len(got) != 2 || got[0] != "https://a.example.com" || got[1] != "http://localhost:5173" {
		t.Fatalf("splitList() = %q", got)
	}
	if splitList("") != nil {
		t.Fatal("an empty list must be nil")
	}
}

func TestServeRefusesSharedOrMissingAddresses(t *testing.T) {
	for name, cfg := range map[string]serveConfig{
		"same address":    {edgeListen: "127.0.0.1:8080", apiListen: "127.0.0.1:8080", level: "info"},
		"no edge address": {apiListen: "127.0.0.1:8081", level: "info"},
		"no api address":  {edgeListen: "127.0.0.1:8080", level: "info"},
	} {
		if err := run(cfg); err == nil {
			t.Errorf("%s: run accepted the settings", name)
		}
	}
}
