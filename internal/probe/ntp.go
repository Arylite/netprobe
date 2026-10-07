package probe

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"
)

const (
	defaultNTPOffset = time.Second
	ntpPacketSize    = 48
)

// ntpEpoch is the start of the NTP era 0: 1900-01-01.
var ntpEpoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// parseNTP reads "host[:port]" (port 123) and the offset of the clock that is
// tolerated, a duration such as 500ms (1s by default).
func parseNTP(target, expect string) (addr string, maxOffset time.Duration, err error) {
	addr, err = splitHostPort(target, "123")
	if err != nil {
		return "", 0, err
	}
	maxOffset = defaultNTPOffset
	if expect != "" {
		maxOffset, err = time.ParseDuration(expect)
		if err != nil || maxOffset <= 0 || maxOffset > time.Hour {
			return "", 0, fmt.Errorf("expect %q: want the offset of the clock that is tolerated, such as 500ms", expect)
		}
	}
	return addr, maxOffset, nil
}

// NTP asks a time server for the time. The time is the round trip. It fails
// when the clock of this machine is further than expect from the server's, so
// it also tells when the edge itself has drifted.
func (p *Prober) NTP(ctx context.Context, target, expect string) Outcome {
	addr, maxOffset, err := parseNTP(target, expect)
	if err != nil {
		return failure(0, err)
	}
	ctx, cancel := context.WithDeadline(ctx, p.deadline(ctx))
	defer cancel()

	start := time.Now()
	conn, err := p.dialer.DialContext(ctx, "udp", addr)
	if err != nil {
		return failure(time.Since(start), err)
	}
	defer conn.Close()
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}

	req := make([]byte, ntpPacketSize)
	req[0] = 0x23 // version 4, mode 3 (client)
	t1 := time.Now()
	binary.BigEndian.PutUint64(req[40:], toNTP(t1)) // transmit time, echoed back as the origin
	if _, err := conn.Write(req); err != nil {
		return failure(time.Since(start), err)
	}
	resp := make([]byte, ntpPacketSize)
	n, err := conn.Read(resp)
	t4 := time.Now()
	rtt := t4.Sub(t1)
	if err != nil {
		return failure(rtt, err)
	}
	if n < ntpPacketSize {
		return Outcome{RTT: rtt, Err: "the answer is too short"}
	}
	if mode := resp[0] & 7; mode != 4 && mode != 5 {
		return Outcome{RTT: rtt, Err: "that is not the answer of a time server"}
	}
	if resp[1] == 0 {
		return Outcome{RTT: rtt, Err: "the server refuses to say the time (kiss-of-death)"}
	}
	if binary.BigEndian.Uint64(resp[24:]) != toNTP(t1) {
		return Outcome{RTT: rtt, Err: "the answer is not to this request"}
	}
	t2 := fromNTP(binary.BigEndian.Uint64(resp[32:]))
	t3 := fromNTP(binary.BigEndian.Uint64(resp[40:]))
	if t3.IsZero() || t3.Before(ntpEpoch.AddDate(10, 0, 0)) {
		return Outcome{RTT: rtt, Err: "the server does not know the time yet"}
	}
	offset := (t2.Sub(t1) + t3.Sub(t4)) / 2
	if offset.Abs() > maxOffset {
		return Outcome{RTT: rtt, Err: fmt.Sprintf("the clock is %s %s the server's", offset.Abs().Round(time.Millisecond), aheadOrBehind(offset))}
	}
	return Outcome{OK: true, RTT: rtt}
}

// aheadOrBehind says where the clock of this machine is, given the offset of
// the server from it.
func aheadOrBehind(offset time.Duration) string {
	if offset > 0 {
		return "behind"
	}
	return "ahead of"
}

func toNTP(t time.Time) uint64 {
	d := t.Sub(ntpEpoch)
	sec := uint64(d / time.Second)
	frac := uint64(d%time.Second) << 32 / uint64(time.Second)
	return sec<<32 | frac
}

func fromNTP(v uint64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	sec := int64(v >> 32)
	frac := int64(v&0xffffffff) * int64(time.Second) >> 32
	return ntpEpoch.Add(time.Duration(sec)*time.Second + time.Duration(frac))
}
