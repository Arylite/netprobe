# netprobe

Network probing for a fleet of machines. **Edges** measure the network from where
they are; a **central** assigns their checks, stores the results, alerts, and
shows them in a web UI.

Status: 1.0.0-rc.1. Edges poll the central, run their checks and report. The
central opens an incident when a check keeps failing or an edge goes quiet, and
tells webhooks. A web UI manages edges, checks, channels and users; Grafana
reads the results. Everything lives in PostgreSQL with TimescaleDB, and both
sides have a `doctor` command.

## Architecture

```
edge (Go) --- HTTPS + token: poll assignments, post results ---> central (Go) ---> Postgres / TimescaleDB
                                                                  ^   |                  ^
                                  web UI (static files) ---------+   +--> webhooks       |
                                  HTTPS + bearer token, JSON API         (incidents)     |
                                                                          Grafana (SQL, read-only)
```

- The central serves two APIs and no files: the edge API and an API for the web
  UI, described by an OpenAPI document it publishes itself. The UI is a separate client, hosted anywhere (static files, its
  own server), and talks to the central like any other client.
- Edges only dial out. They poll `GET /assignments` and post results in batches;
  the poll is also the heartbeat. Plain HTTPS and JSON: it passes any reverse
  proxy and can be tested with curl.
- One token per edge, stored hashed, revocable. The central sits behind a normal
  TLS certificate.
- The central reads the edge address from the request (with explicit trusted
  proxies): no STUN.
- The two APIs are separate listeners, so they can face different networks: the
  edge API on the internet, the UI API on an intranet. The UI API authenticates
  with bearer tokens, not cookies, and answers only the origins it is told to.
- Configuration comes from environment variables, with the same names everywhere.
- A `doctor` command on both sides checks DNS, TLS, token, clock, database and
  proxies, and says what is wrong.

## Layout

```
cmd/netprobe-central   server entry point and its commands
cmd/netprobe-edge      agent entry point
e2e                    edge and central tested together
internal/api           wire contract between edge and central
internal/cli           environment defaults and logging for the commands
internal/probe         network measurements and the policy on what they may reach
internal/edge          the agent: scheduler, buffer, central client
internal/central       the server: edge API, web API, storage, alerting
internal/central/store  PostgreSQL: migrations, edges, checks, results, users
internal/central/auth   passwords (argon2id), policy, login throttling
internal/central/webapi the UI API: sessions, roles, CORS, endpoints, OpenAPI
internal/central/alert  incidents from failing checks and silent edges, webhooks
web                    the management UI: React, built to static files
internal/version       build information
```

Dependencies go one way: `cmd` -> `edge` or `central` -> `api` and `probe`.
`api` and `probe` import no other package of the project, and `edge` and
`central` never import each other (only `e2e` imports both, in tests).

## Try it

```sh
make build dev-db        # binaries, and a TimescaleDB on 127.0.0.1:5432
export NETPROBE_DATABASE_URL='postgres://netprobe:netprobe@127.0.0.1:5432/netprobe?sslmode=disable'

printf '%s
' 'a long admin password' | bin/netprobe-central user add --username alice --role admin
bin/netprobe-central serve        # edge API on 127.0.0.1:8080, UI API on 127.0.0.1:8081
```

