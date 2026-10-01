package api

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestCheckValidate(t *testing.T) {
	ok := Check{ID: "c1", Kind: KindTCP, Target: "example.com:443", IntervalSeconds: 10}
	tests := []struct {
		name   string
		mutate func(*Check)
		valid  bool
	}{
		{"valid", func(*Check) {}, true},
		{"no id", func(c *Check) { c.ID = "" }, false},
		{"unknown kind", func(c *Check) { c.Kind = "icmp" }, false},
		{"no target", func(c *Check) { c.Target = "" }, false},
		{"zero interval", func(c *Check) { c.IntervalSeconds = 0 }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ok
			tt.mutate(&c)
			if err := c.Validate(); (err == nil) != tt.valid {
				t.Fatalf("Validate() = %v, want valid=%v", err, tt.valid)
			}
		})
	}
}

func TestResultValidate(t *testing.T) {
	ok := Result{CheckID: "c1", At: time.Now(), OK: true, RTTMillis: 12.5}
	tests := []struct {
		name   string
		mutate func(*Result)
		valid  bool
	}{
		{"valid", func(*Result) {}, true},
		{"no check", func(r *Result) { r.CheckID = "" }, false},
		{"no time", func(r *Result) { r.At = time.Time{} }, false},
		{"negative rtt", func(r *Result) { r.RTTMillis = -1 }, false},
		{"nan rtt", func(r *Result) { r.RTTMillis = math.NaN() }, false},
		{"long error", func(r *Result) { r.Error = strings.Repeat("x", maxErrorLength+1) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := ok
			tt.mutate(&r)
			if err := r.Validate(); (err == nil) != tt.valid {
				t.Fatalf("Validate() = %v, want valid=%v", err, tt.valid)
			}
		})
	}
}

func TestResultsRequestBatchLimit(t *testing.T) {
	r := Result{CheckID: "c1", At: time.Now()}
	req := ResultsRequest{Results: make([]Result, MaxResultsPerBatch+1)}
	for i := range req.Results {
		req.Results[i] = r
	}
	if err := req.Validate(); err == nil {
		t.Fatal("accepted a batch over the limit")
	}
	req.Results = req.Results[:MaxResultsPerBatch]
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	in := Assignments{Checks: []Check{{ID: "c1", Kind: KindHTTP, Target: "https://example.com", IntervalSeconds: 30}}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Assignments
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Checks) != 1 || out.Checks[0] != in.Checks[0] {
		t.Fatalf("round trip: %+v", out)
	}
}
