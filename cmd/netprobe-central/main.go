package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/version"
)

func main() {
	listen := flag.String("listen", cli.Getenv("NETPROBE_LISTEN", "127.0.0.1:8080"), "address of the edge API (NETPROBE_LISTEN)")
	checksFile := flag.String("checks-file", cli.Getenv("NETPROBE_CHECKS_FILE", ""), "JSON file listing the checks to assign (NETPROBE_CHECKS_FILE)")
	level := flag.String("log-level", cli.Getenv("NETPROBE_LOG_LEVEL", "info"), "debug, info, warn or error (NETPROBE_LOG_LEVEL)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("netprobe-central", version.String())
		return
	}
	if err := run(*listen, *checksFile, *level); err != nil {
		fmt.Fprintln(os.Stderr, "netprobe-central:", err)
		os.Exit(1)
	}
}
