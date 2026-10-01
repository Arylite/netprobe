package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/edge"
	"github.com/Arylite/netprobe/internal/version"
)

const minPollInterval = time.Second

func run(central string, poll time.Duration, level string) error {
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
	client, err := edge.NewClient(central)
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
