package main

import (
	"flag"
	"strings"
	"time"

	"github.com/Arylite/netprobe/internal/central/alert"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/cli"
)

const defaultSessionTTL = 12 * time.Hour

func serve(args []string) error {
	ttlDefault, err := cli.GetenvDuration("NETPROBE_SESSION_TTL", defaultSessionTTL)
	if err != nil {
		return err
	}
	failuresDefault, err := cli.GetenvInt("NETPROBE_ALERT_FAILURES", alert.DefaultFailures)
	if err != nil {
		return err
	}
	silenceDefault, err := cli.GetenvDuration("NETPROBE_EDGE_SILENCE", alert.DefaultSilence)
	if err != nil {
		return err
	}
	alertIntervalDefault, err := cli.GetenvDuration("NETPROBE_ALERT_INTERVAL", alert.DefaultInterval)
	if err != nil {
		return err
	}
	retentionDefault, err := cli.GetenvDuration("NETPROBE_RETENTION", 0)
	if err != nil {
		return err
	}
	var cfg serveConfig
	var origins string
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&cfg.edgeListen, "edge-listen", cli.Getenv("NETPROBE_EDGE_LISTEN", "127.0.0.1:8080"), "address of the edge API, where edges poll and report (NETPROBE_EDGE_LISTEN)")
	fs.StringVar(&cfg.apiListen, "api-listen", cli.Getenv("NETPROBE_API_LISTEN", "127.0.0.1:8081"), "address of the UI API, where the web UI signs in and reads (NETPROBE_API_LISTEN)")
	fs.StringVar(&origins, "cors-origins", cli.Getenv("NETPROBE_CORS_ORIGINS", ""), "web origins allowed to call the UI API from a browser, scheme://host[:port] separated by commas; none by default (NETPROBE_CORS_ORIGINS)")
	fs.DurationVar(&cfg.sessionTTL, "session-ttl", ttlDefault, "how long a UI login lasts (NETPROBE_SESSION_TTL)")
	fs.IntVar(&cfg.alertFailures, "alert-failures", failuresDefault, "failures in a row that open an incident for a check on an edge (NETPROBE_ALERT_FAILURES)")
	fs.DurationVar(&cfg.edgeSilence, "edge-silence", silenceDefault, "how long an edge may go without a result before it is reported (NETPROBE_EDGE_SILENCE)")
	fs.DurationVar(&cfg.alertInterval, "alert-interval", alertIntervalDefault, "how often results are evaluated and notifications sent (NETPROBE_ALERT_INTERVAL)")
	fs.DurationVar(&cfg.retention, "retention", retentionDefault, "how long results are kept, 0 for ever; applied at start (NETPROBE_RETENTION)")
	databaseURL := databaseFlag(fs)
	fs.StringVar(&cfg.level, "log-level", cli.Getenv("NETPROBE_LOG_LEVEL", "info"), "debug, info, warn or error (NETPROBE_LOG_LEVEL)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.databaseURL = *databaseURL
	cfg.corsOrigins = splitList(origins)
	return run(cfg)
}

// splitList splits a comma separated list and drops the empty items.
func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func anyActive(edges []store.Edge) bool {
	for _, e := range edges {
		if e.Active() {
			return true
		}
	}
	return false
}
