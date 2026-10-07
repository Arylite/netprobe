package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Arylite/netprobe/internal/cli"
)

const healthTimeout = 3 * time.Second

// healthcheckCommand exits 0 when both surfaces accept connections. It is for
// containers, which have no shell or curl to ask the question with.
func healthcheckCommand(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	edgeListen := fs.String("edge-listen", cli.Getenv("NETPROBE_EDGE_LISTEN", "127.0.0.1:8080"), "address of the edge API (NETPROBE_EDGE_LISTEN)")
	apiListen := fs.String("api-listen", cli.Getenv("NETPROBE_API_LISTEN", "127.0.0.1:8081"), "address of the UI API (NETPROBE_API_LISTEN)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, addr := range []string{*edgeListen, *apiListen} {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("listen address %q: %w", addr, err)
		}
		if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
			host = "127.0.0.1"
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), healthTimeout)
		if err != nil {
			return fmt.Errorf("%s does not accept connections: %w", addr, err)
		}
		_ = conn.Close()
	}
	fmt.Fprintln(out, "ok")
	return nil
}
