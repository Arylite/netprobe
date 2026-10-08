[Docs](README.md) / Go deeper

# Development

Build it, test it, and see how CI and releases work.

> **Level:** advanced | **You need:** Go, Node.js, and Docker for the database

## Build and test

```sh
make build    # binaries in bin/
make test     # tests with the race detector (database tests are skipped)
make test-db  # all tests, against the database of make dev-db
make lint     # go vet and golangci-lint
make web-test # type-check and test the web UI
make web      # build the web UI into web/dist
make help     # everything else
```

`make dev-db` starts a TimescaleDB for development and tests on `127.0.0.1:5432`.

## Run from the source

```sh
make build dev-db        # binaries, and a TimescaleDB on 127.0.0.1:5432
export NETPROBE_DATABASE_URL='postgres://netprobe:netprobe@127.0.0.1:5432/netprobe?sslmode=disable'
bin/netprobe-central serve
```

Then follow [API](api.md#try-it) or open the web UI ([Web UI](web-ui.md#working-on-it)).

## Commits

Commits follow `feat:`, `fix:`, `refactor:`, `perf:`, `docs:`, `test:`, `build:`, `ci:` and
`chore:`.

## CI

`ci.yml` only says what runs and in which order:

```mermaid
flowchart LR
    lint --> build
    test --> build
    vuln
    web
    images
    build --> ci
    vuln --> ci
    web --> ci
    images --> ci
```

- `lint`, `test` (against TimescaleDB), `vuln` (Go and npm), `web` (the generated client is up to
  date, types, tests, build) and `images` (built and scanned) run in parallel.
- `build` follows lint and test.
- The `ci` job at the end is the one check to require.

The work is in the reusable workflows beside it (`lint.yml`, `test.yml`, `vuln.yml`, `web.yml`,
`images.yml`, `build.yml`). `codeql.yml` runs on its own.

## Releases

A tag `vX.Y.Z` on `main` runs the same checks, then `package` and `publish`:

- a GitHub release with archives for linux (amd64, arm64), macOS (arm64) and windows (amd64);
- the web UI as `netprobe-ui_vX.Y.Z.tar.gz`, `install.sh`, `SHA256SUMS`, and an attestation of
  where they were built;
- the images, published at the same time, with their provenance and a bill of materials.

A tag with a suffix, such as `v1.0.0-rc.1`, makes a pre-release, and does not move `latest`.

---

Previous: [Security](security.md) | Back to the [documentation home](README.md)
