//go:build linux

package probe

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"golang.org/x/sys/unix"
)

const (
	basePort    = 33434 // where traceroute has always started
	icmpOrigin  = 2     // SO_EE_ORIGIN_ICMP
	icmpUnreach = 3     // destination unreachable
)

var unreachable4 = map[uint8]string{
	0: "network unreachable", 1: "host unreachable", 2: "protocol unreachable",
	4: "fragmentation needed", 9: "network prohibited", 10: "host prohibited", 13: "administratively prohibited",
}

// answer is one ICMP error read from the queue of the socket.
type answer struct {
	from netip.Addr
	typ  uint8
	code uint8
	port int // the destination port of the packet it answers; 0 when unknown
}

// trace sends UDP packets to the destination with a time to live of 1, 2, ...
// Each router that drops one answers with an ICMP error, which the kernel keeps
// for the socket (IP_RECVERR) and which needs no privilege to read. The
// destination answers with a port unreachable.
func trace(ctx context.Context, dst netip.Addr, maxHops int) (route, error) {
	var r route
	if !dst.Is4() {
		return r, errors.New("traceroute is for IPv4 addresses")
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return r, fmt.Errorf("traceroute is not allowed here: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_RECVERR, 1); err != nil {
		return r, fmt.Errorf("traceroute is not allowed here: %w", err)
	}

	to := &unix.SockaddrInet4{Addr: dst.As4()}
	silent := 0
	for ttl := 1; ttl <= maxHops; ttl++ {
		if ctx.Err() != nil {
			r.problem = fmt.Sprintf("gave up at hop %d: out of time", ttl)
			return r, nil
		}
		drain(fd)
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_TTL, ttl); err != nil {
			return r, err
		}
		to.Port = basePort + ttl
		start := time.Now()
		if err := unix.Sendto(fd, []byte{0}, 0, to); err != nil {
			return r, fmt.Errorf("send: %w", err)
		}
		a, ok := wait(ctx, fd, perHop, to.Port)
		rtt := time.Since(start)
		if !ok {
			if silent++; silent >= darkHops {
				r.problem = fmt.Sprintf("the path goes dark after hop %d (%s)", ttl-silent, lastSeen(r))
				return r, nil
			}
			continue
		}
		silent = 0
		r.hops, r.last, r.rtt = ttl, a.from, rtt
		switch {
		case a.typ == icmpUnreach && a.code == 3 && a.from == dst:
			r.reached = true
			return r, nil
		case a.typ == icmpUnreach:
			why, known := unreachable4[a.code]
			if !known {
				why = fmt.Sprintf("unreachable, code %d", a.code)
			}
			r.problem = fmt.Sprintf("%s: %s at hop %d", a.from, why, ttl)
			return r, nil
		}
	}
	return r, nil
}

func lastSeen(r route) string {
	if r.hops == 0 {
		return "no router answered"
	}
	return "last answer from " + r.last.String()
}

// wait blocks until the socket has an ICMP error for the packet sent to port, or
// the time is up. The late answer of an earlier hop is not the answer of this
// one: each hop has its own port, which the error carries back.
func wait(ctx context.Context, fd int, timeout time.Duration, port int) (answer, bool) {
	if d, ok := ctx.Deadline(); ok && time.Until(d) < timeout {
		timeout = time.Until(d)
	}
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return answer{}, false
		}
		fds := []unix.PollFd{{Fd: int32(fd)}}
		n, err := unix.Poll(fds, int(left.Milliseconds())+1)
		if err != nil && err != unix.EINTR {
			return answer{}, false
		}
		if n > 0 {
			if a, ok := readError(fd); ok && (a.port == 0 || a.port == port) {
				return a, true
			}
		}
	}
}

// drain empties what an earlier hop left in the error queue.
func drain(fd int) {
	for {
		if _, ok := readError(fd); !ok {
			break
		}
	}
	_, _ = unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
}

// readError takes one ICMP error from the queue, without waiting.
func readError(fd int) (answer, bool) {
	buf, oob := make([]byte, 1), make([]byte, 512)
	_, oobn, _, from, err := unix.Recvmsg(fd, buf, oob, unix.MSG_ERRQUEUE|unix.MSG_DONTWAIT)
	if err != nil {
		return answer{}, false
	}
	msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return answer{}, false
	}
	// The name of the message is where the packet that failed was going.
	port := 0
	if sa, ok := from.(*unix.SockaddrInet4); ok {
		port = sa.Port
	}
	for _, m := range msgs {
		// struct sock_extended_err, then the sockaddr_in of the router.
		if m.Header.Level != unix.IPPROTO_IP || m.Header.Type != unix.IP_RECVERR || len(m.Data) < 24 {
			continue
		}
		if m.Data[4] != icmpOrigin {
			continue
		}
		var ip [4]byte
		copy(ip[:], m.Data[20:24])
		return answer{from: netip.AddrFrom4(ip), typ: m.Data[5], code: m.Data[6], port: port}, true
	}
	return answer{}, false
}
