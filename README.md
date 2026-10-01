# netprobe

Network probing for a fleet of machines. **Edges** measure the network from where
they are; a **central** assigns their checks, stores the results, alerts, and
shows them in a web UI.

Status: 0.6. An edge authenticates with its own token, polls the central for its
checks, runs them (TCP connect time, HTTP response time) and reports the results
in batches. Edges, checks and results live in PostgreSQL with TimescaleDB, and
both sides have a `doctor` command. There is no way to read results back over
HTTP yet.

## Architecture

```
edge (Go) --- HTTPS + token: poll assignments, post results ---> central (Go) ---> Postgres / TimescaleDB
                                                                    ^
                                        web UI (separate app) --- HTTPS + bearer token, JSON API
```

- The central serves two APIs and no files: the edge API and, from 0.7, an API
  for the web UI. The UI is a separate client, hosted anywhere (static files, its
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
cmd/netprobe-central   server entry point
cmd/netprobe-edge      agent entry point
e2e                    edge and central tested together
internal/api           wire contract between edge and central
internal/cli           environment defaults and logging for the commands
internal/probe         network measurements and the policy on what they may reach
internal/edge          the agent: scheduler, buffer, central client
internal/central       the server: edge API, web API, storage, alerting
internal/central/store  PostgreSQL: migrations, edges, checks, results
internal/version       build information
```

Dependencies go one way: `cmd` -> `edge` or `central` -> `api` and `probe`.
`api` and `probe` import no other package of the project, and `edge` and
`central` never import each other (only `e2e` imports both, in tests).

## Try it

```sh
make build dev-db        # binaries, and a TimescaleDB on 127.0.0.1:5432
export NETPROBE_DATABASE_URL='postgres://netprobe:netprobe@127.0.0.1:5432/netprobe?sslmode=disable'

bin/netprobe-central check add --id web --kind http --target https://example.com --interval 30
bin/netprobe-central edge add --name paris          # prints the token, once
bin/netprobe-central serve                          # 127.0.0.1:8080

NETPROBE_TOKEN=np_... bin/netprobe-edge --central http://127.0.0.1:8080
```

The central applies its migrations on start. `edge list` and `check list` show
what is registered, `edge revoke --name paris` cuts an edge off at once.

Results go to a TimescaleDB hypertable, compressed after 7 days and kept until
you delete them. The extension must be creatable by the database user.

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
- Results wait in memory, at most 10 000, while the central is unreachable; the
  oldest go first. A batch the central refuses as invalid is dropped rather than
  retried forever. On shutdown the edge stops measuring, then sends what is left.

Every flag has a `NETPROBE_*` variable: `NETPROBE_LISTEN`, `NETPROBE_DATABASE_URL`,
`NETPROBE_CENTRAL`, `NETPROBE_TOKEN`, `NETPROBE_TOKEN_FILE`,
`NETPROBE_POLL_INTERVAL`, `NETPROBE_REPORT_INTERVAL`, `NETPROBE_DENY`,
`NETPROBE_LOG_LEVEL`.

## Develop

```sh
make build    # binaries in bin/
make test     # tests with the race detector (database tests are skipped)
make test-db  # all tests, against the database of make dev-db
make lint     # go vet and golangci-lint
make help     # everything else
```

Commits follow `feat:`, `fix:`, `refactor:`, `perf:`, `docs:`, `test:`, `build:`,
`ci:` and `chore:`.

License: Apache 2.0.
