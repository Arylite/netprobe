package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/Arylite/netprobe/internal/central/store"
)

func edgeCommand(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "add":
		return edgeAdd(args[1:], out)
	case "list":
		return edgeList(args[1:], out)
	case "revoke":
		return edgeRevoke(args[1:], out)
	}
	return fmt.Errorf("%w: unknown edge command %q", errUsage, args[0])
}

// openForEdge parses the flags shared by the edge commands and opens the store;
// the name is empty for commands that take none.
func openForEdge(cmd string, args []string, wantName bool) (*store.Store, string, error) {
	fs := flag.NewFlagSet("edge "+cmd, flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	var name *string
	if wantName {
		name = fs.String("name", "", "name of the edge: lowercase letters, digits and dashes")
	}
	if err := fs.Parse(args); err != nil {
		return nil, "", err
	}
	if wantName && *name == "" {
		return nil, "", errors.New("--name is required")
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return nil, "", err
	}
	if wantName {
		return st, *name, nil
	}
	return st, "", nil
}

func edgeAdd(args []string, out io.Writer) error {
	st, name, err := openForEdge("add", args, true)
	if err != nil {
		return err
	}
	defer st.Close()
	edge, token, err := st.AddEdge(context.Background(), name)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "edge %s registered (id %s)\n", edge.Name, edge.ID)
	fmt.Fprintln(out, "token, shown once, keep it secret:")
	fmt.Fprintln(out, token)
	return nil
}

func edgeList(args []string, out io.Writer) error {
	st, _, err := openForEdge("list", args, false)
	if err != nil {
		return err
	}
	defer st.Close()
	edges, err := st.ListEdges(context.Background())
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tID\tSTATUS\tCREATED")
	for _, e := range edges {
		status := "active"
		if !e.Active() {
			status = "revoked"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Name, e.ID, status, e.CreatedAt.Format("2006-01-02 15:04"))
	}
	return w.Flush()
}

func edgeRevoke(args []string, out io.Writer) error {
	st, name, err := openForEdge("revoke", args, true)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.RevokeEdge(context.Background(), name); err != nil {
		return err
	}
	fmt.Fprintf(out, "edge %s revoked\n", name)
	return nil
}
