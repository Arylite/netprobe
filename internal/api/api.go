package api

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
)

// Paths of the edge API, served by the central.
const (
	PathHealth      = "/healthz"
	PathAssignments = "/v1/assignments"
	PathResults     = "/v1/results"
)

// Kinds of check.
const (
	KindTCP      = "tcp"        // a connection opens
	KindHTTP     = "http"       // an HTTP request answers as expected
	KindDNS      = "dns"        // a name resolves
	KindTLS      = "tls"        // a certificate is valid, and for long enough
	KindICMP     = "icmp"       // ICMP echo (ping), and how many are lost
	KindNTP      = "ntp"        // a time server answers, and the clock is right
	KindBanner   = "banner"     // a service greets as expected: SSH, SMTP, FTP, ...
	KindClosed   = "closed"     // a port that must not be reachable is not
	KindDownload = "download"   // a file comes down fast enough
	KindRoute    = "traceroute" // the path to a host, and where it ends
	KindDomain   = "domain"     // a domain name that is not about to expire
)

// Kinds lists every kind of check.
var Kinds = []string{KindTCP, KindHTTP, KindDNS, KindTLS, KindICMP, KindNTP, KindBanner, KindClosed, KindDownload, KindRoute, KindDomain}

// Limits shared by both sides.
const (
	MaxResultsPerBatch = 1000
	MaxErrorLength     = 512
	MaxTargetLength    = 512
	MaxExpectLength    = 256
)

// Check is one measurement an edge runs on a schedule.
type Check struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	// Expect refines what a good answer is; each kind says how.
	Expect          string `json:"expect,omitempty"`
	IntervalSeconds int    `json:"interval_seconds"`
}

// Assignments is what the central answers to an edge poll.
type Assignments struct {
	Checks []Check `json:"checks"`
}

// Result is the outcome of one run of a check.
type Result struct {
	CheckID   string    `json:"check_id"`
	At        time.Time `json:"at"`
	OK        bool      `json:"ok"`
	RTTMillis float64   `json:"rtt_millis"`
	Error     string    `json:"error,omitempty"`
}

// ResultsRequest is a batch of results posted by an edge.
type ResultsRequest struct {
	Results []Result `json:"results"`
}

// Validate reports why a check cannot be run.
func (c Check) Validate() error {
	if c.ID != "" && !slices.Contains(Kinds, c.Kind) {
		return fmt.Errorf("check %s: unknown kind %q", c.ID, c.Kind)
	}
	return c.ValidateShape()
}

// ValidateShape is Validate for a check of a kind that may be newer than the
// reader: an edge keeps such a check, and reports it as unsupported, rather than
// refusing the whole list.
func (c Check) ValidateShape() error {
	switch {
	case c.ID == "":
		return errors.New("check id is empty")
	case c.Kind == "":
		return fmt.Errorf("check %s: kind is empty", c.ID)
	case c.Target == "":
		return fmt.Errorf("check %s: target is empty", c.ID)
	case len(c.Target) > MaxTargetLength:
		return fmt.Errorf("check %s: target is longer than %d bytes", c.ID, MaxTargetLength)
	case len(c.Expect) > MaxExpectLength:
		return fmt.Errorf("check %s: expect is longer than %d bytes", c.ID, MaxExpectLength)
	case c.IntervalSeconds < 1:
		return fmt.Errorf("check %s: interval must be at least 1 second", c.ID)
	}
	return nil
}

// Validate reports why a result must be refused.
func (r Result) Validate() error {
	switch {
	case r.CheckID == "":
		return errors.New("result check id is empty")
	case r.At.IsZero():
		return fmt.Errorf("result of %s: time is missing", r.CheckID)
	case math.IsNaN(r.RTTMillis) || math.IsInf(r.RTTMillis, 0) || r.RTTMillis < 0:
		return fmt.Errorf("result of %s: invalid round-trip time", r.CheckID)
	case len(r.Error) > MaxErrorLength:
		return fmt.Errorf("result of %s: error is longer than %d bytes", r.CheckID, MaxErrorLength)
	}
	return nil
}

// Validate reports why a batch must be refused.
func (r ResultsRequest) Validate() error {
	if len(r.Results) > MaxResultsPerBatch {
		return fmt.Errorf("batch of %d results, at most %d", len(r.Results), MaxResultsPerBatch)
	}
	for _, res := range r.Results {
		if err := res.Validate(); err != nil {
			return err
		}
	}
	return nil
}
