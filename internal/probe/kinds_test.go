package probe

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

func ctx() context.Context { return context.Background() }

func wantFail(t *testing.T, o Outcome, text string) {
	t.Helper()
	if o.OK || !strings.Contains(o.Err, text) {
		t.Fatalf("outcome %+v, want a failure with %q", o, text)
	}
}

func wantOK(t *testing.T, o Outcome) {
	t.Helper()
	if !o.OK || o.Err != "" {
		t.Fatalf("outcome %+v, want success", o)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		kind, target, expect string
		interval             int
		errHas               string
	}{
		{"tcp", "example.com:443", "", 30, ""},
		{"tcp", "example.com", "", 30, "host:port"},
		{"tcp", "example.com:443", "x", 30, "no expect"},
		{"http", "https://example.com", "200;contains:ok;absent:error", 30, ""},
		{"http", "https://example.com", "2xx", 30, ""},
		{"http", "https://example.com", "999", 30, "expect"},
		{"http", "ftp://example.com", "", 30, "http(s)"},
		{"dns", "example.com", "", 30, ""},
		{"dns", "MX example.com@1.1.1.1", "mail", 30, ""},
		{"dns", "SRV example.com", "", 30, "type"},
		{"dns", "example.com@dns.example.com", "", 30, "address"},
		{"dns", "a b c", "", 30, "want"},
		{"tls", "example.com", "", 60, ""},
		{"tls", "example.com:8443", "30", 60, ""},
		{"tls", "example.com", "soon", 60, "days"},
		{"tls", "example.com", "", 30, "every 60 seconds at most"},
		{"icmp", "1.1.1.1", "", 5, ""},
		{"icmp", "1.1.1.1", "101", 5, "0 to 100"},
		{"icmp", "https://1.1.1.1", "", 5, "host"},
		{"icmp", "1.1.1.1", "", 1, "every 5 seconds"},
		{"ntp", "pool.ntp.org", "", 30, ""},
		{"ntp", "pool.ntp.org:123", "250ms", 30, ""},
		{"ntp", "pool.ntp.org", "later", 30, "offset"},
		{"banner", "example.com:22", "SSH-2.0", 30, ""},
		{"banner", "example.com", "", 30, "host:port"},
		{"closed", "example.com:5432", "", 30, ""},
		{"closed", "example.com:5432", "x", 30, "no expect"},
		{"closed", "example.com:99999", "", 30, "port"},
		{"download", "https://example.com/f.bin", "5", 60, ""},
		{"download", "https://example.com/f.bin", "fast", 60, "megabits"},
		{"download", "https://example.com/f.bin", "", 10, "every 60 seconds"},
	}
	for _, tt := range tests {
		c := api.Check{ID: "c", Kind: tt.kind, Target: tt.target, Expect: tt.expect, IntervalSeconds: tt.interval}
		err := Validate(c)
		switch {
		case tt.errHas == "" && err != nil:
			t.Errorf("%s %q %q: %v", tt.kind, tt.target, tt.expect, err)
		case tt.errHas != "" && (err == nil || !strings.Contains(err.Error(), tt.errHas)):
			t.Errorf("%s %q %q: error %v, want %q", tt.kind, tt.target, tt.expect, err, tt.errHas)
		}
	}
}

func TestEveryKindIsRunnable(t *testing.T) {
	for _, k := range api.Kinds {
		o := local().Run(ctx(), api.Check{ID: "c", Kind: k, Target: "%", IntervalSeconds: 60})
		if strings.Contains(o.Err, "unsupported check kind") {
			t.Errorf("kind %s has no probe", k)
		}
	}
	wantFail(t, local().Run(ctx(), api.Check{Kind: "nope"}), "unsupported")
}

func TestHTTPExpect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/moved":
			http.Redirect(w, r, "/", http.StatusFound)
		case "/gone":
			http.Error(w, "gone", http.StatusGone)
		default:
			_, _ = w.Write([]byte("all systems ok"))
		}
	}))
	defer ts.Close()

	run := func(path, expect string) Outcome {
		return local().Run(ctx(), api.Check{Kind: api.KindHTTP, Target: ts.URL + path, Expect: expect})
	}
	wantOK(t, run("/", "200"))
	wantOK(t, run("/", "2xx;contains:systems"))
	wantOK(t, run("/", "absent:error"))
	wantFail(t, run("/", "contains:down"), `does not hold "down"`)
	wantFail(t, run("/", "absent:ok"), `holds "ok"`)
	wantFail(t, run("/", "201"), "HTTP 200")
	wantOK(t, run("/gone", "410"))
	wantFail(t, run("/gone", ""), "HTTP 410")
	wantOK(t, run("/moved", "302"))
	wantOK(t, run("/moved", ""))
	wantOK(t, run("/moved", "3xx"))               // a redirect that is expected is not followed
	wantFail(t, run("/moved", "201"), "HTTP 200") // followed to the page
}

