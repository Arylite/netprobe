package registry

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func TestAddAuthenticateAndHashing(t *testing.T) {
	r, dir := open(t)
	e, token, err := r.Add("paris")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, tokenPrefix) || e.Name != "paris" || e.ID == "" {
		t.Fatalf("edge %+v token %q", e, token)
	}
	got, ok := r.Authenticate(token)
	if !ok || got.ID != e.ID {
		t.Fatalf("Authenticate() = %+v, %v", got, ok)
	}
	if _, ok := r.Authenticate(token + "x"); ok {
		t.Fatal("accepted a wrong token")
	}
	if _, ok := r.Authenticate(""); ok {
		t.Fatal("accepted an empty token")
	}
	file := filepath.Join(dir, registryFile)
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("the token is stored in clear")
	}
	if info, _ := os.Stat(file); runtime.GOOS != "windows" && info.Mode().Perm() != fileMode {
		t.Fatalf("file mode %v", info.Mode().Perm())
	}
}

func TestAddValidatesNames(t *testing.T) {
	r, _ := open(t)
	for _, bad := range []string{"", "Paris", "-a", "a b", strings.Repeat("a", 64), "a/b"} {
		if _, _, err := r.Add(bad); err == nil {
			t.Errorf("Add(%q) succeeded", bad)
		}
	}
	if _, _, err := r.Add("paris"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Add("paris"); err == nil {
		t.Fatal("accepted a duplicate name")
	}
}

func TestRevokeFreesTheNameAndStopsTheToken(t *testing.T) {
	r, _ := open(t)
	_, token, _ := r.Add("paris")
	if err := r.Revoke("paris"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Authenticate(token); ok {
		t.Fatal("a revoked token still authenticates")
	}
	if err := r.Revoke("paris"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	if _, _, err := r.Add("paris"); err != nil {
		t.Fatalf("the name was not freed: %v", err)
	}
	list, _ := r.List()
	if len(list) != 2 || list[0].Active() || !list[1].Active() {
		t.Fatalf("list %+v", list)
	}
}

func TestChangesFromAnotherProcessAreSeen(t *testing.T) {
	server, dir := open(t)
	now := time.Now()
	server.now = func() time.Time { return now }

	cli, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := cli.Add("paris")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := server.Authenticate(token); ok {
		t.Fatal("seen before the reload delay")
	}
	now = now.Add(2 * reloadEvery)
	if _, ok := server.Authenticate(token); !ok {
		t.Fatal("not seen after the reload delay")
	}
	if err := cli.Revoke("paris"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * reloadEvery)
	if _, ok := server.Authenticate(token); ok {
		t.Fatal("a revocation by another process was not seen")
	}
}

func TestOpenRejectsACorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, registryFile), []byte("nope"), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("opened a corrupt registry")
	}
}
