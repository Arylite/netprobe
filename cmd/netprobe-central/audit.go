package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
)

func auditCommand(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "list" {
		return errUsage
	}
	fs := flag.NewFlagSet("audit list", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	limit := fs.Int("limit", 50, "how many events to list, the newest first")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	events, err := st.ListAudit(context.Background(), *limit)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "WHEN\tWHO\tACTION\tTARGET\tFROM")
	for _, e := range events {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.At.Local().Format("2006-01-02 15:04:05"), dash(e.Actor), e.Action, dash(e.Target), dash(e.ClientIP))
	}
	return w.Flush()
}
