package main

import (
	"flag"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/cli"
)

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", cli.Getenv("NETPROBE_LISTEN", "127.0.0.1:8080"), "address of the edge API (NETPROBE_LISTEN)")
	databaseURL := databaseFlag(fs)
	level := fs.String("log-level", cli.Getenv("NETPROBE_LOG_LEVEL", "info"), "debug, info, warn or error (NETPROBE_LOG_LEVEL)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return run(*listen, *databaseURL, *level)
}

func anyActive(edges []store.Edge) bool {
	for _, e := range edges {
		if e.Active() {
			return true
		}
	}
	return false
}
