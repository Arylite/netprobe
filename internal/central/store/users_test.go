package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func TestAddAndReadUsers(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()

	if err := s.AddUser(ctx, "alice", store.RoleAdmin, "hash-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUser(ctx, "bob", store.RoleViewer, "hash-b"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUser(ctx, "alice", store.RoleViewer, "x"); !errors.Is(err, store.ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}

	u, hash, err := s.PasswordHash(ctx, "alice")
	if err != nil || u.Role != store.RoleAdmin || hash != "hash-a" {
		t.Fatalf("PasswordHash() = %+v, %q, %v", u, hash, err)
	}
	if _, _, err := s.PasswordHash(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	users, err := s.ListUsers(ctx)
	if err != nil || len(users) != 2 || users[0].Username != "alice" || users[1].Role != store.RoleViewer {
		t.Fatalf("ListUsers() = %+v, %v", users, err)
	}
}

func TestAddUserValidates(t *testing.T) {
	s := storetest.Open(t)
	for _, name := range []string{"", "Alice", "-a", "a b", strings.Repeat("a", 65), "a/b"} {
		if err := s.AddUser(context.Background(), name, store.RoleViewer, "h"); err == nil {
			t.Errorf("AddUser(%q) succeeded", name)
		}
	}
	if err := s.AddUser(context.Background(), "alice", "root", "h"); err == nil {
		t.Fatal("an unknown role was accepted")
	}
}

func TestTheLastAdministratorCannotBeDeleted(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	_ = s.AddUser(ctx, "alice", store.RoleAdmin, "h")
	_ = s.AddUser(ctx, "bob", store.RoleViewer, "h")

	if err := s.DeleteUser(ctx, "alice"); !errors.Is(err, store.ErrLastAdmin) {
		t.Fatalf("last admin: %v", err)
	}
	if err := s.DeleteUser(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, "bob"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	_ = s.AddUser(ctx, "carol", store.RoleAdmin, "h")
	if err := s.DeleteUser(ctx, "alice"); err != nil {
		t.Fatalf("with another admin: %v", err)
	}
}

func TestSessions(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	_ = s.AddUser(ctx, "alice", store.RoleAdmin, "h")

	token, expires, err := s.CreateSession(ctx, "alice", time.Hour)
	if err != nil || !strings.HasPrefix(token, "ns_") || time.Until(expires) < 59*time.Minute {
		t.Fatalf("CreateSession() = %q, %v, %v", token, expires, err)
	}
	u, ok, err := s.AuthenticateSession(ctx, token)
	if err != nil || !ok || u.Username != "alice" || u.Role != store.RoleAdmin {
		t.Fatalf("AuthenticateSession() = %+v, %v, %v", u, ok, err)
	}
	for _, bad := range []string{"", "ns_wrong", token + "x", "np_" + strings.TrimPrefix(token, "ns_")} {
		if _, ok, err := s.AuthenticateSession(ctx, bad); ok || err != nil {
			t.Errorf("AuthenticateSession(%q) = ok %v, err %v", bad, ok, err)
		}
	}

	if err := s.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.AuthenticateSession(ctx, token); ok {
		t.Fatal("a deleted session still authenticates")
	}
	if err := s.DeleteSession(ctx, "ns_unknown"); err != nil {
		t.Fatalf("deleting an unknown session: %v", err)
	}
}

func TestExpiredSessionsAreRefusedAndPurged(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	_ = s.AddUser(ctx, "alice", store.RoleAdmin, "h")
	expired, _, _ := s.CreateSession(ctx, "alice", -time.Minute)
	live, _, _ := s.CreateSession(ctx, "alice", time.Hour)

	if _, ok, _ := s.AuthenticateSession(ctx, expired); ok {
		t.Fatal("an expired session authenticated")
	}
	if n, err := s.PurgeSessions(ctx); err != nil || n != 1 {
		t.Fatalf("PurgeSessions() = %d, %v", n, err)
	}
	if _, ok, _ := s.AuthenticateSession(ctx, live); !ok {
		t.Fatal("a live session was purged")
	}
}

func TestChangingAPasswordEndsTheSessions(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	_ = s.AddUser(ctx, "alice", store.RoleAdmin, "old")
	token, _, _ := s.CreateSession(ctx, "alice", time.Hour)

	if err := s.SetPasswordHash(ctx, "alice", "new"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.AuthenticateSession(ctx, token); ok {
		t.Fatal("a session survived the password change")
	}
	if _, hash, _ := s.PasswordHash(ctx, "alice"); hash != "new" {
		t.Fatalf("hash %q", hash)
	}
	if err := s.SetPasswordHash(ctx, "nobody", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestDeletingAUserEndsItsSessions(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	_ = s.AddUser(ctx, "alice", store.RoleAdmin, "h")
	_ = s.AddUser(ctx, "bob", store.RoleViewer, "h")
	token, _, _ := s.CreateSession(ctx, "bob", time.Hour)
	if err := s.DeleteUser(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.AuthenticateSession(ctx, token); ok {
		t.Fatal("a session outlived its user")
	}
}

func TestFirstAdminIsCreatedOnlyWhileThereIsNoAccount(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()

	if has, err := s.HasUsers(ctx); err != nil || has {
		t.Fatalf("an empty database: %v, %v", has, err)
	}
	if err := s.AddFirstAdmin(ctx, "Not A Name", "$argon2id$x"); err == nil {
		t.Fatal("an invalid username was accepted")
	}
	if err := s.AddFirstAdmin(ctx, "alice", "$argon2id$x"); err != nil {
		t.Fatal(err)
	}
	if has, _ := s.HasUsers(ctx); !has {
		t.Fatal("the first administrator is not there")
	}
	if err := s.AddFirstAdmin(ctx, "bob", "$argon2id$x"); !errors.Is(err, store.ErrSetupDone) {
		t.Fatalf("a second first administrator: %v", err)
	}
	users, _ := s.ListUsers(ctx)
	if len(users) != 1 || users[0].Username != "alice" || users[0].Role != store.RoleAdmin {
		t.Fatalf("users: %+v", users)
	}
}

func TestAnAccountKeepsAtMostTwentySessions(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	if err := s.AddUser(ctx, "alice", store.RoleViewer, "$argon2id$x"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUser(ctx, "bob", store.RoleViewer, "$argon2id$x"); err != nil {
		t.Fatal(err)
	}
	bob, _, _ := s.CreateSession(ctx, "bob", time.Hour)

	var tokens []string
	for range 25 {
		token, _, err := s.CreateSession(ctx, "alice", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	live := 0
	for i, token := range tokens {
		_, ok, err := s.AuthenticateSession(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			live++
		}
		if (i < 5) == ok {
			t.Errorf("session %d: live=%v", i, ok)
		}
	}
	if live != 20 {
		t.Fatalf("%d live sessions", live)
	}
	// Another account is not touched.
	if _, ok, _ := s.AuthenticateSession(ctx, bob); !ok {
		t.Fatal("the session of another account was ended")
	}
}
