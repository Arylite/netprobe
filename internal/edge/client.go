package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/Arylite/netprobe/internal/api"
)

const (
	requestTimeout = 15 * time.Second
	maxBodyBytes   = 1 << 20
)

// Client talks to the edge API of the central.
type Client struct {
	base *url.URL
	http *http.Client
	etag string
}

// NewClient checks the central URL and prepares a client.
func NewClient(central string) (*Client, error) {
	u, err := url.Parse(central)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("central URL %q: want http(s)://host[:port]", central)
	}
	return &Client{base: u, http: &http.Client{Timeout: requestTimeout}}, nil
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
	res, err := c.http.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("get assignments: %w", err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusNotModified:
		return nil, false, nil
	case http.StatusOK:
	default:
		return nil, false, fmt.Errorf("get assignments: %s", res.Status)
	}

	var body api.Assignments
	if err := json.NewDecoder(io.LimitReader(res.Body, maxBodyBytes)).Decode(&body); err != nil {
		return nil, false, fmt.Errorf("decode assignments: %w", err)
	}
	for _, check := range body.Checks {
		if err := check.Validate(); err != nil {
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
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.JoinPath(api.PathResults).String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("post results: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("post results: %s", res.Status)
	}
	return nil
}
