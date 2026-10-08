[Docs](README.md) / Go deeper

# Security

What netprobe guarantees, and how.

> **Level:** advanced | **To report a vulnerability:** see [SECURITY.md](../SECURITY.md)

## In short

| Area | Guarantee |
|---|---|
| Passwords | argon2id, with a policy and login throttling |
| Sessions | opaque, 256 bits, stored hashed, 12 hours by default |
| Transport | TLS 1.3, optional client certificates for edges |
| Edge tokens | random, stored as SHA-256 hashes, shown once, revocable, no expiry |
| Probing | an edge cannot be turned against its own machine |
| Secrets | channel addresses and secrets encrypted with AES-256-GCM; write-only |
| Traces | an audit trail of sign-ins and every change |
| Supply chain | non-root, read-only images; scanned; signed attestations |

## Accounts and sessions

- Passwords are hashed with argon2id, 12 to 128 characters, and refused when they are among the
  most common ones, repeat a few characters or form a sequence. At most four are hashed at once
  (64 MiB each), so a burst of logins cannot use up the memory of the server; the next waits, then
  gets a 503.
- A failed login looks the same for an unknown user and a wrong password and takes the same time.
  Failures are throttled per address and per account and address. There is no lock per account on
  purpose: it would let anyone lock a user out.
- Sessions are opaque, 256 bits, stored hashed, end after 12 hours (`--session-ttl`), and an
  account keeps at most 20. Changing a password needs the current one and ends every session of
  the account.
- Behind a reverse proxy, set `--trusted-proxies` to its address. Without it every client looks
  like the proxy: the failure limits would lock everyone out together and the logs would name no
  one. The header is only believed from those addresses, read from the right, and a range that
  holds every address is refused.

## Transport

- Both surfaces can serve TLS 1.3 themselves: `--edge-tls-cert` and `--edge-tls-key`,
  `--api-tls-cert` and `--api-tls-key`. A renewed certificate is picked up within 30 seconds,
  without a restart.
- `--edge-client-ca` also requires every edge to present a certificate signed by that authority;
  an edge gives its own with `--client-cert` and `--client-key`, and trusts a private authority
  with `--ca-file`.
- Otherwise put a TLS reverse proxy in front. The central listens on loopback by default and warns
  when it is bound elsewhere over plain HTTP.
- The edge refuses to send its token over plain HTTP to anything but this machine.
- The UI API answers with `nosniff`, `no-store`, a CSP that allows nothing,
  `Referrer-Policy: no-referrer` and, when it serves TLS, HSTS.

## What is probed and what is called

- Each edge has its own random token. The central stores only its SHA-256 hash. A token is shown
  once, and goes to the edge through `NETPROBE_TOKEN` or `--token-file`, never a flag: command
  lines are visible to every user.
- A failure of the database is answered 503, never 401: an edge must not conclude that its token
  was refused when the central is in trouble.
- The central decides what an edge probes, so a compromised central must not be able to turn
  edges against their own network. At dial time, after name resolution (so redirects and DNS
  tricks are covered), an edge refuses loopback, link-local, unspecified and multicast addresses,
  the metadata services of the clouds, and IPv6 forms that carry such an address (NAT64, 6to4).
  Private ranges stay allowed: probing an intranet is the point. `--deny` takes other CIDRs, or
  `none`.
- The same ranges are denied to the webhooks of the central (`--webhook-deny`), which are written
  by administrators and not by the central.
- Results wait in memory, at most 10,000, while the central is unreachable; the oldest go first. A
  batch the central refuses as invalid is dropped rather than retried forever.

## Secrets and traces

- The address and the signing secret of a channel must be read back to be used, so they are
  encrypted in the database with AES-256-GCM, bound to their row, under a key in the file
  `NETPROBE_SECRET_KEY_FILE` names. `netprobe-central secret-key` makes one;
  `netprobe-central secrets encrypt` takes the channels that were added before there was one;
  `doctor` says when some are still in clear. The compose file does all of it.
- A secret is write-only: no API returns it, and no log or error shows an address.
- An audit trail records sign-ins and refusals, and every change of edges, checks, channels, users
  and passwords, with who and from which address, never a password, secret or address.
  Administrators read it in the UI or with `audit list`; it is kept for a year
  (`--audit-retention`).
- Files and URLs that hold secrets can be given as files: `NETPROBE_DATABASE_URL_FILE`,
  `NETPROBE_SECRET_KEY_FILE`, `NETPROBE_TOKEN_FILE`.
- Grafana reads the database through a role that can only read what it needs: see
  [Grafana](grafana.md#how-grafana-is-connected).

## Build and supply chain

- Images run as non-root, read-only, without capabilities; they are scanned for known
  vulnerabilities with a fix, and a release is not published if one is found.
- CI runs `govulncheck`, `npm audit`, CodeQL and the linters; actions are pinned by commit;
  Dependabot proposes the updates.
- Releases carry checksums and an attestation:

  ```sh
  gh attestation verify FILE --repo Arylite/netprobe
  ```

---

Previous: [Docker stack](docker.md) | Next: [Development](development.md)
