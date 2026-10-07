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
  check add --id ID --kind KIND --target T [--expect E] [--interval SECONDS]
                           assign a check to every edge
  check list               list the checks
  check remove --id ID     stop assigning a check (its results are kept)
  channel add --name NAME --url URL [--secret-stdin]
                           add a webhook told when an incident opens or resolves;
                           the secret that signs it is read from stdin
  channel list             list the channels
  channel remove --name NAME
  channel test --name NAME send a test notification
  incident list [--open]   list the incidents, the newest first
  audit list               list who did what, the newest first
  user add --username NAME --role admin|viewer
                           create an account; the password is read from stdin
  user list                list the accounts
  user passwd --username NAME
                           change a password (read from stdin)
  user delete --username NAME
  secret-key               print a new key for NETPROBE_SECRET_KEY_FILE
  secrets encrypt          encrypt the channels written before there was a key
  doctor                   check the setup and say what is wrong
  healthcheck              exit 0 when both surfaces accept connections (containers)
  version                  print the version

Every command takes -h for its flags.
`

var errUsage = errors.New("usage")

func main() {
	err := dispatch(os.Args[1:], os.Stdout)
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
	case errors.Is(err, errChecksFailed):
		os.Exit(1)
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
	case "check":
		return checkCommand(args[1:], out)
	case "channel":
		return channelCommand(args[1:], out)
	case "incident":
		return incidentCommand(args[1:], out)
	case "audit":
		return auditCommand(args[1:], out)
	case "user":
		return userCommand(args[1:], out)
	case "secret-key":
		return secretKeyCommand(args[1:], out)
	case "secrets":
		return secretsCommand(args[1:], out)
	case "doctor":
		return doctorCommand(args[1:], out)
	case "healthcheck":
		return healthcheckCommand(args[1:], out)
	case "version", "--version":
		fmt.Fprintln(out, "netprobe-central", version.String())
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
		return nil
	}
	return fmt.Errorf("%w: unknown command %q", errUsage, args[0])
}
