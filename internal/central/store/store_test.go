package store_test

import (
	"context"
	"strings"
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

func TestInspectDoesNotMigrate(t *testing.T) {
	url := storetest.EmptyDatabase(t)
	info, err := store.Inspect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if info.ServerVersion == "" || info.SchemaVersion != 0 || info.LatestVersion < 1 {
		t.Fatalf("empty database: %+v", info)
	}
	again, _ := store.Inspect(context.Background(), url)
	if again.SchemaVersion != 0 {
		t.Fatal("inspecting changed the database")
	}
}

func TestInspectSeesAMigratedDatabase(t *testing.T) {
	_, url := storetest.OpenURL(t)
	info, err := store.Inspect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if info.TimescaleVersion == "" || info.SchemaVersion != info.LatestVersion || info.SchemaVersion < 1 {
		t.Fatalf("migrated database: %+v", info)
	}
}

func TestInspectFailsOnAnUnreachableDatabase(t *testing.T) {
	_, err := store.Inspect(context.Background(), "postgres://user:hunter2@127.0.0.1:1/none?connect_timeout=1")
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error %v", err)
	}
}
