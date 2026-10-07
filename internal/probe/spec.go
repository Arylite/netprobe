package probe

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

// minInterval is the shortest interval a kind may run at: some probes send
// several packets, or move a lot of data.
var minInterval = map[string]int{
	api.KindICMP:     5,
	api.KindNTP:      5,
	api.KindTLS:      60,
	api.KindDownload: 60,
	api.KindRoute:    60,
	api.KindDomain:   3600,
}

// Validate reports why a check cannot be run: a target or an expectation that
// its kind cannot read, or an interval that is too short for it.
func Validate(c api.Check) error {
	if min := minInterval[c.Kind]; c.IntervalSeconds < min {
		return fmt.Errorf("check %s: a %s check runs every %d seconds at most", c.ID, c.Kind, min)
	}
	if err := parse(c); err != nil {
		return fmt.Errorf("check %s: %w", c.ID, err)
	}
	return nil
}

// parse reads the target and the expectation of c the way its kind does.
func parse(c api.Check) error {
	var err error
	switch c.Kind {
	case api.KindTCP:
		err = noExpect(c)
		if err == nil {
			err = hostPort(c.Target, "")
		}
	case api.KindHTTP:
		_, err = parseHTTP(c.Target, c.Expect)
	case api.KindDNS:
		_, err = parseDNS(c.Target)
	case api.KindTLS:
		_, _, err = parseTLS(c.Target, c.Expect)
	case api.KindICMP:
		_, _, err = parsePing(c.Target, c.Expect)
	case api.KindNTP:
		_, _, err = parseNTP(c.Target, c.Expect)
	case api.KindBanner, api.KindClosed:
		if c.Kind == api.KindClosed {
			err = noExpect(c)
		}
		if err == nil {
			err = hostPort(c.Target, "")
		}
	case api.KindDownload:
		_, _, err = parseDownload(c.Target, c.Expect)
	case api.KindRoute:
		_, _, err = parseTraceroute(c.Target, c.Expect)
	case api.KindDomain:
		_, _, err = parseDomain(c.Target, c.Expect)
	default:
		err = fmt.Errorf("unknown kind %q", c.Kind)
	}
	return err
}

// Run measures c.
func (p *Prober) Run(ctx context.Context, c api.Check) Outcome {
	switch c.Kind {
	case api.KindTCP:
		return p.TCP(ctx, c.Target)
	case api.KindHTTP:
		return p.httpCheck(ctx, c.Target, c.Expect)
	case api.KindDNS:
		return p.DNS(ctx, c.Target, c.Expect)
	case api.KindTLS:
		return p.TLS(ctx, c.Target, c.Expect)
	case api.KindICMP:
		return p.Ping(ctx, c.Target, c.Expect)
	case api.KindNTP:
		return p.NTP(ctx, c.Target, c.Expect)
	case api.KindBanner:
		return p.Banner(ctx, c.Target, c.Expect)
	case api.KindClosed:
		return p.Closed(ctx, c.Target)
	case api.KindDownload:
		return p.Download(ctx, c.Target, c.Expect)
	case api.KindRoute:
		return p.Traceroute(ctx, c.Target, c.Expect)
	case api.KindDomain:
		return p.Domain(ctx, c.Target, c.Expect)
	}
	return Outcome{Err: fmt.Sprintf("unsupported check kind %q", c.Kind)}
}

func noExpect(c api.Check) error {
	if c.Expect != "" {
		return fmt.Errorf("a %s check takes no expect", c.Kind)
	}
	return nil
}

// hostPort checks that target is host:port, or a host alone when a default
// port is given.
func hostPort(target, defaultPort string) error {
	_, err := splitHostPort(target, defaultPort)
	return err
}

func splitHostPort(target, defaultPort string) (string, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		if defaultPort == "" || strings.ContainsAny(target, " \t/") || target == "" {
			return "", fmt.Errorf("target %q: want host:port", target)
		}
		host, port = strings.Trim(target, "[]"), defaultPort
	}
	if host == "" {
		return "", fmt.Errorf("target %q: the host is empty", target)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("target %q: %q is not a port", target, port)
	}
	return net.JoinHostPort(host, port), nil
}

// deadline is the time a probe may take: the timeout of the prober, or the
// context, whichever ends first.
func (p *Prober) deadline(ctx context.Context) time.Time {
	d := time.Now().Add(p.timeout)
	if cd, ok := ctx.Deadline(); ok && cd.Before(d) {
		return cd
	}
	return d
}
