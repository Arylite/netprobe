package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"
)

// defaultCertDays is how long a certificate must still be valid.
const defaultCertDays = 14

// parseTLS reads "host[:port]" (port 443) and the days a certificate must
// still be valid for.
func parseTLS(target, expect string) (addr string, days int, err error) {
	addr, err = splitHostPort(target, "443")
	if err != nil {
		return "", 0, err
	}
	days = defaultCertDays
	if expect != "" {
		days, err = strconv.Atoi(expect)
		if err != nil || days < 0 || days > 3650 {
			return "", 0, fmt.Errorf("expect %q: want the number of days a certificate must still be valid", expect)
		}
	}
	return addr, days, nil
}

// TLS measures the time to open a connection and finish the handshake. It fails
// when the certificate is not trusted, does not match the name, or expires in
// fewer days than expect (14 by default).
func (p *Prober) TLS(ctx context.Context, target, expect string) Outcome {
	addr, days, err := parseTLS(target, expect)
	if err != nil {
		return failure(0, err)
	}
	host, _, _ := net.SplitHostPort(addr)
	d := tls.Dialer{
		NetDialer: p.dialer,
		Config:    &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: p.roots},
	}
	ctx, cancel := context.WithDeadline(ctx, p.deadline(ctx))
	defer cancel()

	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", addr)
	rtt := time.Since(start)
	if err != nil {
		return failure(rtt, err)
	}
	defer conn.Close()
	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return Outcome{RTT: rtt, Err: "no certificate"}
	}
	end := state.PeerCertificates[0].NotAfter
	left := time.Until(end)
	if left < time.Duration(days)*24*time.Hour {
		return Outcome{RTT: rtt, Err: fmt.Sprintf("the certificate expires in %d days (%s)", int(left.Hours()/24), end.UTC().Format("2006-01-02"))}
	}
	return Outcome{OK: true, RTT: rtt}
}
