package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/cli"
)

const connectTimeout = 30 * time.Second

func databaseFlag(fs *flag.FlagSet) *string {
	return fs.String("database-url", cli.Getenv("NETPROBE_DATABASE_URL", ""), "PostgreSQL URL, or set NETPROBE_DATABASE_URL_FILE to a file that holds it (NETPROBE_DATABASE_URL)")
}

// resolveDatabaseURL returns the URL given, or the one in the file that
// NETPROBE_DATABASE_URL_FILE names.
func resolveDatabaseURL(url string) (string, error) {
	if file := os.Getenv("NETPROBE_DATABASE_URL_FILE"); url == "" && file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read the database URL file: %w", err)
		}
		url = strings.TrimSpace(string(raw))
	}
	return url, nil
}

// openStore connects and migrates. The URL holds a password, so it is never
// echoed in an error.
func openStore(ctx context.Context, url string) (*store.Store, error) {
	url, err := resolveDatabaseURL(url)
	if err != nil {
		return nil, err
	}
	if url == "" {
		return nil, errors.New("set --database-url, NETPROBE_DATABASE_URL or NETPROBE_DATABASE_URL_FILE")
	}
	cipher, err := loadCipher()
	if err != nil {
		return nil, err
	}
	var opts []store.Option
	if cipher != nil {
		opts = append(opts, store.WithCipher(cipher))
	}
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	return store.Open(ctx, url, opts...)
}
