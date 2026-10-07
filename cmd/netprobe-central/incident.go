package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/Arylite/netprobe/internal/central/store"
)

func incidentCommand(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	if args[0] == "list" {
		return incidentList(args[1:], out)
	}
	return fmt.Errorf("%w: unknown incident command %q", errUsage, args[0])
}

func incidentList(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("incident list", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	openOnly := fs.Bool("open", false, "only the incidents that are still going on")
	limit := fs.Int("limit", 50, "how many to list, the newest first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	incidents, err := st.ListIncidents(context.Background(), *openOnly, *limit)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tKIND\tCHECK\tEDGE\tSTARTED\tSTATE\tDETAIL")
	for _, i := range incidents {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", i.ID, i.Kind, dash(i.CheckID), i.EdgeName,
			i.StartedAt.Local().Format("2006-01-02 15:04"), incidentState(i), i.Detail)
	}
	return w.Flush()
}

func incidentState(i store.Incident) string {
	if i.ResolvedAt == nil {
		return "open"
	}
	return i.Resolution + " after " + i.ResolvedAt.Sub(i.StartedAt).Round(time.Second).String()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