// dnsServer answers every query with one A record, or with rcode when it is not 0.
func dnsServer(t *testing.T, ip [4]byte, rcode byte) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			q := buf[:n]
			// The header, then the question as it came.
			end := 12
			for end < n && q[end] != 0 {
				end += int(q[end]) + 1
			}
			end += 5 // the zero label, the type and the class
			resp := append([]byte{}, q[:end]...)
			resp[2], resp[3] = 0x81, 0x80|rcode
			resp[6], resp[7] = 0, 0
			if rcode == 0 {
				resp[7] = 1
				resp = append(resp, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4)
				resp = append(resp, ip[:]...)
			}
			_, _ = pc.WriteTo(resp, addr)
		}
	}()
	return pc.LocalAddr().String()
}

func TestDNS(t *testing.T) {
	server := dnsServer(t, [4]byte{192, 0, 2, 7}, 0)
	run := func(target, expect string) Outcome {
		return local().Run(ctx(), api.Check{Kind: api.KindDNS, Target: target, Expect: expect})
	}
	wantOK(t, run("A example.test@"+server, ""))
	wantOK(t, run("example.test@"+server, "192.0.2.7"))
	wantFail(t, run("A example.test@"+server, "9.9.9.9"), "none of 192.0.2.7 holds")

	missing := dnsServer(t, [4]byte{}, 3)
	wantFail(t, run("A nothing.test@"+missing, ""), "")
}

func TestDNSServerFollowsThePolicy(t *testing.T) {
	server := dnsServer(t, [4]byte{192, 0, 2, 7}, 0)
	p := New(DefaultPolicy(), testTimeout)
	wantFail(t, p.Run(ctx(), api.Check{Kind: api.KindDNS, Target: "example.test@" + server}), "denied by the policy")
}

func ntpServer(t *testing.T, skew time.Duration) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 128)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil || n < ntpPacketSize {
				return
			}
			resp := make([]byte, ntpPacketSize)
			resp[0], resp[1] = 0x24, 2 // version 4, mode 4, stratum 2
			copy(resp[24:32], buf[40:48])
			now := toNTP(time.Now().Add(skew))
			binary.BigEndian.PutUint64(resp[32:], now)
			binary.BigEndian.PutUint64(resp[40:], now)
			_, _ = pc.WriteTo(resp, addr)
		}
	}()
	return pc.LocalAddr().String()
}

func TestNTP(t *testing.T) {
	run := func(addr, expect string) Outcome {
		return local().Run(ctx(), api.Check{Kind: api.KindNTP, Target: addr, Expect: expect})
	}
	wantOK(t, run(ntpServer(t, 0), ""))
	wantOK(t, run(ntpServer(t, 2*time.Second), "5s"))
	wantFail(t, run(ntpServer(t, 5*time.Second), ""), "behind the server's")
	wantFail(t, run(ntpServer(t, -5*time.Second), ""), "ahead of the server's")
}

func TestNTPRefusesWhatIsNotATimeServer(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 128)
		_, addr, _ := pc.ReadFrom(buf)
		_, _ = pc.WriteTo(make([]byte, ntpPacketSize), addr) // mode 0, stratum 0
	}()
	wantFail(t, local().Run(ctx(), api.Check{Kind: api.KindNTP, Target: pc.LocalAddr().String()}), "")
}

func TestBanner(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("SSH-2.0-OpenSSH_9.9\r\nmore"))
			c.Close()
		}
	}()
	run := func(addr, expect string) Outcome {
		return local().Run(ctx(), api.Check{Kind: api.KindBanner, Target: addr, Expect: expect})
	}
	wantOK(t, run(ln.Addr().String(), ""))
	wantOK(t, run(ln.Addr().String(), "SSH-2.0"))
	wantFail(t, run(ln.Addr().String(), "220"), "does not hold")

	quiet, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer quiet.Close()
	go func() {
		for {
			c, err := quiet.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	p := New(Policy{}, 300*time.Millisecond)
	wantFail(t, p.Run(ctx(), api.Check{Kind: api.KindBanner, Target: quiet.Addr().String()}), "says nothing")
}

func TestClosed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	run := func(target string) Outcome {
		return local().Run(ctx(), api.Check{Kind: api.KindClosed, Target: target})
	}
	wantFail(t, run(addr), "the port is open")
	ln.Close()
	wantOK(t, run(addr))
	// A name that does not resolve says nothing about the port.
	wantFail(t, run("nothing.invalid:80"), "")
	// Nor does a policy that forbids the connection.
	denied := New(DefaultPolicy(), testTimeout).Run(ctx(), api.Check{Kind: api.KindClosed, Target: addr})
	wantFail(t, denied, "denied by the policy")
}

func TestDownload(t *testing.T) {
	big := make([]byte, 1<<20)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/small" {
			_, _ = w.Write([]byte("tiny"))
			return
		}
		_, _ = w.Write(big)
	}))
	defer ts.Close()
	run := func(path, expect string) Outcome {
		return local().Run(ctx(), api.Check{Kind: api.KindDownload, Target: ts.URL + path, Expect: expect})
	}
	wantOK(t, run("/big", ""))
	wantOK(t, run("/big", "1"))
	wantFail(t, run("/big", "100000"), "Mbit/s, under")
	wantFail(t, run("/small", "1"), "too small")
	wantOK(t, run("/small", ""))
}

