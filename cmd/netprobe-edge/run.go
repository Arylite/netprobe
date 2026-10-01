package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/edge"
	"github.com/Arylite/netprobe/internal/probe"
	"github.com/Arylite/netprobe/internal/version"
)

const (
	minInterval  = time.Second
	probeTimeout = 10 * time.Second
)

type config struct {
	central   string
	tokenFile string
	deny      string
	poll      time.Duration
	report    time.Duration
	level     string
}

func run(cfg config) error {
	if cfg.central == "" {
		return errors.New("set --central or NETPROBE_CENTRAL")
	}
	if cfg.poll < minInterval || cfg.report < minInterval {
		return fmt.Errorf("poll and report intervals must be at least %s", minInterval)
	}
	log, err := cli.NewLogger(cfg.level)
	if err != nil {
		return err
	}
	policy, err := parseDeny(cfg.deny)
	if err != nil {
		return err
	}
	token, err := readToken(cfg.tokenFile)
	if err != nil {
		return err
	}
	client, err := edge.NewClient(cfg.central, token)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Info("edge starting", "central", cfg.central, "poll_interval", cfg.poll.String(), "report_interval", cfg.report.String(), "deny", cfg.deny, "version", version.Version)
	if cfg.deny == noDeny {
		log.Warn("no range is denied: the central can make this edge probe any address, loopback included")
	}
	agent := &edge.Agent{
		Client:         client,
		Measure:        edge.ProbeMeasurer(probe.New(policy, probeTimeout)),
		Interval:       cfg.poll,
		ReportInterval: cfg.report,
		Log:            log,
	}
	agent.Run(ctx)
	log.Info("edge stopped")
	return nil
}

const noDeny = "none"

// parseDeny turns the --deny value into a policy: CIDRs separated by commas,
// or "none".
func parseDeny(value string) (probe.Policy, error) {
	if value == noDeny {
		return probe.Policy{}, nil
	}
	var cidrs []string
	for _, c := range strings.Split(value, ",") {
		cidrs = append(cidrs, strings.TrimSpace(c))
	}
	return probe.ParsePolicy(cidrs)
}

// readToken takes the token from the file when one is given, else from
// NETPROBE_TOKEN. It is never a flag: command lines are visible to every user.
func readToken(file string) (string, error) {
	token := os.Getenv("NETPROBE_TOKEN")
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read token file: %w", err)
		}
		token = string(raw)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("no token: set NETPROBE_TOKEN or --token-file")
	}
	return token, nil
}
