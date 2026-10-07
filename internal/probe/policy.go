package probe

import (
	"fmt"
	"net/netip"
	"strings"
	"syscall"
)

// DefaultDeny are the ranges an edge never connects to unless told otherwise:
// loopback, link-local (which holds the metadata service of most clouds),
// unspecified and multicast, and the addresses at which the other clouds serve
// theirs. Private ranges are allowed: probing an intranet is the point.
var DefaultDeny = []string{
	"127.0.0.0/8", "::1/128",
	"169.254.0.0/16", "fe80::/10",
	"0.0.0.0/8", "::/128",
	"224.0.0.0/4", "ff00::/8",
	// Metadata services outside link-local: Alibaba Cloud, Oracle Cloud,
	// Azure's host service, and AWS over IPv6.
	"100.100.100.200/32", "192.0.0.192/32", "168.63.129.16/32", "fd00:ec2::254/128",
}

var (
	nat64      = netip.MustParsePrefix("64:ff9b::/96")
	nat64Local = netip.MustParsePrefix("64:ff9b:1::/48")
	sixToFour  = netip.MustParsePrefix("2002::/16")
)

// embeddedIPv4 returns the IPv4 address that an IPv6 address carries when it
// is one a gateway translates: NAT64 (a connection to 64:ff9b::a9fe:a9fe
// reaches 169.254.169.254) and 6to4. A policy that only looked at the IPv6 form
// would let them through.
func embeddedIPv4(ip netip.Addr) (netip.Addr, bool) {
	b := ip.As16()
	switch {
	case nat64.Contains(ip), nat64Local.Contains(ip):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case sixToFour.Contains(ip):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	}
	return netip.Addr{}, false
}

// Policy lists the address ranges a probe must not connect to. The zero value
// denies nothing.
type Policy struct {
	deny []netip.Prefix
}

// DenyNone is the --deny value that denies nothing.
const DenyNone = "none"

// ParseDeny reads CIDR ranges separated by commas, or "none".
func ParseDeny(value string) (Policy, error) {
	if strings.TrimSpace(value) == DenyNone {
		return Policy{}, nil
	}
	var cidrs []string
	for c := range strings.SplitSeq(value, ",") {
		cidrs = append(cidrs, strings.TrimSpace(c))
	}
	return ParsePolicy(cidrs)
}

// DefaultPolicy is the policy of DefaultDeny.
func DefaultPolicy() Policy {
	p, err := ParsePolicy(DefaultDeny)
	if err != nil {
		panic("probe: invalid DefaultDeny: " + err.Error())
	}
	return p
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
	if v4, ok := embeddedIPv4(ip); ok {
		return p.Denies(v4)
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
