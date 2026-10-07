package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDomainDays = 30
	rdapBase          = "https://rdap.org/domain/"
)

// parseDomain reads a domain name and the days that must remain before it
// expires (30 by default).
func parseDomain(target, expect string) (name string, days int, err error) {
	name = strings.ToLower(strings.TrimSuffix(target, "."))
	if !strings.Contains(name, ".") || len(name) > 253 || strings.ContainsAny(name, " \t/:@") {
		return "", 0, fmt.Errorf("target %q: want a domain name such as example.com", target)
	}
	days = defaultDomainDays
	if expect != "" {
		days, err = strconv.Atoi(expect)
		if err != nil || days < 0 || days > 3650 {
			return "", 0, fmt.Errorf("expect %q: want the days that must remain before the domain expires", expect)
		}
	}
	return name, days, nil
}

// Domain asks the RDAP service of the registry when a domain expires, and fails
// when it is within expect days (30 by default). The time is the whole query.
func (p *Prober) Domain(ctx context.Context, target, expect string) Outcome {
	name, days, err := parseDomain(target, expect)
	if err != nil {
		return failure(0, err)
	}
	base := p.rdap
	if base == "" {
		base = rdapBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+url.PathEscape(name), nil)
	if err != nil {
		return failure(0, err)
	}
	req.Header.Set("Accept", "application/rdap+json")
	start := time.Now()
	res, err := p.client.Do(req)
	if err != nil {
		return failure(time.Since(start), err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	rtt := time.Since(start)
	if err != nil {
		return failure(rtt, err)
	}
	if res.StatusCode == http.StatusNotFound {
		return Outcome{RTT: rtt, Err: "the registry does not know " + name}
	}
	if res.StatusCode >= http.StatusBadRequest {
		return Outcome{RTT: rtt, Err: "the RDAP service answered " + res.Status}
	}
	var doc struct {
		Events []struct {
			Action string    `json:"eventAction"`
			Date   time.Time `json:"eventDate"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return Outcome{RTT: rtt, Err: "the RDAP answer is not readable"}
	}
	for _, e := range doc.Events {
		if e.Action != "expiration" {
			continue
		}
		left := time.Until(e.Date)
		if left < time.Duration(days)*24*time.Hour {
			return Outcome{RTT: rtt, Err: fmt.Sprintf("%s expires in %d days (%s)", name, int(left.Hours()/24), e.Date.UTC().Format("2006-01-02"))}
		}
		return Outcome{OK: true, RTT: rtt}
	}
	return Outcome{RTT: rtt, Err: "the registry does not say when " + name + " expires"}
}
