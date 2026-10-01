package main

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/doctor"
	"github.com/Arylite/netprobe/internal/edge"
)

// errChecksFailed makes the exit status non-zero; the report already said why.
var errChecksFailed = errors.New("some checks failed")

// doctorCommand diagnoses the settings the agent would run with.
func doctorCommand(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	central := fs.String("central", cli.Getenv("NETPROBE_CENTRAL", ""), "URL of the central, http(s)://host[:port] (NETPROBE_CENTRAL)")
	tokenFile := fs.String("token-file", cli.Getenv("NETPROBE_TOKEN_FILE", ""), "file holding the token, or set NETPROBE_TOKEN (NETPROBE_TOKEN_FILE)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	token, err := readToken(*tokenFile)
	if err != nil && *tokenFile != "" {
		return err // a file that cannot be read is not a missing token
	}

	lines := doctor.Run(context.Background(), edge.DoctorSteps(edge.DoctorConfig{Central: *central, Token: token}))
	doctor.Print(out, lines)
	if doctor.Failed(lines) {
		return errChecksFailed
	}
	return nil
}
