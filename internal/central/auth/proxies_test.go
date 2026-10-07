package auth

import (
	"net/http"
	"testing"
)

func request(peer string, xff ...string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = peer
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestClientIPWithoutTrustedProxiesIsThePeer(t *testing.T) {
	var none Proxies
	if got := none.ClientIP(request("203.0.113.9:5555", "198.51.100.1")); got != "203.0.113.9" {
		t.Fatalf("got %s", got)
	}
}

func TestClientIPBelievesOnlyWhatATrustedProxyAdded(t *testing.T) {
	p, err := ParseProxies([]string{"10.0.0.0/8", "192.0.2.7", "2001:db8::/32"})
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		r    *http.Request
		want string
	}{
		"a client behind one proxy":                 {request("10.0.0.5:443", "198.51.100.1"), "198.51.100.1"},
		"a chain of proxies":                        {request("10.0.0.5:443", "198.51.100.1, 10.0.0.9"), "198.51.100.1"},
		"several header lines":                      {request("10.0.0.5:443", "198.51.100.1", "10.0.0.9"), "198.51.100.1"},
		"a forged first hop is not the client":      {request("10.0.0.5:443", "1.2.3.4, 198.51.100.1"), "198.51.100.1"},
		"the sender is not a proxy: header ignored": {request("203.0.113.9:5555", "198.51.100.1"), "203.0.113.9"},
		"a single trusted address":                  {request("192.0.2.7:1", "198.51.100.2"), "198.51.100.2"},
		"ipv6 proxy and client":                     {request("[2001:db8::1]:443", "2001:4860::1"), "2001:4860::1"},
		"a port after the address":                  {request("10.0.0.5:443", "198.51.100.1:51000"), "198.51.100.1"},
		"an ipv6 in brackets":                       {request("10.0.0.5:443", "[2001:4860::1]"), "2001:4860::1"},
		"an ipv4 as ipv6":                           {request("10.0.0.5:443", "::ffff:198.51.100.1"), "198.51.100.1"},
		"no header: the proxy itself":               {request("10.0.0.5:443"), "10.0.0.5"},
		"garbage from a proxy: the proxy":           {request("10.0.0.5:443", "not-an-address"), "10.0.0.5"},
		"garbage among the hops: the proxy":         {request("10.0.0.5:443", "198.51.100.1, garbage, 10.0.0.9"), "10.0.0.5"},
		"every hop trusted: the first one":          {request("10.0.0.5:443", "10.1.1.1, 10.0.0.9"), "10.1.1.1"},
	} {
		if got := p.ClientIP(c.r); got != c.want {
			t.Errorf("%s: got %s, want %s", name, got, c.want)
		}
	}
}

func TestParseProxiesRefusesWhatWouldTrustEveryone(t *testing.T) {
	for _, bad := range [][]string{{"0.0.0.0/0"}, {"::/0"}, {"nonsense"}, {"10.0.0.0/33"}, {""}} {
		if _, err := ParseProxies(bad); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
	if p, err := ParseProxies(nil); err != nil || len(p.prefixes) != 0 {
		t.Fatalf("no proxy: %v, %v", p, err)
	}
}
