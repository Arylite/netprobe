package probe

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

const maxBanner = 512

// Banner opens a connection and reads what the service says first: the line of
// an SSH, SMTP, FTP, IMAP or POP3 server, or the greeting of a database. It
// fails when nothing comes, or when expect is not in the line. The time is the
// connection and the first line.
func (p *Prober) Banner(ctx context.Context, target, expect string) Outcome {
	if err := hostPort(target, ""); err != nil {
		return failure(0, err)
	}
	ctx, cancel := context.WithDeadline(ctx, p.deadline(ctx))
	defer cancel()

	start := time.Now()
	conn, err := p.dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return failure(time.Since(start), err)
	}
	defer conn.Close()
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}
	line, err := bufio.NewReaderSize(conn, maxBanner).ReadString('\n')
	rtt := time.Since(start)
	if line == "" {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return Outcome{RTT: rtt, Err: "the service says nothing"}
		}
		return Outcome{RTT: rtt, Err: "the service closed the connection without a word"}
	}
	line = strings.TrimSpace(strings.ToValidUTF8(line, ""))
	if expect != "" && !strings.Contains(line, expect) {
		return Outcome{RTT: rtt, Err: fmt.Sprintf("the banner %q does not hold %q", clipBanner(line), expect)}
	}
	return Outcome{OK: true, RTT: rtt}
}

func clipBanner(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

// Closed checks that a port is not reachable: it is a good result when the
// connection is refused or goes unanswered, a failure when it opens. Use it for
// what must stay behind a firewall: a database, a management port.
func (p *Prober) Closed(ctx context.Context, target string) Outcome {
	if err := hostPort(target, ""); err != nil {
		return failure(0, err)
	}
	start := time.Now()
	conn, err := p.dialer.DialContext(ctx, "tcp", target)
	rtt := time.Since(start)
	if err == nil {
		_ = conn.Close()
		return Outcome{RTT: rtt, Err: "the port is open"}
	}
	if unreachable(err) {
		return Outcome{OK: true, RTT: rtt}
	}
	// Anything else, such as a name that does not resolve or a denied address,
	// says nothing about the port.
	return failure(rtt, err)
}

// unreachable reports whether err is a refusal, a silence or a missing route:
// the ways in which a port shows it cannot be reached.
func unreachable(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	for _, e := range []error{syscall.ECONNREFUSED, syscall.ENETUNREACH, syscall.EHOSTUNREACH} {
		if errors.Is(err, e) {
			return true
		}
	}
	msg := err.Error()
	return strings.Contains(msg, "refused") || strings.Contains(msg, "unreachable")
}
