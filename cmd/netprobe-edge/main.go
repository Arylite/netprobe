package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/version"
)

const defaultPollInterval = 30 * time.Second

func main() {
	pollDefault, err := cli.GetenvDuration("NETPROBE_POLL_INTERVAL", defaultPollInterval)
	if err != nil {
		fail(err)
	}
	central := flag.String("central", cli.Getenv("NETPROBE_CENTRAL", ""), "URL of the central, http(s)://host[:port] (NETPROBE_CENTRAL)")
	poll := flag.Duration("poll-interval", pollDefault, "time between two polls (NETPROBE_POLL_INTERVAL)")
	level := flag.String("log-level", cli.Getenv("NETPROBE_LOG_LEVEL", "info"), "debug, info, warn or error (NETPROBE_LOG_LEVEL)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("netprobe-edge", version.String())
		return
	}
	if err := run(*central, *poll, *level); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "netprobe-edge:", err)
	os.Exit(1)
}
