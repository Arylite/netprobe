package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	pingCount    = 4
	pingSpacing  = 200 * time.Millisecond
	defaultLoss  = 50
	pingPayload  = "netprobe-ping-netprobe-ping-1"
	pingMaxWaitS = 2 * time.Second
)

// parsePing reads a host or an address, and the share of echoes that may be
// lost, in percent (50 by default).
func parsePing(target, expect string) (host string, maxLoss int, err error) {
	if target == "" || strings.ContainsAny(target, " \t/@") || strings.Contains(target, "://") {
		return "", 0, fmt.Errorf("target %q: want a host name or an address", target)
	}
	host = strings.Trim(target, "[]")
	maxLoss = defaultLoss
	if expect != "" {
		maxLoss, err = strconv.Atoi(expect)
		if err != nil || maxLoss < 0 || maxLoss > 100 {
			return "", 0, fmt.Errorf("expect %q: want the share of echoes that may be lost, from 0 to 100", expect)
		}
	}
	return host, maxLoss, nil
}

// Ping sends four ICMP echoes. It fails when more than expect percent of them
// are lost (50 by default); the time is the mean of the answers.
//
// It needs no privilege on Linux when the group of the process may open ping
// sockets (net.ipv4.ping_group_range), and otherwise a raw socket.
func (p *Prober) Ping(ctx context.Context, target, expect string) Outcome {
	host, maxLoss, err := parsePing(target, expect)
	if err != nil {
		return failure(0, err)
	}
	ctx, cancel := context.WithDeadline(ctx, p.deadline(ctx))
	defer cancel()

	ip, err := resolveOne(ctx, host)
	if err != nil {
		return failure(0, err)
	}
	if p.policy.Denies(ip) {
		return failure(0, fmt.Errorf("connection to %s is denied by the policy", ip))
	}
	rtts, err := echo(ctx, ip)
	if err != nil {
		return failure(0, err)
	}
	lost := pingCount - len(rtts)
	if len(rtts) == 0 {
		if maxLoss < 100 {
			return Outcome{Err: fmt.Sprintf("no answer from %s", ip)}
		}
		return Outcome{OK: true}
	}
	var sum time.Duration
	for _, r := range rtts {
		sum += r
	}
	mean := sum / time.Duration(len(rtts))
	if lost*100/pingCount > maxLoss {
		return Outcome{RTT: mean, Err: fmt.Sprintf("%d of %d echoes lost", lost, pingCount)}
	}
	return Outcome{OK: true, RTT: mean}
}

// resolveOne turns a name into one address, an IPv4 one when there is any.
func resolveOne(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap(), nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, err
	}
	for _, ip := range ips {
		if ip.Unmap().Is4() {
			return ip.Unmap(), nil
		}
	}
	if len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("no address for %s", host)
	}
	return ips[0], nil
}

// pingSeq makes the identifier of every run of echoes its own, so that runs
// sharing the process do not take each other's answers for theirs.
var pingSeq atomic.Uint32

// pingConn is what echo needs of an ICMP socket.
type pingConn interface {
	ReadFrom(b []byte) (int, net.Addr, error)
	WriteTo(b []byte, dst net.Addr) (int, error)
	SetReadDeadline(t time.Time) error
	Close() error
}

// echo sends the echoes and returns the round trip of each answer.
func echo(ctx context.Context, ip netip.Addr) ([]time.Duration, error) {
	conn, datagram, err := listenICMP(ip.Is6())
	if err != nil {
		return nil, fmt.Errorf("ping is not allowed here: %w (see the guide on checks)", err)
	}
	defer func() { _ = conn.Close() }()
	return exchange(ctx, conn, datagram, ip)
}

