package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/probe"
	"github.com/Arylite/netprobe/internal/version"
)

const (
	defaultPollInterval   = 30 * time.Second
	defaultReportInterval = 10 * time.Second
)

func main() {
	pollDefault, err := cli.GetenvDuration("NETPROBE_POLL_INTERVAL", defaultPollInterval)
	if err != nil {
		fail(err)
	}
	reportDefault, err := cli.GetenvDuration("NETPROBE_REPORT_INTERVAL", defaultReportInterval)
	if err != nil {
		fail(err)
	}

	var cfg config
	flag.StringVar(&cfg.central, "central", cli.Getenv("NETPROBE_CENTRAL", ""), "URL of the central, http(s)://host[:port] (NETPROBE_CENTRAL)")
	flag.StringVar(&cfg.tokenFile, "token-file", cli.Getenv("NETPROBE_TOKEN_FILE", ""), "file holding the token, or set NETPROBE_TOKEN (NETPROBE_TOKEN_FILE)")
	flag.StringVar(&cfg.deny, "deny", cli.Getenv("NETPROBE_DENY", strings.Join(probe.DefaultDeny, ",")), "ranges never probed, CIDRs separated by commas, or none (NETPROBE_DENY)")
	flag.DurationVar(&cfg.poll, "poll-interval", pollDefault, "time between two polls of the central (NETPROBE_POLL_INTERVAL)")
	flag.DurationVar(&cfg.report, "report-interval", reportDefault, "time between two reports of results (NETPROBE_REPORT_INTERVAL)")
	flag.StringVar(&cfg.level, "log-level", cli.Getenv("NETPROBE_LOG_LEVEL", "info"), "debug, info, warn or error (NETPROBE_LOG_LEVEL)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("netprobe-edge", version.String())
		return
	}
	if err := run(cfg); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "netprobe-edge:", err)
	os.Exit(1)
}
