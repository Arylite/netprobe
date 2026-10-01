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
	listen := fs.String("listen", cli.Getenv("NETPROBE_LISTEN", "127.0.0.1:8080"), "address of the edge API (NETPROBE_LISTEN)")
	databaseURL := databaseFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	steps, release := central.DoctorSteps(central.DoctorConfig{DatabaseURL: *databaseURL, Listen: *listen})
	defer release()
	lines := doctor.Run(context.Background(), steps)
	doctor.Print(out, lines)
	if doctor.Failed(lines) {
		return errChecksFailed
	}
	return nil
}