Then open the UI (see [The web UI](#the-web-ui)), or do what it does over the UI
API; its contract is `GET /api/v1/openapi.yaml`:

```sh
API=http://127.0.0.1:8081
TOKEN=$(curl -s $API/api/v1/login -d '{"username":"alice","password":"a long admin password"}' | jq -r .token)
EDGE=$(curl -s $API/api/v1/edges -H "Authorization: Bearer $TOKEN" -d '{"name":"paris"}' | jq -r .token)
curl -s $API/api/v1/checks -H "Authorization: Bearer $TOKEN"   -d '{"id":"web","kind":"http","target":"https://example.com","interval_seconds":30}'

NETPROBE_TOKEN=$EDGE bin/netprobe-edge --central http://127.0.0.1:8080

curl -s "$API/api/v1/checks/web/results?limit=5" -H "Authorization: Bearer $TOKEN"
```

The same things can be done from the command line: `edge add|list|revoke`,
`check add|list|remove`, `channel add|list|remove|test`, `incident list` and
`user add|list|passwd|delete`. The central applies its migrations on start. The
extension must be creatable by the database user.

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
clocks. `netprobe-central doctor` checks the database, TimescaleDB, the schema,
the edges and checks, edges that stopped reporting, and how the edge API is
exposed. Neither changes anything: the central's check reads the database
without migrating it. The exit status is 1 when a step failed.

## The web UI

`web/` is a React app that manages the central: sign in, an overview of every
check on each edge, the incidents, and the pages to add and remove edges,
checks, notification channels and users, and to change one's own password. It
draws no charts: Grafana does that (see below). The light theme comes first and
a switch in the header moves to the dark one.

It is static files, so host it anywhere. The release holds
`netprobe-ui_vX.Y.Z.tar.gz`; or build it yourself with `make web` into
`web/dist`. Say where the central's UI API is in `config.json`, next to
`index.html`, and name the origin that serves the UI in the central's
`--cors-origins`:

```json
{ "apiUrl": "https://central.example.com:8081" }
```

The page loads only its own scripts (a Content-Security-Policy is part of
`index.html`); a host that wants `connect-src` narrowed to the central can send
its own header. The session token lives in `sessionStorage`, so it goes with the
tab.

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
unless `--retention` says how long, for example `--retention 2160h` for 90 days:
older data is dropped in the background, and the setting is applied each time the
central starts.

Grafana reads them with its PostgreSQL data source, with a user of its own that
can only read what it needs. Never grant `SELECT` on all of `edges` (it holds the
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
- Roles: `viewer` reads everything, `admin` also manages edges, checks and users.
  Nobody can delete their own account or the last administrator.
- A browser on another origin must be named in `--cors-origins`
  (`scheme://host[:port]`, no wildcard). By default no origin is allowed.
- Passwords are hashed with argon2id, 12 to 128 characters. A failed login looks
  the same for an unknown user and a wrong password and takes the same time.
  Failures are throttled per address and per account and address; a success does
  not reset the limit of the address. Changing a password needs the current one
  and ends every session of the account.
- `GET /api/v1/status` summarises each check on each active edge over the last
  day, so a dashboard needs no more than one call; `incidents` and `channels`
  (administrators only) are the other additions of the alerting.
- Plain HTTP, like the edge API: put a TLS reverse proxy in front outside
  localhost. The central warns when a surface is bound elsewhere.

## Security model

- Each edge has its own random token. The central stores only its SHA-256 hash
  in the database. A token is shown once, when it is created.
- The token goes to the edge through `NETPROBE_TOKEN` or `--token-file`, never a
  flag: command lines are visible to every user of the machine.
- The central speaks plain HTTP and expects a TLS reverse proxy in front of it
  when it is reachable from a network. It listens on loopback by default and
  warns when it is bound elsewhere.
- A failure of the database is answered 503, never 401: an edge must not
  conclude that its token was refused when the central is the one in trouble.
- The edge refuses to send its token over plain HTTP to anything but this
  machine: a remote central must be an `https://` URL.
- `GET /healthz` needs no token; everything under `/v1/` does.
- The central decides what an edge probes, so a compromised central must not be
  able to turn edges against their own network. At dial time, after name
  resolution (so redirects and DNS tricks are covered), an edge refuses
  loopback, link-local (cloud metadata services live there), unspecified and
  multicast addresses. Private ranges stay allowed: probing an intranet is the
  point. `--deny` takes other CIDRs, or `none` to allow everything.
- The central dials the address of a channel as an administrator wrote it:
  `--deny` is for edges, and an alert manager on the same machine is a normal
  target. Only administrators create channels and read their addresses, the
  address is never logged nor shown in an error, redirects are not followed, and
  a signing secret is write-only.
- Results wait in memory, at most 10 000, while the central is unreachable; the
  oldest go first. A batch the central refuses as invalid is dropped rather than
  retried forever. On shutdown the edge stops measuring, then sends what is left.

Every flag has a `NETPROBE_*` variable: `NETPROBE_EDGE_LISTEN`, `NETPROBE_API_LISTEN`, `NETPROBE_CORS_ORIGINS`,
`NETPROBE_SESSION_TTL`, `NETPROBE_ALERT_FAILURES`, `NETPROBE_EDGE_SILENCE`,
`NETPROBE_ALERT_INTERVAL`, `NETPROBE_RETENTION`, `NETPROBE_DATABASE_URL`,
`NETPROBE_CENTRAL`, `NETPROBE_TOKEN`, `NETPROBE_TOKEN_FILE`,
`NETPROBE_POLL_INTERVAL`, `NETPROBE_REPORT_INTERVAL`, `NETPROBE_DENY`,
`NETPROBE_LOG_LEVEL`.

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
TimescaleDB), `vuln` (Go and npm) and `web` (the generated client is up to date,
types, tests, build) run in parallel, `build` follows lint and test, and the `ci`
job at the end is the one check to require. The work is in the reusable
workflows beside it (`lint.yml`, `test.yml`, `vuln.yml`, `web.yml`,
`build.yml`).

A tag `vX.Y.Z` on `main` runs the same checks, then `package` and `publish`:
a GitHub release with archives for linux (amd64, arm64), macOS (arm64) and
windows (amd64), the web UI as `netprobe-ui_vX.Y.Z.tar.gz`, `SHA256SUMS`, and an
attestation of where they were built
(`gh attestation verify FILE --repo Arylite/netprobe`). A tag with a suffix,
such as `v1.0.0-rc.1`, makes a pre-release.

License: Apache 2.0.
