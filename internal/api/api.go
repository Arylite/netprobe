package api

import (
	"errors"
	"fmt"
	"math"
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
	KindTCP  = "tcp"
	KindHTTP = "http"
)

// Limits shared by both sides.
const (
	MaxResultsPerBatch = 1000
	maxErrorLength     = 512
)

// Check is one measurement an edge runs on a schedule.
type Check struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Target          string `json:"target"`
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
	switch {
	case c.ID == "":
		return errors.New("check id is empty")
	case c.Kind != KindTCP && c.Kind != KindHTTP:
		return fmt.Errorf("check %s: unknown kind %q", c.ID, c.Kind)
	case c.Target == "":
		return fmt.Errorf("check %s: target is empty", c.ID)
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
	case len(r.Error) > maxErrorLength:
		return fmt.Errorf("result of %s: error is longer than %d bytes", r.CheckID, maxErrorLength)
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
