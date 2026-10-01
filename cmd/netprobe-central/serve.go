package main

import (
	"flag"

	"github.com/Arylite/netprobe/internal/cli"
)

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", cli.Getenv("NETPROBE_LISTEN", "127.0.0.1:8080"), "address of the edge API (NETPROBE_LISTEN)")
	checksFile := fs.String("checks-file", cli.Getenv("NETPROBE_CHECKS_FILE", ""), "JSON file listing the checks to assign (NETPROBE_CHECKS_FILE)")
	dataDir := dataDirFlag(fs)
	level := fs.String("log-level", cli.Getenv("NETPROBE_LOG_LEVEL", "info"), "debug, info, warn or error (NETPROBE_LOG_LEVEL)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return run(*listen, *checksFile, *dataDir, *level)
}

func dataDirFlag(fs *flag.FlagSet) *string {
	return fs.String("data-dir", cli.Getenv("NETPROBE_DATA_DIR", "data"), "directory holding the registry of edges (NETPROBE_DATA_DIR)")
}
