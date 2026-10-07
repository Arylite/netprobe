package main

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/Arylite/netprobe/internal/central"
	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/doctor"
)

// errChecksFailed makes the exit status non-zero; the report already said why.
var errChecksFailed = errors.New("some checks failed")

// doctorCommand diagnoses the settings 'serve' would run with.
func doctorCommand(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	edgeListen := fs.String("edge-listen", cli.Getenv("NETPROBE_EDGE_LISTEN", "127.0.0.1:8080"), "address of the edge API (NETPROBE_EDGE_LISTEN)")
	apiListen := fs.String("api-listen", cli.Getenv("NETPROBE_API_LISTEN", "127.0.0.1:8081"), "address of the UI API (NETPROBE_API_LISTEN)")
	edgeCert := fs.String("edge-tls-cert", cli.Getenv("NETPROBE_EDGE_TLS_CERT", ""), "certificate file of the edge API (NETPROBE_EDGE_TLS_CERT)")
	apiCert := fs.String("api-tls-cert", cli.Getenv("NETPROBE_API_TLS_CERT", ""), "certificate file of the UI API (NETPROBE_API_TLS_CERT)")
	databaseURLFlag := databaseFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	url, err := resolveDatabaseURL(*databaseURLFlag)
	if err != nil {
		return err
	}
	steps, release := central.DoctorSteps(central.DoctorConfig{DatabaseURL: url, EdgeListen: *edgeListen, APIListen: *apiListen, EdgeTLSCert: *edgeCert, APITLSCert: *apiCert})
	defer release()
	lines := doctor.Run(context.Background(), steps)
	doctor.Print(out, lines)
	if doctor.Failed(lines) {
		return errChecksFailed
	}
	return nil
}
