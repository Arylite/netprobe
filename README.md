# netprobe

[![CI](https://github.com/Arylite/netprobe/actions/workflows/ci.yml/badge.svg)](https://github.com/Arylite/netprobe/actions/workflows/ci.yml)
[![CodeQL](https://github.com/Arylite/netprobe/actions/workflows/codeql.yml/badge.svg)](https://github.com/Arylite/netprobe/actions/workflows/codeql.yml)
[![Release](https://img.shields.io/github/v/release/Arylite/netprobe?include_prereleases&sort=semver)](https://github.com/Arylite/netprobe/releases)
[![License](https://img.shields.io/github/license/Arylite/netprobe)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/Arylite/netprobe?logo=go&logoColor=white)](go.mod)
[![Images](https://img.shields.io/badge/images-ghcr.io-2496ED?logo=docker&logoColor=white)](https://github.com/Arylite?tab=packages&repo_name=netprobe)

Network probing for a fleet of machines. **Edges** measure the network from where
they are; a **central** assigns their checks, stores the results, alerts, and is
managed from a web UI.

![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-20232A?logo=react&logoColor=61DAFB)
![TypeScript](https://img.shields.io/badge/TypeScript-3178C6?logo=typescript&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-4169E1?logo=postgresql&logoColor=white)
![TimescaleDB](https://img.shields.io/badge/TimescaleDB-FDB515?logo=timescale&logoColor=black)
![Grafana](https://img.shields.io/badge/Grafana-F46800?logo=grafana&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-2496ED?logo=docker&logoColor=white)
![Cloudflare](https://img.shields.io/badge/Cloudflare_Tunnel-F38020?logo=cloudflare&logoColor=white)

Status: 1.0.0-rc.1. Edges poll the central, run their checks and report. The
central opens an incident when a check keeps failing or an edge goes quiet, and
tells webhooks. A web UI manages edges, checks, channels and users; Grafana
reads the results. Everything lives in PostgreSQL with TimescaleDB, and both
sides have a `doctor` command.

## At a glance

![Top language](https://img.shields.io/github/languages/top/Arylite/netprobe)
![Code size](https://img.shields.io/github/languages/code-size/Arylite/netprobe)
![Repository size](https://img.shields.io/github/repo-size/Arylite/netprobe)
![Last commit](https://img.shields.io/github/last-commit/Arylite/netprobe)
![Commit activity](https://img.shields.io/github/commit-activity/m/Arylite/netprobe)
![Open issues](https://img.shields.io/github/issues/Arylite/netprobe)
![Stars](https://img.shields.io/github/stars/Arylite/netprobe?style=flat)

| | |
|---|---|
| **Binaries** | `netprobe-central`, `netprobe-edge`, for linux (amd64, arm64), macOS (arm64), windows (amd64) |
| **Images** | `central`, `edge`, `web`, `db`, for amd64 and arm64, scanned before they are published |
| **Checks** | TCP connect and HTTP request, every second at best |
| **Storage** | PostgreSQL with TimescaleDB: compressed after 7 days, kept as long as you say |
| **Alerts** | incidents from failing checks and silent edges, signed webhooks |
| **Interfaces** | a web UI to manage it, three Grafana dashboards to look at it, a documented JSON API |
| **Security** | TLS 1.3, client certificates, argon2id, encrypted secrets, audit trail |

## Try it

With Docker, one command starts the central, its database, the web UI and a TLS
proxy:

```sh
docker compose up -d
docker compose logs central | grep setup_code     # six digits
```

Open <https://localhost> (the proxy makes a certificate with its own authority:
your browser warns once), give the code, and create the administrator. The home
page then guides the first steps: register an edge, add a check, add a channel.
See [Docker](#docker) for a real name, a Cloudflare Tunnel, edges and upgrades.

From the source:

```sh
make build dev-db        # binaries, and a TimescaleDB on 127.0.0.1:5432
export NETPROBE_DATABASE_URL='postgres://netprobe:netprobe@127.0.0.1:5432/netprobe?sslmode=disable'

printf '%s\n' 'a long admin password' | bin/netprobe-central user add --username alice --role admin
bin/netprobe-central serve        # edge API on 127.0.0.1:8080, UI API on 127.0.0.1:8081
```

Then open the UI (see [The web UI](#the-web-ui)), or do what it does over the UI
API; its contract is `GET /api/v1/openapi.yaml`:

```sh
API=http://127.0.0.1:8081
TOKEN=$(curl -s $API/api/v1/login -d '{"username":"alice","password":"a long admin password"}' | jq -r .token)
EDGE=$(curl -s $API/api/v1/edges -H "Authorization: Bearer $TOKEN" -d '{"name":"paris"}' | jq -r .token)
curl -s $API/api/v1/checks -H "Authorization: Bearer $TOKEN" -d '{"id":"web","kind":"http","target":"https://example.com","interval_seconds":30}'

NETPROBE_TOKEN=$EDGE bin/netprobe-edge --central http://127.0.0.1:8080

curl -s "$API/api/v1/checks/web/results?limit=5" -H "Authorization: Bearer $TOKEN"
```

The same things can be done from the command line: `edge add|list|revoke`,
`check add|list|remove`, `channel add|list|remove|test`, `incident list`,
`audit list` and `user add|list|passwd|delete`. The central applies its migrations
on start, and brings the TimescaleDB extension to the version of the server. The
extension must be creatable by the database user.

## Architecture

```
edge (Go) --- HTTPS + token: poll assignments, post results ---> central (Go) ---> Postgres / TimescaleDB
                                                                  ^   |                  ^
                                  web UI (static files) ---------+   +--> webhooks       |
                                  HTTPS + bearer token, JSON API         (incidents)     |
                                                                          Grafana (SQL, read-only)
```

- The central serves two APIs and no files: the edge API and an API for the web
  UI, described by an OpenAPI document it publishes itself. The UI is a separate
  client, hosted anywhere (static files, its own server), and talks to the central
  like any other client.
- Edges only dial out. They poll `GET /v1/assignments` and post results in
  batches; the poll is also the heartbeat. Plain HTTPS and JSON: it passes any
  reverse proxy and can be tested with curl.
- One token per edge, stored hashed, revocable.
- The central takes the address of a client from the connection, or from
  `X-Forwarded-For` when the connection comes from a proxy named in
  `--trusted-proxies`: no STUN.
- The two APIs are separate listeners, so they can face different networks: the
  edge API on the internet, the UI API on an intranet. The UI API authenticates
  with bearer tokens, not cookies, and answers only the origins it is told to.
- Configuration comes from environment variables, with the same names everywhere.
- A `doctor` command on both sides checks DNS, TLS, token, clock, database and
  proxies, and says what is wrong.

## Layout

```
cmd/netprobe-central    server entry point and its commands
cmd/netprobe-edge       agent entry point
e2e                     edge and central tested together
deploy                  the proxy, web server and database images, an edge compose file
internal/api            wire contract between edge and central
internal/cli            environment defaults and logging for the commands
internal/probe          network measurements and the policy on what they may reach
internal/edge           the agent: scheduler, buffer, central client
internal/central        the server: edge API, doctor
internal/central/store   PostgreSQL: migrations, edges, checks, results, users, incidents
internal/central/auth    passwords (argon2id), policy, login throttling, trusted proxies
internal/central/webapi  the UI API: sessions, roles, CORS, endpoints, OpenAPI
internal/central/alert   incidents from failing checks and silent edges, webhooks
web                     the management UI: React, built to static files
```

Dependencies go one way: `cmd` -> `edge` or `central` -> `api` and `probe`.
`api` and `probe` import no other package of the project, and `edge` and
`central` never import each other (only `e2e` imports both, in tests).

## Docker

`Dockerfile` builds three images, `central`, `edge` and `web`, from distroless
(Go) and unprivileged nginx (UI); `deploy/db` is TimescaleDB without the Go tools
of its image, which the central does not use and whose Go runtime carries known
vulnerabilities. Every image runs as a user without privileges, and every base
is pinned by digest.

`compose.yaml` runs the central, its database, the web UI, Grafana and a Caddy proxy:

- Only the proxy publishes ports (80 and 443). It serves the UI at `/`, the UI API
  at `/api/` and the edge API at `/v1/` and `/healthz`, all on one name, so the UI
  needs no CORS.
- The database is on a network with no way out. Its password, its URL and the key
  that encrypts the secrets of the channels are made at the first start, in the
  `secrets` volume. **Back that volume up with `db-data`**: without the key the
  channels cannot be read again.
- Every container has a read-only file system, no Linux capability and
  `no-new-privileges`. The central trusts the proxy, and only it, to say who the
  client is.
- Grafana is at <https://grafana.localhost>, on a name of its own so that nothing it
  runs shares the origin of the UI. It reads the database through a role that can
  only read the results, and comes with three dashboards. Its administrator is
  `admin`; the password is made at the first start:
  `docker compose exec grafana cat /grafana-secrets/grafana_admin_password`.

Settings go in a `.env` file (see `.env.example`). For a real name, set
`NETPROBE_DOMAIN` and `NETPROBE_PUBLIC_URL`: the proxy gets a certificate from Let's
Encrypt, which needs ports 80 and 443 to reach the host.

**Cloudflare Tunnel (optional).** To publish the UI and the edge API with no open
port, create a tunnel in the Cloudflare dashboard, give it the public hostname of
`NETPROBE_PUBLIC_URL` with the service `http://proxy:8088`, keep its token in a file
`cloudflared_token` next to `compose.yaml`, and start with the profile:

```sh
NETPROBE_BIND=127.0.0.1 NETPROBE_PUBLIC_URL=https://netprobe.example.com docker compose --profile cloudflared up -d
```

`NETPROBE_BIND=127.0.0.1` keeps the proxy off the network. Cloudflare holds the
certificate; the proxy listens in plain HTTP on port 8088 for the tunnel, inside
the compose network only, and the address of the client is the one `cloudflared`
reports, believed from that one container. Put Cloudflare Access in front of the UI
if it should not be public. The `cloudflared` image is Cloudflare's, pinned by
digest; it is not one of the images this project builds and scans.

An edge goes on the machine it measures from, with `deploy/edge/compose.yaml`: it
needs the token printed when the edge is added, in a file `edge_token`. If the
central uses a private authority, such as the local one of the proxy, give the edge
its certificate with `--ca-file`:

```sh
docker compose cp proxy:/data/caddy/pki/authorities/local/root.crt ./root.crt
```

To upgrade, `docker compose pull && docker compose up -d` for a release, or
`docker compose up -d --build` from the source. The central migrates the schema
and updates the TimescaleDB extension when it starts. Images are published to
`ghcr.io/arylite/netprobe-{central,edge,web,db}`, for amd64 and arm64.

## When something does not work

Run `doctor` where the problem is. It checks what the program needs, in order,
stops at the first failure and says what to try:

```
$ netprobe-edge doctor --central https://central.example.com
ok    config  central https://central.example.com, token set
ok    dns     central.example.com -> 203.0.113.7
ok    tcp     connected to central.example.com:443 in 21 ms
FAIL  tls     TLS handshake failed: x509: certificate signed by unknown authority
              -> the certificate is not signed by an authority this machine trusts: ...
skip  health  not run: an earlier step failed
```

`netprobe-edge doctor` checks the settings, the name, the network path, the
certificate (and when it expires), the central's health, the token and the
clocks. `netprobe-central doctor` checks the database, TimescaleDB and its
version, the schema, the edges and checks, edges that stopped reporting, open
incidents, whether channel secrets are encrypted, the retention, and how each
surface is exposed (or when its certificate expires). Neither changes anything:
the central's check reads the database without migrating it. The exit status is 1
when a step failed. In a container:
`docker compose exec central /netprobe-central doctor`.

## The web UI

`web/` is a React app that manages the central: sign in, an overview of every
check on each edge, the incidents, and the pages to add and remove edges,
checks, notification channels and users, to read the audit log, and to change
one's own password. It draws no charts: Grafana does that (see below). The light
theme comes first and a switch in the header moves to the dark one.

A new central has no account. The UI then opens on a welcome page that creates the
first administrator, with the six digit setup code that the central writes to its
log when it starts (`setup_code`). The page works once. Ten wrong codes, from
anyone, close it for 15 minutes, and a restart makes a new code. An administrator
also gets a short "get started" guide on the overview until an edge, a check and a
channel exist.

It is static files, so host it anywhere. The release holds
`netprobe-ui_vX.Y.Z.tar.gz`; or build it yourself with `make web` into
`web/dist`; or use the `web` image. Say where the central's UI API is in
`config.json`, next to `index.html` (the image makes it from `NETPROBE_API_URL`),
and name the origin that serves the UI in the central's `--cors-origins`:

```json
{ "apiUrl": "https://central.example.com:8081" }
```

The page loads only its own scripts (a Content-Security-Policy is part of
`index.html`); a host that wants `connect-src` narrowed to the central can send
its own header, as the image does. The session token lives in `sessionStorage`, so
it goes with the tab.

To work on it: `cd web && npm ci && npm run dev` serves it on
`http://127.0.0.1:5173` (add that origin to `--cors-origins`), `npm test` runs
its tests, and `npm run api` regenerates the typed client after a change of
`internal/central/webapi/openapi.yaml`.

## Alerts

An incident opens when a check fails `--alert-failures` times in a row on an edge
(3), or when an edge has sent no result for `--edge-silence` (5 minutes). It
resolves when the check succeeds again, when the edge reports again, or when the
edge is revoked or the check removed. The central looks at the results every
`--alert-interval` (30 seconds); with several centrals on one database, one
evaluates at a time.

A channel is a webhook told when an incident opens and when it resolves. Add one
from the UI or the command line, and try it:

```sh
printf '%s\n' 'the secret' | netprobe-central channel add --name ops --url https://hooks.example.com/x --secret-stdin
netprobe-central channel test --name ops
```

- A channel only hears of incidents that start after it was added.
- An event is sent until the channel answers 2xx, and no longer than an hour
  after it happened: an alert that comes late is not an alert. A channel that
  fails is tried again at the next round and does not hold back the others.
- A resolution goes only to a channel that heard the incident open.
- A webhook may not reach loopback, link-local, multicast or the metadata address
  of a cloud, whatever name it is given (`--webhook-deny`, or `none`).

The body is JSON. `text` reads as a sentence, which chat tools with incoming
webhooks accept as it is:

```json
{
  "event": "opened",
  "text": "[netprobe] web on paris is failing: 3 failures in a row, last: connection refused",
  "incident": { "id": 12, "kind": "check", "check_id": "web", "edge_id": "1a08...", "edge": "paris",
                "started_at": "2026-10-07T09:12:00Z", "detail": "3 failures in a row, last: connection refused" }
}
```

`event` is `opened`, `resolved` or `test`. With a secret, each request carries
`X-Netprobe-Timestamp` and `X-Netprobe-Signature`, which is `sha256=` and the hex
HMAC-SHA256, keyed by the secret, of the timestamp, a dot and the body. A
receiver should recompute it and refuse a timestamp that is not recent:

```python
expected = "sha256=" + hmac.new(secret, ts.encode() + b"." + body, hashlib.sha256).hexdigest()
ok = hmac.compare_digest(expected, signature_header)
```

## Results, retention and Grafana

Results go to a TimescaleDB hypertable, `results` (`edge_id`, `check_id`, `at`,
`ok`, `rtt_millis`, `error`), compressed after 7 days. They are kept for ever
unless `--retention` says how long, for example `--retention 2160h` for 90 days
(the compose file does): older data is dropped in the background, and the setting
is applied each time the central starts.

Grafana reads them with its PostgreSQL data source, with a user of its own that
can only read what it needs. The compose file sets all of this up, with the
dashboards in `deploy/grafana/dashboards`; this is for a Grafana of your own. Never grant `SELECT` on all of `edges` (it holds the
hashes of the tokens), on `users` or on `channels` (secrets):

```sql
CREATE ROLE grafana LOGIN PASSWORD '...';
GRANT CONNECT ON DATABASE netprobe TO grafana;
GRANT USAGE ON SCHEMA public TO grafana;
GRANT SELECT ON results, checks, incidents TO grafana;
GRANT SELECT (id, name) ON edges TO grafana;
```

For example, the response time of a check, one line per edge:

```sql
SELECT $__timeGroup(r.at, $__interval) AS time, e.name AS metric, avg(r.rtt_millis) AS value
FROM results r JOIN edges e ON e.id = r.edge_id
WHERE $__timeFilter(r.at) AND r.check_id = '$check' AND r.ok
GROUP BY 1, 2 ORDER BY 1
```

## The UI API

It is a separate listener (`--api-listen`) from the edge API (`--edge-listen`), so
one can face the internet and the other an intranet. The web UI is a separate
application: it is hosted wherever you like and calls this API.

- `POST /api/v1/login` trades a username and a password for an opaque session
  token (12 hours, `--session-ttl`); the UI sends it as `Authorization: Bearer`.
  There are no cookies, so there is nothing for another site to forge: no CSRF.
  `GET` and `POST /api/v1/setup` create the first administrator of an empty central.
- Roles: `viewer` reads everything, `admin` also manages edges, checks, channels
  and users and reads the audit log. Nobody can delete their own account or the
  last administrator.
- A browser on another origin must be named in `--cors-origins`
  (`scheme://host[:port]`, no wildcard). By default no origin is allowed.
- `GET /api/v1/status` summarises each check on each active edge over the last
  day, so a dashboard needs no more than one call.

## Security model

**Accounts and sessions**

- Passwords are hashed with argon2id, 12 to 128 characters, and refused when they
  are among the most common ones, repeat a few characters or form a sequence. At
  most four are hashed at once (64 MiB each), so a burst of logins cannot use up
  the memory of the server; the next waits, then gets a 503.
- A failed login looks the same for an unknown user and a wrong password and
  takes the same time. Failures are throttled per address and per account and
  address. There is no lock per account on purpose: it would let anyone lock a
  user out.
- Sessions are opaque, 256 bits, stored hashed, end after 12 hours, and an account
  keeps at most 20. Changing a password needs the current one and ends every
  session of the account.
- Behind a reverse proxy, set `--trusted-proxies` to its address. Without it every
  client looks like the proxy: the failure limits would lock everyone out together
  and the logs would name no one. The header is only believed from those
  addresses, read from the right, and a range that holds every address is refused.

**Transport**

- Both surfaces can serve TLS 1.3 themselves: `--edge-tls-cert` and `--edge-tls-key`,
  `--api-tls-cert` and `--api-tls-key`. A renewed certificate is picked up within
  30 seconds, without a restart. `--edge-client-ca` also requires every edge to
  present a certificate signed by that authority; an edge gives its own with
  `--client-cert` and `--client-key`, and trusts a private authority with `--ca-file`.
- Otherwise put a TLS reverse proxy in front. The central listens on loopback by
  default and warns when it is bound elsewhere over plain HTTP.
- The edge refuses to send its token over plain HTTP to anything but this machine.
- The UI API answers with `nosniff`, `no-store`, a CSP that allows nothing,
  `Referrer-Policy: no-referrer` and, when it serves TLS, HSTS.

**What is probed and what is called**

- Each edge has its own random token. The central stores only its SHA-256 hash.
  A token is shown once, and goes to the edge through `NETPROBE_TOKEN` or
  `--token-file`, never a flag: command lines are visible to every user.
- A failure of the database is answered 503, never 401: an edge must not
  conclude that its token was refused when the central is in trouble.
- The central decides what an edge probes, so a compromised central must not be
  able to turn edges against their own network. At dial time, after name
  resolution (so redirects and DNS tricks are covered), an edge refuses
  loopback, link-local, unspecified and multicast addresses, the metadata services
  of the clouds, and IPv6 forms that carry such an address (NAT64, 6to4). Private
  ranges stay allowed: probing an intranet is the point. `--deny` takes other
  CIDRs, or `none`.
- The same ranges are denied to the webhooks of the central (`--webhook-deny`),
  which are written by administrators and not by the central.
- Results wait in memory, at most 10 000, while the central is unreachable; the
  oldest go first. A batch the central refuses as invalid is dropped rather than
  retried forever.

**Secrets and traces**

- The address and the signing secret of a channel must be read back to be used, so
  they are encrypted in the database with AES-256-GCM, bound to their row, under a
  key in the file `NETPROBE_SECRET_KEY_FILE` names. `netprobe-central secret-key`
  makes one; `netprobe-central secrets encrypt` takes the channels that were added
  before there was one; `doctor` says when some are still in clear. The compose
  file does all of it. A secret is write-only: no API returns it, and no log or
  error shows an address.
- An audit trail records sign-ins and refusals, and every change of edges, checks,
  channels, users and passwords, with who and from which address, never a
  password, secret or address. Administrators read it in the UI or with
  `audit list`; it is kept for a year (`--audit-retention`).
- Files and URLs that hold secrets can be given as files: `NETPROBE_DATABASE_URL_FILE`,
  `NETPROBE_SECRET_KEY_FILE`, `NETPROBE_TOKEN_FILE`.

**Build and supply chain**

- Images run as non-root, read-only, without capabilities; they are scanned for
  known vulnerabilities with a fix, and a release is not published if one is found.
- CI runs `govulncheck`, `npm audit`, CodeQL and the linters; actions are pinned by
  commit; Dependabot proposes the updates. Releases carry checksums and an
  attestation (`gh attestation verify FILE --repo Arylite/netprobe`).
- To report a vulnerability, see `SECURITY.md`.

## Configuration

Every flag has a `NETPROBE_*` variable. The central (`netprobe-central serve`):

| Variable | What |
|---|---|
| `NETPROBE_DATABASE_URL` or `_FILE` | the PostgreSQL URL |
| `NETPROBE_EDGE_LISTEN`, `NETPROBE_API_LISTEN` | the addresses of the two surfaces |
| `NETPROBE_EDGE_TLS_CERT`, `_KEY`, `NETPROBE_EDGE_CLIENT_CA` | TLS of the edge API, and the CA of the edges |
| `NETPROBE_API_TLS_CERT`, `_KEY` | TLS of the UI API |
| `NETPROBE_TRUSTED_PROXIES` | the proxies trusted to say who the client is |
| `NETPROBE_CORS_ORIGINS`, `NETPROBE_SESSION_TTL` | browser origins allowed, and the length of a login |
| `NETPROBE_SECRET_KEY_FILE` | the key that encrypts the secrets of the channels |
| `NETPROBE_ALERT_FAILURES`, `NETPROBE_EDGE_SILENCE`, `NETPROBE_ALERT_INTERVAL` | when an incident opens, and how often it is looked for |
| `NETPROBE_WEBHOOK_DENY` | where the webhooks may not connect |
| `NETPROBE_RETENTION`, `NETPROBE_AUDIT_RETENTION` | how long results and the audit trail are kept |
| `NETPROBE_LOG_LEVEL` | debug, info, warn or error |

The edge (`netprobe-edge`): `NETPROBE_CENTRAL`, `NETPROBE_TOKEN` or
`NETPROBE_TOKEN_FILE`, `NETPROBE_CA_FILE`, `NETPROBE_CLIENT_CERT`,
`NETPROBE_CLIENT_KEY`, `NETPROBE_POLL_INTERVAL`, `NETPROBE_REPORT_INTERVAL`,
`NETPROBE_DENY` and `NETPROBE_LOG_LEVEL`. The web image: `NETPROBE_API_URL`. The
compose file: see `.env.example`.

## Develop

```sh
make build    # binaries in bin/
make test     # tests with the race detector (database tests are skipped)
make test-db  # all tests, against the database of make dev-db
make lint     # go vet and golangci-lint
make web-test # type-check and test the web UI
make web      # build the web UI into web/dist
make help     # everything else
```

Commits follow `feat:`, `fix:`, `refactor:`, `perf:`, `docs:`, `test:`, `build:`,
`ci:` and `chore:`.

## CI and releases

`ci.yml` only says what runs and in which order: `lint`, `test` (against
TimescaleDB), `vuln` (Go and npm), `web` (the generated client is up to date,
types, tests, build) and `images` (built and scanned) run in parallel, `build`
follows lint and test, and the `ci` job at the end is the one check to require.
The work is in the reusable workflows beside it (`lint.yml`, `test.yml`,
`vuln.yml`, `web.yml`, `images.yml`, `build.yml`). `codeql.yml` runs on its own.

A tag `vX.Y.Z` on `main` runs the same checks, then `package` and `publish`:
a GitHub release with archives for linux (amd64, arm64), macOS (arm64) and
windows (amd64), the web UI as `netprobe-ui_vX.Y.Z.tar.gz`, `SHA256SUMS`, and an
attestation of where they were built. The images are published at the same time,
with their provenance and a bill of materials. A tag with a suffix, such as
`v1.0.0-rc.1`, makes a pre-release, and does not move `latest`.

License: Apache 2.0.
