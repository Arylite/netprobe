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
	caFile := fs.String("ca-file", cli.Getenv("NETPROBE_CA_FILE", ""), "PEM file of a CA to trust besides the system ones (NETPROBE_CA_FILE)")
	certFile := fs.String("client-cert", cli.Getenv("NETPROBE_CLIENT_CERT", ""), "client certificate (PEM) when the central asks edges for one (NETPROBE_CLIENT_CERT)")
	keyFile := fs.String("client-key", cli.Getenv("NETPROBE_CLIENT_KEY", ""), "private key of the client certificate (NETPROBE_CLIENT_KEY)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tlsConfig, err := edge.TLSConfig(*caFile, *certFile, *keyFile)
	if err != nil {
		return err
	}

	token, err := readToken(*tokenFile)
	if err != nil && *tokenFile != "" {
		return err // a file that cannot be read is not a missing token
	}

	lines := doctor.Run(context.Background(), edge.DoctorSteps(edge.DoctorConfig{Central: *central, Token: token, TLS: tlsConfig}))
	doctor.Print(out, lines)
	if doctor.Failed(lines) {
		return errChecksFailed
	}
	return nil
}
