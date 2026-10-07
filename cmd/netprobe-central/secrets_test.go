package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func keyFile(t *testing.T) string {
	t.Helper()
	out, err := runCLI(t, "secret-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ParseKey(out); err != nil {
		t.Fatalf("the key printed is not a key: %q: %v", out, err)
	}
	file := filepath.Join(t.TempDir(), "secret_key")
	if err := os.WriteFile(file, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestSecretKeysAreDifferentEachTime(t *testing.T) {
	a, _ := runCLI(t, "secret-key")
	b, _ := runCLI(t, "secret-key")
	if a == b || strings.TrimSpace(a) == "" {
		t.Fatalf("%q and %q", a, b)
	}
}

func TestChannelsAreEncryptedWhenThereIsAKey(t *testing.T) {
	_, url := storetest.OpenURL(t)
	t.Setenv("NETPROBE_SECRET_KEY_FILE", keyFile(t))

	withStdin(t, "the secret\n")
	if _, err := runCLI(t, "channel", "add", "--database-url", url, "--name", "ops", "--url", "https://hooks.example.com/very-secret-path", "--secret-stdin"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var address, secret string
	if err := conn.QueryRow(ctx, `SELECT url, secret FROM channels`).Scan(&address, &secret); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(address, "secret-path") || strings.Contains(secret, "the secret") {
		t.Fatalf("the database holds %q and %q", address, secret)
	}
	// Read back with the key, and not without.
	if out, err := runCLI(t, "channel", "list", "--database-url", url); err != nil || !strings.Contains(out, "hooks.example.com") {
		t.Fatalf("list: %q, %v", out, err)
	}
	t.Setenv("NETPROBE_SECRET_KEY_FILE", "")
	if _, err := runCLI(t, "channel", "list", "--database-url", url); err == nil {
		t.Fatal("encrypted channels were read without the key")
	}
}

func TestSecretsEncryptTakesTheChannelsOfBeforeTheKey(t *testing.T) {
	st, url := storetest.OpenURL(t)
	if _, err := st.AddChannel(context.Background(), "old", "https://example.com/old", "secret-old"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "secrets", "encrypt", "--database-url", url); err == nil {
		t.Fatal("encrypted without a key")
	}
	t.Setenv("NETPROBE_SECRET_KEY_FILE", keyFile(t))
	out, err := runCLI(t, "secrets", "encrypt", "--database-url", url)
	if err != nil || !strings.Contains(out, "1 channels encrypted") {
		t.Fatalf("encrypt: %q, %v", out, err)
	}
	if out, _ := runCLI(t, "secrets", "encrypt", "--database-url", url); !strings.Contains(out, "0 channels") {
		t.Fatalf("a second time: %q", out)
	}
	if _, err := runCLI(t, "secrets", "frobnicate"); err == nil {
		t.Fatal("an unknown command was accepted")
	}
}

func TestABadKeyFileIsRefused(t *testing.T) {
	_, url := storetest.OpenURL(t)
	file := filepath.Join(t.TempDir(), "secret_key")
	if err := os.WriteFile(file, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETPROBE_SECRET_KEY_FILE", file)
	if _, err := runCLI(t, "check", "list", "--database-url", url); err == nil {
		t.Fatal("a bad key was accepted")
	}
	t.Setenv("NETPROBE_SECRET_KEY_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := runCLI(t, "check", "list", "--database-url", url); err == nil {
		t.Fatal("a missing key file was ignored")
	}
}
