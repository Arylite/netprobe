package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func newCipher(t *testing.T) *store.Cipher {
	t.Helper()
	text, err := store.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.ParseKey(text)
	if err != nil {
		t.Fatal(err)
	}
	c, err := store.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestKeysAreParsedFromBase64OrHexadecimal(t *testing.T) {
	text, _ := store.GenerateKey()
	a, err := store.ParseKey(" " + text + "\n")
	if err != nil || len(a) != 32 {
		t.Fatalf("base64: %v", err)
	}
	hex := ""
	for _, b := range a {
		hex += string("0123456789abcdef"[b>>4]) + string("0123456789abcdef"[b&15])
	}
	if b, err := store.ParseKey(hex); err != nil || string(a) != string(b) {
		t.Fatalf("hexadecimal: %v", err)
	}
	for _, bad := range []string{"", "short", strings.Repeat("a", 63), "!!!"} {
		if _, err := store.ParseKey(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := store.NewCipher(make([]byte, 16)); err == nil {
		t.Fatal("a key of 16 bytes was accepted")
	}
}

// raw reads a channel as the database holds it.
func raw(t *testing.T, url, name string) (address, secret string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if err := conn.QueryRow(ctx, `SELECT url, secret FROM channels WHERE name = $1`, name).Scan(&address, &secret); err != nil {
		t.Fatal(err)
	}
	return address, secret
}

func TestChannelsAreEncryptedInTheDatabaseAndReadBackInClear(t *testing.T) {
	c := newCipher(t)
	s, url := storetest.OpenURL(t, store.WithCipher(c))
	ctx := context.Background()

	if _, err := s.AddChannel(ctx, "ops", "https://hooks.example.com/T0/very-secret-path", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddChannel(ctx, "plain", "https://example.com/hook", ""); err != nil {
		t.Fatal(err)
	}

	address, secret := raw(t, url, "ops")
	if strings.Contains(address, "very-secret-path") || strings.Contains(secret, "s3cret") || !strings.HasPrefix(address, "enc1:") || !strings.HasPrefix(secret, "enc1:") {
		t.Fatalf("the database holds %q and %q", address, secret)
	}
	if _, secret := raw(t, url, "plain"); secret != "" {
		t.Fatalf("an empty secret was encrypted: %q", secret)
	}

	got, err := s.GetChannel(ctx, "ops")
	if err != nil || got.URL != "https://hooks.example.com/T0/very-secret-path" || got.Secret != "s3cret" {
		t.Fatalf("get: %+v, %v", got, err)
	}
	list, err := s.ListChannels(ctx)
	if err != nil || len(list) != 2 || list[0].Secret != "s3cret" || list[1].URL != "https://example.com/hook" {
		t.Fatalf("list: %+v, %v", list, err)
	}

	// What is sent is read back in clear too.
	edge, _, _ := s.AddEdge(ctx, "paris")
	if _, err := s.OpenIncident(ctx, store.IncidentEdge, "", edge.ID, "silent", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingNotifications(ctx, time.Now().UTC().Add(-time.Hour))
	if err != nil || len(pending) == 0 {
		t.Fatalf("pending: %v, %v", pending, err)
	}
	for _, n := range pending {
		if n.Channel.Name == "ops" && (n.Channel.URL != "https://hooks.example.com/T0/very-secret-path" || n.Channel.Secret != "s3cret") {
			t.Fatalf("a notification is for %+v", n.Channel)
		}
	}
}

func TestAValueCannotBeMovedToAnotherChannel(t *testing.T) {
	c := newCipher(t)
	s, url := storetest.OpenURL(t, store.WithCipher(c))
	ctx := context.Background()
	for _, name := range []string{"a", "b"} {
		if _, err := s.AddChannel(ctx, name, "https://example.com/"+name, "secret-"+name); err != nil {
			t.Fatal(err)
		}
	}
	conn, _ := pgx.Connect(ctx, url)
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `UPDATE channels SET url = (SELECT url FROM channels WHERE name = 'a') WHERE name = 'b'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetChannel(ctx, "b"); err == nil || !strings.Contains(err.Error(), "does not open") {
		t.Fatalf("a moved value was read: %v", err)
	}
}

func TestEncryptedChannelsNeedTheKeyAndTheRightOne(t *testing.T) {
	c := newCipher(t)
	s, url := storetest.OpenURL(t, store.WithCipher(c))
	ctx := context.Background()
	if _, err := s.AddChannel(ctx, "ops", "https://example.com/hook", "s3cret"); err != nil {
		t.Fatal(err)
	}

	for name, opts := range map[string][]store.Option{"no key": nil, "another key": {store.WithCipher(newCipher(t))}} {
		other, err := store.Open(ctx, url, opts...)
		if err != nil {
			t.Fatal(err)
		}
		_, err = other.GetChannel(ctx, "ops")
		other.Close()
		if err == nil {
			t.Errorf("%s: the channel was read", name)
		}
		if name == "no key" && !errors.Is(err, store.ErrKeyMissing) {
			t.Errorf("no key: %v", err)
		}
	}
}

func TestChannelsWrittenBeforeThereWasAKeyAreEncryptedOnRequest(t *testing.T) {
	plain, url := storetest.OpenURL(t)
	ctx := context.Background()
	for _, name := range []string{"old", "older"} {
		if _, err := plain.AddChannel(ctx, name, "https://example.com/"+name, "secret-"+name); err != nil {
			t.Fatal(err)
		}
	}
	if clear, sealed, err := plain.ChannelEncryption(ctx); err != nil || clear != 2 || sealed != 0 {
		t.Fatalf("before: %d clear, %d sealed, %v", clear, sealed, err)
	}
	if _, err := plain.EncryptChannels(ctx); err == nil {
		t.Fatal("channels were encrypted without a key")
	}

	s, err := store.Open(ctx, url, store.WithCipher(newCipher(t)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Still readable, in clear, before and after.
	if got, err := s.GetChannel(ctx, "old"); err != nil || got.Secret != "secret-old" {
		t.Fatalf("before: %+v, %v", got, err)
	}
	if n, err := s.EncryptChannels(ctx); err != nil || n != 2 {
		t.Fatalf("encrypt: %d, %v", n, err)
	}
	if n, err := s.EncryptChannels(ctx); err != nil || n != 0 {
		t.Fatalf("a second time: %d, %v", n, err)
	}
	if clear, sealed, _ := s.ChannelEncryption(ctx); clear != 0 || sealed != 2 {
		t.Fatalf("after: %d clear, %d sealed", clear, sealed)
	}
	if address, secret := raw(t, url, "older"); !strings.HasPrefix(address, "enc1:") || !strings.HasPrefix(secret, "enc1:") {
		t.Fatalf("the database holds %q and %q", address, secret)
	}
	if got, err := s.GetChannel(ctx, "older"); err != nil || got.URL != "https://example.com/older" || got.Secret != "secret-older" {
		t.Fatalf("after: %+v, %v", got, err)
	}
}
