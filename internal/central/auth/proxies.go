package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// Proxies are the reverse proxies trusted to say who the client is. Behind a
// proxy every request shares its address, so the failure limits would lock
// everyone out together. The zero value trusts none.
type Proxies struct {
	prefixes []netip.Prefix
}

// ParseProxies reads addresses or CIDR ranges of proxies. A range that holds
// every address is refused: it would trust every sender.
func ParseProxies(values []string) (Proxies, error) {
	var p Proxies
	for _, v := range values {
		v = strings.TrimSpace(v)
		prefix, err := netip.ParsePrefix(v)
		if err != nil {
			addr, addrErr := netip.ParseAddr(v)
			if addrErr != nil {
				return Proxies{}, fmt.Errorf("trusted proxy %q: want an address or a CIDR range", v)
			}
			prefix = netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen())
		}
		if prefix.Bits() == 0 {
			return Proxies{}, errors.New("trusted proxies: a range that holds every address would trust every sender")
		}
		p.prefixes = append(p.prefixes, prefix.Masked())
	}
	return p, nil
}

func (p Proxies) trusts(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	for _, prefix := range p.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// ClientIP is the address a request is counted under: the peer, or when that is
// a trusted proxy the first address of X-Forwarded-For, from the right, that is
// not one itself.
func (p Proxies) ClientIP(r *http.Request) string {
	peer := PeerHost(r)
	peerAddr, err := netip.ParseAddr(peer)
	if err != nil || !p.trusts(peerAddr) {
		return peer
	}
	var hops []string
	for _, line := range r.Header.Values("X-Forwarded-For") {
		for hop := range strings.SplitSeq(line, ",") {
			hops = append(hops, strings.TrimSpace(hop))
		}
	}
	for i, hop := range slices.Backward(hops) {
		addr, ok := parseHop(hop)
		if !ok {
			return peer
		}
		if !p.trusts(addr) || i == 0 {
			return addr.Unmap().WithZone("").String()
		}
	}
	return peer
}

// parseHop reads an address as proxies write it: bare, with a port, or an IPv6
// address in brackets.
func parseHop(hop string) (netip.Addr, bool) {
	if addr, err := netip.ParseAddr(hop); err == nil {
		return addr, true
	}
	if ap, err := netip.ParseAddrPort(hop); err == nil {
		return ap.Addr(), true
	}
	if strings.HasPrefix(hop, "[") && strings.HasSuffix(hop, "]") {
		if addr, err := netip.ParseAddr(hop[1 : len(hop)-1]); err == nil {
			return addr, true
		}
	}
	return netip.Addr{}, false
}
