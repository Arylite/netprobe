package edge

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

const (
	requestTimeout = 15 * time.Second
	maxBodyBytes   = 1 << 20
)

// ErrUnauthorized is returned when the central refuses the token.
var ErrUnauthorized = errors.New("the central refused the token")

// StatusError is an answer of the central that is neither a success nor an
// authentication failure.
type StatusError struct {
	Op     string
	Code   int
	Status string
}

func (e *StatusError) Error() string { return e.Op + ": " + e.Status }

// BatchError is a batch that cannot be sent as it is, because it is invalid or
// cannot be encoded. Sending it again can only fail again.
type BatchError struct{ Err error }

func (e *BatchError) Error() string { return e.Err.Error() }
func (e *BatchError) Unwrap() error { return e.Err }

// Client talks to the edge API of the central.
type Client struct {
	base  *url.URL
	token string
	http  *http.Client
	etag  string
}

// ClientOption changes how a Client talks to the central.
type ClientOption func(*Client)

// WithTLS sets the TLS settings of the connection, for a private CA or a client
// certificate.
func WithTLS(cfg *tls.Config) ClientOption {
	return func(c *Client) {
		if cfg == nil {
			return
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = cfg
		c.http.Transport = transport
	}
}

// NewClient checks the central URL and prepares a client. A token is never
// sent over plain HTTP unless the central is on this machine.
func NewClient(central, token string, opts ...ClientOption) (*Client, error) {
	u, err := url.Parse(central)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("central URL %q: want http(s)://host[:port]", central)
	}
	if token != "" && u.Scheme == "http" && !isLoopback(u.Hostname()) {
		return nil, errors.New("refusing to send the token over plain HTTP to a remote central: use https")
	}
	c := &Client{base: u, token: token, http: &http.Client{Timeout: requestTimeout}}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.http.Do(req)
}

// Assignments fetches the checks to run. changed is false when the central
// answered that nothing differs from the previous call.
func (c *Client) Assignments(ctx context.Context) (checks []api.Check, changed bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.JoinPath(api.PathAssignments).String(), nil)
	if err != nil {
		return nil, false, err
	}
	if c.etag != "" {
		req.Header.Set("If-None-Match", c.etag)
	}
	res, err := c.do(req)
	if err != nil {
		return nil, false, fmt.Errorf("get assignments: %w", err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusNotModified:
		return nil, false, nil
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, false, fmt.Errorf("get assignments: %w", ErrUnauthorized)
	default:
		return nil, false, fmt.Errorf("get assignments: %s", res.Status)
	}

	var body api.Assignments
	if err := json.NewDecoder(io.LimitReader(res.Body, maxBodyBytes)).Decode(&body); err != nil {
		return nil, false, fmt.Errorf("decode assignments: %w", err)
	}
	for _, check := range body.Checks {
		if err := check.ValidateShape(); err != nil {
			return nil, false, fmt.Errorf("assignments: %w", err)
		}
	}
	c.etag = res.Header.Get("ETag")
	return body.Checks, true, nil
}

// PostResults sends a batch of results.
func (c *Client) PostResults(ctx context.Context, results []api.Result) error {
	payload := api.ResultsRequest{Results: results}
	if err := payload.Validate(); err != nil {
		return &BatchError{Err: err}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return &BatchError{Err: fmt.Errorf("encode results: %w", err)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.JoinPath(api.PathResults).String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.do(req)
	if err != nil {
		return fmt.Errorf("post results: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("post results: %w", ErrUnauthorized)
	}
	if res.StatusCode != http.StatusNoContent {
		return &StatusError{Op: "post results", Code: res.StatusCode, Status: res.Status}
	}
	return nil
}
