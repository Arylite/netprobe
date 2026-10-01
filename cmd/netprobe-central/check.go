package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/Arylite/netprobe/internal/api"
)

func checkCommand(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "add":
		return checkAdd(args[1:], out)
	case "list":
		return checkList(args[1:], out)
	case "remove":
		return checkRemove(args[1:], out)
	}
	return fmt.Errorf("%w: unknown check command %q", errUsage, args[0])
}

func checkAdd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("check add", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	id := fs.String("id", "", "identifier of the check")
	kind := fs.String("kind", "", "tcp or http")
	target := fs.String("target", "", "host:port for tcp, a URL for http")
	interval := fs.Int("interval", 30, "seconds between two runs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	check := api.Check{ID: *id, Kind: *kind, Target: *target, IntervalSeconds: *interval}
	if err := check.Validate(); err != nil {
		return err
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.AddCheck(context.Background(), check); err != nil {
		return err
	}
	fmt.Fprintf(out, "check %s added\n", check.ID)
	return nil
}

func checkList(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("check list", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	checks, err := st.ListChecks(context.Background())
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tKIND\tTARGET\tINTERVAL")
	for _, c := range checks {
		fmt.Fprintf(w, "%s\t%s\t%s\t%ds\n", c.ID, c.Kind, c.Target, c.IntervalSeconds)
	}
	return w.Flush()
}

func checkRemove(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("check remove", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	id := fs.String("id", "", "identifier of the check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("--id is required")
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.RemoveCheck(context.Background(), *id); err != nil {
		return err
	}
	fmt.Fprintf(out, "check %s removed, its results are kept\n", *id)
	return nil
}
