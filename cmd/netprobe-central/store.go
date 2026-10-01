package main

import (
	"context"
	"errors"
	"flag"
	"time"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/cli"
)

const connectTimeout = 30 * time.Second

func databaseFlag(fs *flag.FlagSet) *string {
	return fs.String("database-url", cli.Getenv("NETPROBE_DATABASE_URL", ""), "PostgreSQL URL (NETPROBE_DATABASE_URL)")
}

// openStore connects and migrates. The URL holds a password, so it is never
// echoed in an error.
func openStore(ctx context.Context, url string) (*store.Store, error) {
	if url == "" {
		return nil, errors.New("set --database-url or NETPROBE_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	return store.Open(ctx, url)
}
