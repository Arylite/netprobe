package probe

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

type dnsSpec struct {
	qtype  string // A, AAAA, MX, TXT, NS, CNAME; empty is A and AAAA
	name   string
	server string // host:port of a resolver; empty is the one of the system
}

// parseDNS reads "[TYPE ]name[@server]", for example "MX example.com@1.1.1.1".
func parseDNS(target string) (dnsSpec, error) {
	var spec dnsSpec
	fields := strings.Fields(target)
	if len(fields) == 2 {
		spec.qtype = strings.ToUpper(fields[0])
		switch spec.qtype {
		case "A", "AAAA", "MX", "TXT", "NS", "CNAME":
		default:
			return spec, fmt.Errorf("target %q: the type is A, AAAA, MX, TXT, NS or CNAME", target)
		}
		fields = fields[1:]
	}
	if len(fields) != 1 {
		return spec, fmt.Errorf("target %q: want [TYPE ]name[@server]", target)
	}
	name, server, _ := strings.Cut(fields[0], "@")
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 || strings.ContainsAny(name, "/:") {
		return spec, fmt.Errorf("target %q: %q is not a name", target, name)
	}
	spec.name = name
	if server != "" {
		hp, err := splitHostPort(server, "53")
		if err != nil {
			return spec, fmt.Errorf("target %q: the server: %w", target, err)
		}
		host, _, _ := net.SplitHostPort(hp)
		if _, err := netip.ParseAddr(host); err != nil {
			return spec, fmt.Errorf("target %q: the server must be an address, not a name", target)
		}
		spec.server = hp
	}
	return spec, nil
}

// DNS measures the time to resolve a name. A good answer has at least one
// record, and when expect is given, one of them holds that text.
func (p *Prober) DNS(ctx context.Context, target, expect string) Outcome {
	spec, err := parseDNS(target)
	if err != nil {
		return failure(0, err)
	}
	r := net.DefaultResolver
	if spec.server != "" {
		// The server is dialed by the prober, so the policy covers it.
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return p.dialer.DialContext(ctx, network, spec.server)
			},
		}
	}
	ctx, cancel := context.WithDeadline(ctx, p.deadline(ctx))
	defer cancel()

	start := time.Now()
	answers, err := lookup(ctx, r, spec)
	rtt := time.Since(start)
	if err != nil {
		return failure(rtt, err)
	}
	if len(answers) == 0 {
		return Outcome{RTT: rtt, Err: "no record"}
	}
	if expect != "" && !anyContains(answers, expect) {
		return Outcome{RTT: rtt, Err: fmt.Sprintf("none of %s holds %q", strings.Join(answers, ", "), expect)}
	}
	return Outcome{OK: true, RTT: rtt}
}

func lookup(ctx context.Context, r *net.Resolver, spec dnsSpec) ([]string, error) {
	switch spec.qtype {
	case "":
		return r.LookupHost(ctx, spec.name)
	case "A", "AAAA":
		network := "ip4"
		if spec.qtype == "AAAA" {
			network = "ip6"
		}
		ips, err := r.LookupIP(ctx, network, spec.name)
		out := make([]string, 0, len(ips))
		for _, ip := range ips {
			out = append(out, ip.String())
		}
		return out, err
	case "MX":
		mx, err := r.LookupMX(ctx, spec.name)
		out := make([]string, 0, len(mx))
		for _, m := range mx {
			out = append(out, fmt.Sprintf("%d %s", m.Pref, m.Host))
		}
		return out, err
	case "NS":
		ns, err := r.LookupNS(ctx, spec.name)
		out := make([]string, 0, len(ns))
		for _, n := range ns {
			out = append(out, n.Host)
		}
		return out, err
	case "TXT":
		return r.LookupTXT(ctx, spec.name)
	default: // CNAME
		c, err := r.LookupCNAME(ctx, spec.name)
		if err != nil {
			return nil, err
		}
		return []string{c}, nil
	}
}

func anyContains(answers []string, text string) bool {
	text = strings.ToLower(text)
	for _, a := range answers {
		if strings.Contains(strings.ToLower(a), text) {
			return true
		}
	}
	return false
}
