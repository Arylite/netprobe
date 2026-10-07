package probe

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	policy  Policy
	timeout time.Duration
	dialer  *net.Dialer
	client  *http.Client
	roots   *x509.CertPool // nil: the roots of the system
	rdap    string         // where domains are looked up; empty is rdap.org
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
		policy:  policy,
		timeout: timeout,
		dialer:  dialer,
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
	return p.httpCheck(ctx, target, "")
}

// maxBodyScan is how much of a body is searched for the text of an expect.
const maxBodyScan = 256 << 10

// httpSpec is what a good answer is: statuses (an exact code, or a class such
// as 2xx), and texts the body holds or must not hold. Without a status, any
// below 400 will do.
type httpSpec struct {
	target   string
	statuses []string
	contains []string
	absent   []string
}

// parseHTTP reads an expect such as "200;contains:ok;absent:error".
func parseHTTP(target, expect string) (httpSpec, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return httpSpec{}, fmt.Errorf("target %q: want http(s)://host[/path]", target)
	}
	spec := httpSpec{target: target}
	for _, item := range strings.Split(expect, ";") {
		item = strings.TrimSpace(item)
		switch {
		case item == "":
		case strings.HasPrefix(item, "contains:"):
			spec.contains = append(spec.contains, strings.TrimPrefix(item, "contains:"))
		case strings.HasPrefix(item, "absent:"):
			spec.absent = append(spec.absent, strings.TrimPrefix(item, "absent:"))
		case validStatus(item):
			spec.statuses = append(spec.statuses, item)
		default:
			return httpSpec{}, fmt.Errorf("expect %q: want a status (200 or 2xx), contains:TEXT or absent:TEXT, separated by ;", item)
		}
	}
	return spec, nil
}

func validStatus(s string) bool {
	if len(s) != 3 {
		return false
	}
	if strings.HasSuffix(s, "xx") {
		return s[0] >= '1' && s[0] <= '5'
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 100 && n <= 599
}

// statusOK reports whether code is a good status for the spec.
func (h httpSpec) statusOK(code int) bool {
	if len(h.statuses) == 0 {
		return code < http.StatusBadRequest
	}
	for _, s := range h.statuses {
		if strings.HasSuffix(s, "xx") && int(s[0]-'0') == code/100 {
			return true
		}
		if n, err := strconv.Atoi(s); err == nil && n == code {
			return true
		}
	}
	return false
}

// followsRedirects is false when a redirect is itself what is expected.
func (h httpSpec) followsRedirects() bool {
	for _, s := range h.statuses {
		if s[0] == '3' {
			return false
		}
	}
	return true
}

func (p *Prober) httpCheck(ctx context.Context, target, expect string) Outcome {
	spec, err := parseHTTP(target, expect)
	if err != nil {
		return failure(0, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return failure(0, err)
	}
	client := p.client
	if !spec.followsRedirects() {
		c := *p.client
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &c
	}
	start := time.Now()
	res, err := client.Do(req)
	rtt := time.Since(start)
	if err != nil {
		return failure(rtt, err)
	}
	defer res.Body.Close()
	if !spec.statusOK(res.StatusCode) {
		return Outcome{RTT: rtt, Err: "HTTP " + res.Status}
	}
	if len(spec.contains) == 0 && len(spec.absent) == 0 {
		return Outcome{OK: true, RTT: rtt}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBodyScan))
	if err != nil {
		return failure(rtt, err)
	}
	for _, t := range spec.contains {
		if !strings.Contains(string(body), t) {
			return Outcome{RTT: rtt, Err: fmt.Sprintf("the body does not hold %q", t)}
		}
	}
	for _, t := range spec.absent {
		if strings.Contains(string(body), t) {
			return Outcome{RTT: rtt, Err: fmt.Sprintf("the body holds %q", t)}
		}
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
