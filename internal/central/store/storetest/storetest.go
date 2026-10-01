package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Arylite/netprobe/internal/central/store"
)

// Open creates a throwaway database next to the one named by
// NETPROBE_TEST_DATABASE_URL, opens a store on it and drops it when the test
// ends. Without the variable the test is skipped.
func Open(t *testing.T) *store.Store {
	t.Helper()
	s, _ := OpenURL(t)
	return s
}

// OpenURL is Open, and also returns the URL of the throwaway database.
func OpenURL(t *testing.T) (*store.Store, string) {
	t.Helper()
	url := EmptyDatabase(t)
	s, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, url
}

// EmptyDatabase creates a throwaway database with nothing in it, and returns
// its URL. It is dropped when the test ends.
func EmptyDatabase(t *testing.T) string {
	t.Helper()
	base := os.Getenv("NETPROBE_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("NETPROBE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "netprobe_test_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}
