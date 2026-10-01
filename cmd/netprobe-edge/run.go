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
	"github.com/Arylite/netprobe/internal/version"
)

const minPollInterval = time.Second

func run(central, tokenFile string, poll time.Duration, level string) error {
	if central == "" {
		return errors.New("set --central or NETPROBE_CENTRAL")
	}
	if poll < minPollInterval {
		return fmt.Errorf("poll interval %s is below %s", poll, minPollInterval)
	}
	log, err := cli.NewLogger(level)
	if err != nil {
		return err
	}
	token, err := readToken(tokenFile)
	if err != nil {
		return err
	}
	client, err := edge.NewClient(central, token)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Info("edge starting", "central", central, "poll_interval", poll.String(), "version", version.Version)
	(&edge.Agent{Client: client, Interval: poll, Log: log}).Run(ctx)
	log.Info("edge stopped")
	return nil
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
