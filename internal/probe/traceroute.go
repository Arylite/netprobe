package probe

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"time"
)

const (
	defaultMaxHops = 30
	// perHop is how long a router has to answer a probe.
	perHop = time.Second
	// darkHops is how many silent routers in a row end the trace: the path goes dark.
	darkHops = 8
)

// route is what a trace found.
type route struct {
	reached bool
	hops    int           // the number of the last hop that answered
	last    netip.Addr    // who answered last
	rtt     time.Duration // the round trip of the last answer
	problem string        // why it ended before the destination, when it did
}

// parseTraceroute reads a host or an address, and the most hops the path may
// have (30 by default).
func parseTraceroute(target, expect string) (host string, maxHops int, err error) {
	host, _, err = parsePing(target, "")
	if err != nil {
		return "", 0, err
	}
	maxHops = defaultMaxHops
	if expect != "" {
		maxHops, err = strconv.Atoi(expect)
		if err != nil || maxHops < 1 || maxHops > 64 {
			return "", 0, fmt.Errorf("expect %q: want the most hops the path may have, from 1 to 64", expect)
		}
	}
	return host, maxHops, nil
}

// Traceroute follows the path to a host, one router at a time. It is good when
// the host is reached in at most expect hops (30 by default); otherwise it says
// where the path ended: the last router that answered, and how far it was. The
// time is the round trip to the host.
//
// It needs Linux, and no privilege: it sends UDP packets with a short time to
// live and reads the ICMP errors that come back.
func (p *Prober) Traceroute(ctx context.Context, target, expect string) Outcome {
	host, maxHops, err := parseTraceroute(target, expect)
	if err != nil {
		return failure(0, err)
	}
	ctx, cancel := context.WithDeadline(ctx, p.deadline(ctx))
	defer cancel()

	ip, err := resolveOne(ctx, host)
	if err != nil {
		return failure(0, err)
	}
	if p.policy.Denies(ip) {
		return failure(0, fmt.Errorf("connection to %s is denied by the policy", ip))
	}
	r, err := trace(ctx, ip, maxHops)
	if err != nil {
		return failure(0, err)
	}
	if r.reached {
		return Outcome{OK: true, RTT: r.rtt}
	}
	switch {
	case r.problem != "":
		return Outcome{Err: r.problem}
	case r.hops == 0:
		return Outcome{Err: fmt.Sprintf("no router answered, %s is not reached in %d hops", ip, maxHops)}
	}
	return Outcome{Err: fmt.Sprintf("%s is not reached in %d hops: the last answer is from %s, at hop %d", ip, maxHops, r.last, r.hops)}
}
