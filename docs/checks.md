# Checks

A check is one measurement that every edge runs, at its own interval. Each has a **kind**, a
**target** (what to measure) and, for most kinds, an optional **expect** (what a good answer is).
Add them in the UI (**Checks**), or from the command line:

```sh
netprobe-central check add --id site-cert --kind tls --target example.com --expect 21 --interval 300
```

Every result is *ok* or *failed*, with a round-trip time in milliseconds, and when it failed, a
sentence that says why. The time is what each kind measures, given below.

| Kind | It checks | Target | Expect | Least interval |
|---|---|---|---|---|
| [`tcp`](#tcp) | a connection opens | `host:port` | | 1 s |
| [`http`](#http) | a page answers as it should | a URL | status, `contains:`, `absent:` | 1 s |
| [`dns`](#dns) | a name resolves, to the right thing | `[TYPE ]name[@server]` | text in an answer | 1 s |
| [`tls`](#tls) | a certificate is trusted and not about to expire | `host[:port]` | days left (14) | 60 s |
| [`icmp`](#icmp) | a host answers a ping | host or address | loss that is fine, % (50) | 5 s |
| [`traceroute`](#traceroute) | the path to a host, and where it ends | host or address | most hops (30) | 60 s |
| [`ntp`](#ntp) | a time server answers, and the clock is right | `host[:port]` | offset that is fine (1s) | 5 s |
| [`banner`](#banner) | a service greets as expected | `host:port` | text in the first line | 1 s |
| [`closed`](#closed) | a port that must not be open is not | `host:port` | | 1 s |
| [`download`](#download) | a file comes down fast enough | a URL | least Mbit/s | 60 s |
| [`domain`](#domain) | a domain name is not about to expire | a domain name | days left (30) | 1 h |

Every connection an edge opens goes through its [policy](edges.md#what-an-edge-may-reach): a check
cannot reach loopback, link-local or cloud metadata addresses, whatever name it uses.

## tcp

The time to open a connection to `host:port`. Good when it opens.

```
tcp   db.example.com:5432
```

## http

The time until the response headers of a GET arrive, following up to five redirects. Good when the
status is below 400. `expect` refines it, with items separated by `;`:

| Item | Good when |
|---|---|
| `200`, `404` | the status is exactly that |
| `2xx`, `3xx` | the status is in that class |
| `contains:TEXT` | the first 256 KiB of the body hold the text |
| `absent:TEXT` | they do not |

```
http  https://example.com/health   expect: 200;contains:"status":"ok"
http  https://example.com/old      expect: 301           (a redirect that is expected is not followed)
http  https://example.com          expect: absent:maintenance
```

## dns

The time to resolve a name. Good when there is at least one record, and when `expect` is given, one
of the answers holds that text (any case). The target is `[TYPE ]name[@server]`:

- `TYPE` is `A`, `AAAA`, `MX`, `TXT`, `NS` or `CNAME`; without it, the addresses of the name.
- `@server` is the address of a resolver (`1.1.1.1`, `[2606:4700:4700::1111]:53`); without it, the
  resolver of the machine. A server is an address, not a name, and goes through the policy.

```
dns   example.com                       expect: 93.184.
dns   MX example.com@1.1.1.1            expect: mail.example.com
dns   TXT example.com                   expect: v=spf1
```

Pointing the same check at your own resolver and at a public one tells a broken resolver from a
broken zone.

## tls

The time to open a connection and finish the TLS handshake. Fails when the certificate is not
trusted by the machine, does not match the name, has expired, or expires in fewer days than
`expect` (14 by default). The name in the target is the one the certificate is checked for.

```
tls   example.com               expect: 21
tls   mail.example.com:465
```

Certificates are renewed rarely: a check every few minutes is plenty.

## icmp

Four ICMP echoes (a ping), 200 ms apart. The time is the mean of the answers. Fails when no answer
comes, or when more than `expect` percent are lost (50 by default; `0` tolerates none).

```
icmp  gateway.example.com
icmp  1.1.1.1                   expect: 0
```

Sending ICMP needs a permission. On Linux, an unprivileged process may do it when its group is in
`net.ipv4.ping_group_range`; the compose file of the edge sets that, and most distributions already
allow it. Otherwise the check says `ping is not allowed here`, and the fix is the sysctl, or
`CAP_NET_RAW` for the process. A firewall that drops ICMP will make the check fail: use `tcp` there.

## traceroute

Follows the path one router at a time (UDP packets with a growing time to live, and the ICMP errors
that come back), and is good when the host is reached in at most `expect` hops (30 by default). When
it is not, it says where the path ended:

```
203.0.113.9 is not reached in 30 hops: the last answer is from 198.51.100.4, at hop 7
198.51.100.4: administratively prohibited at hop 7
the path goes dark after hop 6 (last answer from 198.51.100.2)
```

Linux and IPv4 only, and it needs no privilege. Many routers do not answer, and some networks swallow
the errors, so a path that goes dark is common and not always a fault: use the check on paths you know
work, and let it tell you when they stop. The time is the round trip to the host.

## ntp

Asks a time server for the time. The time is the round trip. Fails when the clock of the machine is
further from the server's than `expect` (a duration, `1s` by default), so it also tells when an edge
has drifted: results with a wrong clock are wrong results.

```
ntp   pool.ntp.org
ntp   time.cloudflare.com       expect: 250ms
```

## banner

Opens a connection and reads the first line the service sends: SSH, SMTP, FTP, IMAP, POP3 and many
databases speak first. Fails when nothing comes, or when the line does not hold `expect`. The time is
the connection and the first line.

```
banner example.com:22           expect: SSH-2.0
banner mail.example.com:25      expect: 220
```

Services that wait for the client (HTTP, TLS) say nothing: use `http` or `tls` for those.

## closed

The opposite of `tcp`: good when the port **cannot** be reached, because the connection is refused
or goes unanswered. It fails when the port is open. Use it for what must stay behind a firewall: a
database, an admin interface, SSH on a host that should be reached through a bastion only. A name that
does not resolve, or an address the policy forbids, is a failure, not a closed port.

```
closed db.example.com:5432
```

## download

Takes up to 8 MiB of a file. The time is the whole download. Good when the status is below 400 and,
with `expect`, when the speed is at least that many megabits per second. A file under 256 KiB is too
small to give a speed and fails when a speed is expected. It moves real data every time, so it runs
once a minute at the most.

```
download https://example.com/100MB.bin   expect: 20
```

## domain

Asks the RDAP service of the registry (through `rdap.org`) when a domain name expires, and fails when
that is within `expect` days (30 by default). A domain that lapses takes everything down with it.

```
domain example.com    expect: 60
```

The lookup is public and rate limited: it runs once an hour at the most. Not every registry publishes
an expiry date; the check then fails with `does not say when it expires`, and is not worth having for
that name.

## Choosing intervals and where to run them

- Every edge runs every check, so `N` edges send `N` measurements per interval. Put the checks that
  describe one place (the certificate of a site, the expiry of a domain) at a long interval: they
  change slowly.
- To tell a fault of the site from a fault of one edge's network, run the same check from several
  edges: the overview shows each edge, and an incident opens per edge.
- An incident opens after three failed runs in a row ([Alerts](alerting.md)), so an interval of
  `300` seconds notices in about fifteen minutes. Use a shorter one where that is too slow.
