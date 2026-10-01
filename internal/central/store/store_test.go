package store_test

import (
	"context"
	"testing"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func TestOpenAppliesTheMigrationsOnce(t *testing.T) {
	s, url := storetest.OpenURL(t)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}

	// A second open finds the schema up to date and changes nothing.
	again, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer again.Close()
}

func TestOpenFailsOnAnUnreachableDatabase(t *testing.T) {
	if _, err := store.Open(context.Background(), "postgres://nobody:nothing@127.0.0.1:1/none?connect_timeout=1"); err == nil {
		t.Fatal("opened a database that is not there")
	}
}
