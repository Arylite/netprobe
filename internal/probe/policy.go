package probe

import (
	"fmt"
	"net/netip"
	"syscall"
)

// DefaultDeny are the ranges an edge never connects to unless told otherwise:
// loopback, link-local (which holds cloud metadata services), unspecified and
// multicast. Private ranges are allowed: probing an intranet is the point.
var DefaultDeny = []string{
	"127.0.0.0/8", "::1/128",
	"169.254.0.0/16", "fe80::/10",
	"0.0.0.0/8", "::/128",
	"224.0.0.0/4", "ff00::/8",
}

// Policy lists the address ranges a probe must not connect to. The zero value
// denies nothing.
type Policy struct {
	deny []netip.Prefix
}

// ParsePolicy builds a policy from CIDR prefixes.
func ParsePolicy(cidrs []string) (Policy, error) {
	p := Policy{deny: make([]netip.Prefix, 0, len(cidrs))}
	for _, c := range cidrs {
		prefix, err := netip.ParsePrefix(c)
		if err != nil {
			return Policy{}, fmt.Errorf("deny range %q: %w", c, err)
		}
		p.deny = append(p.deny, prefix.Masked())
	}
	return p, nil
}

// Denies reports whether the policy forbids connecting to ip.
func (p Policy) Denies(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	for _, prefix := range p.deny {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// Control is a net.Dialer hook. It sees the address that is about to be
// dialed, after name resolution, so it also covers redirects and DNS answers
// that point at a denied range.
func (p Policy) Control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("parse address %q: %w", address, err)
	}
	if p.Denies(ap.Addr()) {
		return fmt.Errorf("connection to %s is denied by the policy", ap.Addr())
	}
	return nil
}
