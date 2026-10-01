package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Arylite/netprobe/internal/version"
)

const usage = `usage: netprobe-central <command> [flags]

commands:
  serve                    run the central
  edge add --name NAME     register an edge and print its token (shown once)
  edge list                list the edges
  edge revoke --name NAME  cut an edge off
  version                  print the version

Every command takes -h for its flags.
`

var errUsage = errors.New("usage")

func main() {
	err := dispatch(os.Args[1:], os.Stdout)
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
	case errors.Is(err, errUsage):
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "netprobe-central:", err)
		os.Exit(1)
	}
}

func dispatch(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "edge":
		return edgeCommand(args[1:], out)
	case "version", "--version":
		fmt.Fprintln(out, "netprobe-central", version.String())
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
		return nil
	}
	return fmt.Errorf("%w: unknown command %q", errUsage, args[0])
}
