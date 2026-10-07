package edge

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/doctor"
)

const (
	dialTimeout   = 5 * time.Second
	maxClockSkew  = 30 * time.Second
	certWarnDays  = 14
	maxShownAddrs = 3
)

// DoctorConfig is what the edge diagnostics need.
type DoctorConfig struct {
	Central string
	Token   string
	// TLS holds the private CA and the client certificate; nil means the system
	// trust store and no client certificate.
	TLS *tls.Config
}

// DoctorSteps checks, in order, everything an edge needs to work: the settings,
// the name of the central, the network path to it, its certificate, its health,
// the token and the clock.
func DoctorSteps(cfg DoctorConfig) []doctor.Step {
	var (
		client *Client
		host   string
		port   string
		date   time.Time
	)
	https := strings.HasPrefix(cfg.Central, "https://")

	return []doctor.Step{
		{Name: "config", Run: func(context.Context) doctor.Outcome {
			if cfg.Token == "" {
				return doctor.Failure("no token", "set NETPROBE_TOKEN or --token-file with the token printed by 'netprobe-central edge add'")
			}
			c, err := NewClient(cfg.Central, cfg.Token, WithTLS(cfg.TLS))
			if err != nil {
				return doctor.Failure(err.Error(), "--central takes http(s)://host[:port]; a token only travels over https, or to this machine")
			}
			client = c
			host = c.base.Hostname()
			port = c.base.Port()
			if port == "" {
				port = map[string]string{"http": "80", "https": "443"}[c.base.Scheme]
			}
			return doctor.Pass(fmt.Sprintf("central %s, token set", c.base.Redacted()))
		}},
		{Name: "dns", Run: func(ctx context.Context) doctor.Outcome {
			if ip, err := netip.ParseAddr(host); err == nil {
				return doctor.Pass("address " + ip.String() + ", no lookup needed")
			}
			addrs, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return doctor.Failure(fmt.Sprintf("cannot resolve %s: %v", host, err), "check the name in --central, and the DNS server of this machine")
			}
			return doctor.Pass(fmt.Sprintf("%s -> %s", host, strings.Join(addrs[:min(len(addrs), maxShownAddrs)], ", ")))
		}},
		{Name: "tcp", Run: func(ctx context.Context) doctor.Outcome {
			start := time.Now()
			conn, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", net.JoinHostPort(host, port))
			if err != nil {
				return doctor.Failure(fmt.Sprintf("cannot connect to %s: %v", net.JoinHostPort(host, port), err),
					"nothing listens there, a firewall blocks the port, or --central names the wrong address or port")
			}
			_ = conn.Close()
			return doctor.Pass(fmt.Sprintf("connected to %s in %d ms", net.JoinHostPort(host, port), time.Since(start).Milliseconds()))
		}},
		{Name: "tls", Run: func(ctx context.Context) doctor.Outcome {
			if !https {
				return doctor.Skipped("plain HTTP")
			}
			return checkTLS(ctx, cfg.TLS, host, port)
		}},
		{Name: "health", Run: func(ctx context.Context) doctor.Outcome {
			res, err := get(ctx, client, api.PathHealth)
			if err != nil {
				return doctor.Failure(err.Error(), "the connection works but the request does not: a proxy in between may be rewriting it")
			}
			defer res.Body.Close()
			date, _ = http.ParseTime(res.Header.Get("Date"))
			switch res.StatusCode {
			case http.StatusNoContent:
				return doctor.Pass("the central answers")
			case http.StatusServiceUnavailable:
				return doctor.Failure("the central answered 503", "its database is unreachable: run 'netprobe-central doctor' on the central")
			}
			return doctor.Failure("the central answered "+res.Status, "this may not be a netprobe central: check that --central points at its edge API, not at a proxy page")
		}},
		{Name: "auth", Run: func(ctx context.Context) doctor.Outcome {
			checks, _, err := client.Assignments(ctx)
			switch {
			case errors.Is(err, ErrUnauthorized):
				return doctor.Failure("the central refused the token", "the token is wrong, was revoked, or belongs to another central: get a new one with 'netprobe-central edge add'")
			case err != nil:
				return doctor.Failure(err.Error(), "")
			}
			return doctor.Pass(fmt.Sprintf("token accepted, %d checks assigned", len(checks)))
		}},
		{Name: "clock", Run: func(context.Context) doctor.Outcome {
			if date.IsZero() {
				return doctor.Skipped("the central sent no Date header")
			}
			skew := time.Since(date).Abs()
			if skew > maxClockSkew {
				return doctor.Warning(fmt.Sprintf("this machine is %s away from the central", skew.Round(time.Second)),
					"certificate validation and the time of results depend on the clock: enable NTP")
			}
			return doctor.Pass(fmt.Sprintf("within %s of the central", max(skew.Round(time.Second), time.Second)))
		}},
	}
}

// get makes an anonymous request with the client the agent itself uses.
func get(ctx context.Context, c *Client, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.JoinPath(path).String(), nil)
	if err != nil {
		return nil, err
	}
	return c.http.Do(req)
}

func checkTLS(ctx context.Context, base *tls.Config, host, port string) doctor.Outcome {
	conf := &tls.Config{}
	if base != nil {
		conf = base.Clone()
	}
	if conf.ServerName == "" {
		conf.ServerName = host
	}
	raw, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return doctor.Failure(err.Error(), "")
	}
	conn := tls.Client(raw, conf)
	defer func() { _ = conn.Close() }()
	if err := conn.HandshakeContext(ctx); err != nil {
		detail, hint := explainTLS(err, host)
		return doctor.Failure(detail, hint)
	}
	state := conn.ConnectionState()
	leaf := state.PeerCertificates[0]
	days := int(time.Until(leaf.NotAfter).Hours() / 24)
	detail := fmt.Sprintf("%s, certificate valid until %s (%d days)", tls.VersionName(state.Version), leaf.NotAfter.Format("2006-01-02"), days)
	if days < certWarnDays {
		return doctor.Warning(detail, "the certificate expires soon: renew it before edges lose the central")
	}
	return doctor.Pass(detail)
}

func explainTLS(err error, host string) (detail, hint string) {
	detail = "TLS handshake failed: " + err.Error()
	var verify *tls.CertificateVerificationError
	if !errors.As(err, &verify) {
		return detail, "the port does not speak TLS: use http:// if the central is not behind a TLS proxy"
	}
	var (
		unknown  x509.UnknownAuthorityError
		hostname x509.HostnameError
		invalid  x509.CertificateInvalidError
	)
	switch {
	case errors.As(verify.Err, &unknown):
		return detail, "the certificate is not signed by an authority this machine trusts: install the CA of the central, or use a public certificate"
	case errors.As(verify.Err, &hostname):
		return detail, "the certificate does not cover " + host + ": put the name it was issued for in --central"
	case errors.As(verify.Err, &invalid):
		return detail, "the certificate is expired or not yet valid: renew it, and check the clock of this machine"
	}
	return detail, ""
}
