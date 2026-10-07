package probe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	// maxDownload is how much of a file is taken: the speed is measured on it.
	maxDownload = 8 << 20
	// minMeasurable is the least that tells a speed: less is over before it starts.
	minMeasurable = 256 << 10
)

// parseDownload reads a URL and the least speed that is good, in megabits per
// second (any when empty).
func parseDownload(target, expect string) (spec httpSpec, minMbps float64, err error) {
	spec, err = parseHTTP(target, "")
	if err != nil {
		return spec, 0, err
	}
	if expect != "" {
		minMbps, err = strconv.ParseFloat(expect, 64)
		if err != nil || minMbps <= 0 || minMbps > 100000 {
			return spec, 0, fmt.Errorf("expect %q: want the least speed in megabits per second, such as 5", expect)
		}
	}
	return spec, minMbps, nil
}

// Download takes up to 8 MiB of a file. The time is the whole download. With
// expect, it fails when the speed is lower, in megabits per second.
func (p *Prober) Download(ctx context.Context, target, expect string) Outcome {
	spec, minMbps, err := parseDownload(target, expect)
	if err != nil {
		return failure(0, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.target, nil)
	if err != nil {
		return failure(0, err)
	}
	// The client of the prober has the timeout of a request; a body takes longer.
	client := *p.client
	client.Timeout = 0
	ctx, cancel := context.WithDeadline(ctx, p.deadline(ctx).Add(20*time.Second))
	defer cancel()
	req = req.WithContext(ctx)

	start := time.Now()
	res, err := client.Do(req)
	if err != nil {
		return failure(time.Since(start), err)
	}
	defer res.Body.Close()
	if !spec.statusOK(res.StatusCode) {
		return Outcome{RTT: time.Since(start), Err: "HTTP " + res.Status}
	}
	n, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxDownload))
	rtt := time.Since(start)
	if err != nil {
		return failure(rtt, err)
	}
	if minMbps == 0 {
		return Outcome{OK: true, RTT: rtt}
	}
	if n < minMeasurable {
		return Outcome{RTT: rtt, Err: fmt.Sprintf("the file is %d bytes: too small to measure a speed", n)}
	}
	mbps := float64(n) * 8 / rtt.Seconds() / 1e6
	if mbps < minMbps {
		return Outcome{RTT: rtt, Err: fmt.Sprintf("%.1f Mbit/s, under %g", mbps, minMbps)}
	}
	return Outcome{OK: true, RTT: rtt}
}