// certServer serves a certificate for 127.0.0.1 that ends at notAfter, and
// returns the address and a pool that trusts it.
func certServer(t *testing.T, notAfter time.Time) (string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "probe test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = c.(*tls.Conn).Handshake()
				c.Close()
			}()
		}
	}()
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return ln.Addr().String(), pool
}

func TestTLS(t *testing.T) {
	check := func(addr, expect string) api.Check {
		return api.Check{Kind: api.KindTLS, Target: addr, Expect: expect}
	}
	addr, pool := certServer(t, time.Now().Add(72*time.Hour))
	p := local()
	p.roots = pool

	wantFail(t, p.Run(ctx(), check(addr, "")), "expires in 2 days")
	wantOK(t, p.Run(ctx(), check(addr, "1")))
	wantFail(t, p.Run(ctx(), check(addr, "30")), "expires in")
	// The roots of the system do not know this authority.
	wantFail(t, local().Run(ctx(), check(addr, "")), "x509")

	expired, pool := certServer(t, time.Now().Add(-time.Minute))
	p.roots = pool
	wantFail(t, p.Run(ctx(), check(expired, "")), "expired")
}

func TestParsePing(t *testing.T) {
	host, loss, err := parsePing("[::1]", "")
	if err != nil || host != "::1" || loss != defaultLoss {
		t.Fatalf("%q %d %v", host, loss, err)
	}
	if _, _, err := parsePing("a b", ""); err == nil {
		t.Fatal("a target with a space")
	}
}

func TestICMP(t *testing.T) {
	o := local().Run(ctx(), api.Check{Kind: api.KindICMP, Target: "127.0.0.1", IntervalSeconds: 5})
	if strings.Contains(o.Err, "not allowed here") {
		t.Skip("this machine does not let the process send ICMP: " + o.Err)
	}
	wantOK(t, o)
	if o.RTT < 0 {
		t.Fatalf("round trip %v", o.RTT)
	}
	wantFail(t, New(DefaultPolicy(), testTimeout).Run(ctx(), api.Check{Kind: api.KindICMP, Target: "127.0.0.1"}), "denied by the policy")
}

func TestNTPTimeConversion(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if got := fromNTP(toNTP(now)); got.Sub(now).Abs() > time.Microsecond {
		t.Fatalf("%v became %v", now, got)
	}
}

func TestDomain(t *testing.T) {
	soon := time.Now().Add(10 * 24 * time.Hour).UTC().Format(time.RFC3339)
	later := time.Now().Add(400 * 24 * time.Hour).UTC().Format(time.RFC3339)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/domain/soon.test":
			_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2001-01-01T00:00:00Z"},{"eventAction":"expiration","eventDate":"` + soon + `"}]}`))
		case "/domain/later.test":
			_, _ = w.Write([]byte(`{"events":[{"eventAction":"expiration","eventDate":"` + later + `"}]}`))
		case "/domain/silent.test":
			_, _ = w.Write([]byte(`{"events":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	p := local()
	p.rdap = ts.URL + "/domain/"
	run := func(name, expect string) Outcome {
		return p.Run(ctx(), api.Check{Kind: api.KindDomain, Target: name, Expect: expect})
	}
	wantOK(t, run("later.test", ""))
	wantFail(t, run("soon.test", ""), "expires in 9 days")
	wantOK(t, run("soon.test", "5"))
	wantFail(t, run("later.test", "500"), "expires in")
	wantFail(t, run("silent.test", ""), "does not say")
	wantFail(t, run("unknown.test", ""), "does not know")
}

func TestValidateRouteAndDomain(t *testing.T) {
	ok := []api.Check{
		{ID: "a", Kind: api.KindRoute, Target: "example.com", Expect: "20", IntervalSeconds: 60},
		{ID: "b", Kind: api.KindDomain, Target: "example.com", Expect: "45", IntervalSeconds: 3600},
	}
	for _, c := range ok {
		if err := Validate(c); err != nil {
			t.Errorf("%s: %v", c.Kind, err)
		}
	}
	bad := []api.Check{
		{ID: "a", Kind: api.KindRoute, Target: "example.com", Expect: "99", IntervalSeconds: 60},
		{ID: "a", Kind: api.KindRoute, Target: "example.com", IntervalSeconds: 10},
		{ID: "b", Kind: api.KindDomain, Target: "localhost", IntervalSeconds: 3600},
		{ID: "b", Kind: api.KindDomain, Target: "example.com", IntervalSeconds: 60},
	}
	for _, c := range bad {
		if err := Validate(c); err == nil {
			t.Errorf("%s %q %q %d: accepted", c.Kind, c.Target, c.Expect, c.IntervalSeconds)
		}
	}
}
