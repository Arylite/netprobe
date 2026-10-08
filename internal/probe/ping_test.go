package probe

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type fakePacket struct {
	b    []byte
	from net.Addr
}

// fakeConn answers the echoes it is given as a raw socket would: with replies
// from the target, and with whatever noise the test queued first.
type fakeConn struct {
	mu       sync.Mutex
	cond     *sync.Cond
	queue    []fakePacket
	deadline time.Time
	closed   bool
	target   net.Addr
	writeErr error
	answer   func(id, seq int) bool // which echoes get a reply
}

func newFakeConn(target string) *fakeConn {
	c := &fakeConn{target: &net.IPAddr{IP: net.ParseIP(target)}, answer: func(int, int) bool { return true }}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func echoBytes(typ icmp.Type, id, seq int) []byte {
	b, _ := (&icmp.Message{Type: typ, Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte(pingPayload)}}).Marshal(nil)
	return b
}

func (c *fakeConn) push(b []byte, from net.Addr) {
	c.mu.Lock()
	c.queue = append(c.queue, fakePacket{b, from})
	c.mu.Unlock()
	c.cond.Broadcast()
}

func (c *fakeConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	m, err := icmp.ParseMessage(1, b)
	if err != nil {
		return 0, err
	}
	e := m.Body.(*icmp.Echo)
	if c.answer(e.ID, e.Seq) {
		c.push(echoBytes(ipv4.ICMPTypeEchoReply, e.ID, e.Seq), c.target)
	}
	return len(b), nil
}

func (c *fakeConn) ReadFrom(b []byte) (int, net.Addr, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		if c.closed {
			return 0, nil, net.ErrClosed
		}
		if len(c.queue) > 0 {
			p := c.queue[0]
			c.queue = c.queue[1:]
			return copy(b, p.b), p.from, nil
		}
		if !c.deadline.IsZero() && !time.Now().Before(c.deadline) {
			return 0, nil, os.ErrDeadlineExceeded
		}
		// Wake up at the deadline.
		if !c.deadline.IsZero() {
			t := time.AfterFunc(time.Until(c.deadline), c.cond.Broadcast)
			c.cond.Wait()
			t.Stop()
		} else {
			c.cond.Wait()
		}
	}
}

func (c *fakeConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return nil
}

func (c *fakeConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cond.Broadcast()
	return nil
}

func TestExchangeCountsEveryAnswer(t *testing.T) {
	c := newFakeConn("192.0.2.1")
	rtts, err := exchange(ctx(), c, false, netip.MustParseAddr("192.0.2.1"))
	if err != nil || len(rtts) != pingCount {
		t.Fatalf("rtts %v, err %v, want %d answers", rtts, err, pingCount)
	}
}

// Another check pings another host from the same process: its answers, with the
// same payload and sequence, must not fill the losses of this one.
func TestExchangeIgnoresAnswersFromOtherHosts(t *testing.T) {
	c := newFakeConn("192.0.2.1")
	c.answer = func(int, int) bool { return false }
	other := &net.IPAddr{IP: net.ParseIP("198.51.100.7")}
	for seq := range pingCount {
		// Whatever the identifier, a raw socket may have heard it.
		next := int(pingSeq.Load()+1+uint32(os.Getpid())) & 0xffff
		for _, id := range []int{0, next} {
			c.push(echoBytes(ipv4.ICMPTypeEchoReply, id, seq), other)
		}
	}
	ctx, cancel := context.WithTimeout(ctx(), 400*time.Millisecond)
	defer cancel()
	rtts, err := exchange(ctx, c, false, netip.MustParseAddr("192.0.2.1"))
	if err != nil || len(rtts) != 0 {
		t.Fatalf("rtts %v, err %v, want no answer", rtts, err)
	}
}

// A late answer of an earlier run to the same host carries another identifier.
func TestExchangeIgnoresOtherRunsOfTheSameHost(t *testing.T) {
	c := newFakeConn("192.0.2.1")
	c.answer = func(int, int) bool { return false }
	pingSeq.Add(1) // a run in between
	stale := int(pingSeq.Load()+uint32(os.Getpid())) & 0xffff
	for seq := range pingCount {
		c.push(echoBytes(ipv4.ICMPTypeEchoReply, stale, seq), c.target)
	}
	ctx, cancel := context.WithTimeout(ctx(), 400*time.Millisecond)
	defer cancel()
	rtts, err := exchange(ctx, c, false, netip.MustParseAddr("192.0.2.1"))
	if err != nil || len(rtts) != 0 {
		t.Fatalf("rtts %v, err %v, want no answer", rtts, err)
	}
}

func TestExchangeReportsTheSendError(t *testing.T) {
	c := newFakeConn("192.0.2.1")
	c.writeErr = errors.New("network is unreachable")
	start := time.Now()
	_, err := exchange(ctx(), c, false, netip.MustParseAddr("192.0.2.1"))
	if err == nil || !errors.Is(err, c.writeErr) {
		t.Fatalf("err %v, want the send error", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %v: the reader should wake up at once", time.Since(start))
	}
}

func TestExchangeKeepsPartialAnswers(t *testing.T) {
	c := newFakeConn("192.0.2.1")
	c.answer = func(_, seq int) bool { return seq%2 == 0 }
	ctx, cancel := context.WithTimeout(ctx(), 1500*time.Millisecond)
	defer cancel()
	rtts, err := exchange(ctx, c, false, netip.MustParseAddr("192.0.2.1"))
	if err != nil || len(rtts) != 2 {
		t.Fatalf("rtts %v, err %v, want 2 answers", rtts, err)
	}
}
