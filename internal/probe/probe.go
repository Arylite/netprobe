package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

const maxRedirects = 5

// Outcome is the result of one measurement.
type Outcome struct {
	OK  bool
	RTT time.Duration
	Err string
}

// Prober runs measurements. Every connection it opens goes through the policy.
type Prober struct {
	dialer *net.Dialer
	client *http.Client
}

// New builds a prober whose measurements stop after timeout.
func New(policy Policy, timeout time.Duration) *Prober {
	dialer := &net.Dialer{Timeout: timeout, Control: policy.Control}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		DisableKeepAlives:     true, // each probe measures a fresh connection
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		// No Proxy: a proxy from the environment would bypass the policy.
	}
	return &Prober{
		dialer: dialer,
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

// TCP measures the time to open a connection to host:port.
func (p *Prober) TCP(ctx context.Context, target string) Outcome {
	if _, _, err := net.SplitHostPort(target); err != nil {
		return failure(0, fmt.Errorf("target %q: want host:port", target))
	}
	start := time.Now()
	conn, err := p.dialer.DialContext(ctx, "tcp", target)
	rtt := time.Since(start)
	if err != nil {
		return failure(rtt, err)
	}
	_ = conn.Close()
	return Outcome{OK: true, RTT: rtt}
}

// HTTP measures the time until the response headers of a GET arrive. A status
// of 400 or more is a failure.
func (p *Prober) HTTP(ctx context.Context, target string) Outcome {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return failure(0, fmt.Errorf("target %q: want http(s)://host[/path]", target))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return failure(0, err)
	}
	start := time.Now()
	res, err := p.client.Do(req)
	rtt := time.Since(start)
	if err != nil {
		return failure(rtt, err)
	}
	_ = res.Body.Close()
	if res.StatusCode >= http.StatusBadRequest {
		return Outcome{RTT: rtt, Err: "HTTP " + res.Status}
	}
	return Outcome{OK: true, RTT: rtt}
}

// failure describes an error without the URL it carries, which may hold
// credentials.
func failure(rtt time.Duration, err error) Outcome {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return Outcome{RTT: rtt, Err: err.Error()}
}