// exchange runs the echoes over an open socket.
func exchange(ctx context.Context, conn pingConn, datagram bool, ip netip.Addr) ([]time.Duration, error) {
	v6 := ip.Is6()
	var dst net.Addr = &net.IPAddr{IP: ip.AsSlice()}
	if datagram {
		dst = &net.UDPAddr{IP: ip.AsSlice()}
	}
	// A raw socket hears every echo reply of the host, those of the other checks
	// too: the identifier tells the runs apart, and the source the targets.
	id := int(pingSeq.Add(1)+uint32(os.Getpid())) & 0xffff

	// sentAt and the rest are written by the sender and read by the reader.
	var mu sync.Mutex
	sentAt := make([]time.Time, pingCount)
	var sendErr error
	go func() {
		for seq := range pingCount {
			msg := icmp.Message{
				Type: ipv4.ICMPTypeEcho,
				Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte(pingPayload)},
			}
			if v6 {
				msg.Type = ipv6.ICMPTypeEchoRequest
			}
			b, err := msg.Marshal(nil)
			if err != nil {
				return
			}
			mu.Lock()
			sentAt[seq] = time.Now()
			mu.Unlock()
			if _, err := conn.WriteTo(b, dst); err != nil {
				mu.Lock()
				sendErr = err
				first := seq == 0
				mu.Unlock()
				if first {
					_ = conn.Close() // nothing is on its way: wake the reader now
				}
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(pingSpacing):
			}
		}
	}()

	end := time.Now().Add(pingCount*pingSpacing + pingMaxWaitS)
	if d, ok := ctx.Deadline(); ok && d.Before(end) {
		end = d
	}
	proto, want := 1, icmp.Type(ipv4.ICMPTypeEchoReply)
	if v6 {
		proto, want = 58, ipv6.ICMPTypeEchoReply
	}
	rtts := make([]time.Duration, 0, pingCount)
	seen := make([]bool, pingCount)
	buf := make([]byte, 1500)
	for len(rtts) < pingCount {
		if err := conn.SetReadDeadline(end); err != nil {
			return rtts, sentOr(&mu, &sendErr, err)
		}
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			return rtts, sentOr(&mu, &sendErr, err)
		}
		now := time.Now()
		if src, ok := addrOf(from); !ok || src != ip.WithZone("") {
			continue
		}
		m, err := icmp.ParseMessage(proto, buf[:n])
		if err != nil || m.Type != want {
			continue
		}
		reply, ok := m.Body.(*icmp.Echo)
		if !ok || reply.Seq < 0 || reply.Seq >= pingCount || seen[reply.Seq] || string(reply.Data) != pingPayload {
			continue
		}
		// A ping socket gives the echo its own identifier, and the kernel keeps
		// the other sockets' answers out: only a raw socket needs the check.
		if !datagram && reply.ID != id {
			continue
		}
		mu.Lock()
		sent := sentAt[reply.Seq]
		mu.Unlock()
		if sent.IsZero() {
			continue // an answer to an echo that was not sent yet is not an answer to ours
		}
		seen[reply.Seq] = true
		rtts = append(rtts, now.Sub(sent))
	}
	if len(rtts) == 0 {
		return rtts, sentOr(&mu, &sendErr, nil)
	}
	return rtts, nil
}

// sentOr prefers the error of the sender, the cause, to the one that it made
// the reader see.
func sentOr(mu *sync.Mutex, sendErr *error, err error) error {
	mu.Lock()
	defer mu.Unlock()
	if *sendErr != nil {
		return fmt.Errorf("sending the echo: %w", *sendErr)
	}
	return err
}

// addrOf reads the address a packet came from.
func addrOf(a net.Addr) (netip.Addr, bool) {
	var raw net.IP
	switch a := a.(type) {
	case *net.IPAddr:
		raw = a.IP
	case *net.UDPAddr:
		raw = a.IP
	}
	ip, ok := netip.AddrFromSlice(raw)
	return ip.Unmap(), ok
}

// listenICMP opens a ping socket, or a raw one when that is not allowed.
func listenICMP(v6 bool) (conn *icmp.PacketConn, datagram bool, err error) {
	dgram, raw, addr := "udp4", "ip4:icmp", "0.0.0.0"
	if v6 {
		dgram, raw, addr = "udp6", "ip6:ipv6-icmp", "::"
	}
	conn, err = icmp.ListenPacket(dgram, addr)
	if err == nil {
		return conn, true, nil
	}
	conn, rawErr := icmp.ListenPacket(raw, addr)
	if rawErr == nil {
		return conn, false, nil
	}
	return nil, false, err
}
