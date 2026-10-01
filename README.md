# netprobe

Network probing for a fleet of machines. **Edges** measure the network from where
they are; a **central** assigns their checks, stores the results, alerts, and
shows them in a web UI.

Status: 0.1, project skeleton. Nothing runs yet.

## Architecture

```
edge (Go) --- HTTPS + token: poll assignments, post results ---> central (Go, UI embedded) ---> Postgres / TimescaleDB
                                                                      ^
                                                          browser (session cookie, same origin)
```

- Edges only dial out. They poll `GET /assignments` and post results in batches;
  the poll is also the heartbeat. Plain HTTPS and JSON: it passes any reverse
  proxy and can be tested with curl.
- One token per edge, stored hashed, revocable. The central sits behind a normal
  TLS certificate.
- The central reads the edge address from the request (with explicit trusted
  proxies): no STUN.
- The edge API and the web surface are separate listeners, so they can face
  different networks.
- Configuration comes from environment variables, with the same names everywhere.
- A `doctor` command on both sides checks DNS, TLS, token, clock, database and
  proxies, and says what is wrong.

## Layout

```
cmd/netprobe-central   server entry point
cmd/netprobe-edge      agent entry point
internal/api           wire contract between edge and central
internal/probe         network measurements
internal/edge          the agent: scheduler, buffer, central client
internal/central       the server: edge API, web API, storage, alerting
internal/version       build information
```

Dependencies go one way: `cmd` -> `edge` or `central` -> `api` and `probe`.
`api` and `probe` import no other package of the project, and `edge` and
`central` never import each other.

## Develop

```sh
make build    # binaries in bin/
make test     # tests with the race detector
make lint     # go vet and golangci-lint
make help     # everything else
```

Commits follow `feat:`, `fix:`, `refactor:`, `perf:`, `docs:`, `test:`, `build:`,
`ci:` and `chore:`.

License: Apache 2.0.
