[Docs](README.md) / Go deeper

# Docker stack

What `compose.yaml` runs, what the images are, and how everything is locked down.

> **Level:** advanced | **Read first:** [Deploying](deploy.md)

## The images

`Dockerfile` builds three images from distroless (Go) and unprivileged nginx (UI):
`central`, `edge` and `web`. Two more start from the images of others and fix what is wrong
with them:

| Image | What |
|---|---|
| `central`, `edge` | the Go programs, on distroless |
| `web` | the management UI, on unprivileged nginx |
| `db` (`deploy/db`) | TimescaleDB without the Go tools of its image, which the central does not use and whose Go runtime carries known vulnerabilities |
| `renderer` (`deploy/renderer`) | the image renderer of Grafana, with the security updates of its Chromium |

Every image runs as a user without privileges, and every base is pinned by digest. They are
published to `ghcr.io/arylite/netprobe-{central,edge,web,db,renderer}`, for amd64 and arm64,
scanned before they are published.

## What compose runs

`compose.yaml` runs the central, its database, the web UI, Grafana (with its image renderer) and a
Caddy proxy.

| Topic | How it is set |
|---|---|
| Ports | only the proxy publishes ports (80 and 443). It serves the UI at `/`, the UI API at `/api/` and the edge API at `/v1/` and `/healthz`, all on one name, so the UI needs no CORS |
| Database | on a network with no way out. Its password, its URL and the key that encrypts the secrets of the channels are made at the first start, in the `secrets` volume |
| Containers | every one has a read-only file system, no Linux capability and `no-new-privileges` |
| Client addresses | the central trusts the proxy, and only it, to say who the client is |
| Grafana | at <https://grafana.localhost>, on a name of its own so that nothing it runs shares the origin of the UI. It reads the database through a role that can only read the results, and comes with three dashboards. Administrator: `admin`; the password is made at the first start |
| Renderer | makes the PNG of a panel or of a dashboard; on a network shared with Grafana alone, with no way out |

> [!IMPORTANT]
> **Back up the `secrets` volume with `db-data`.** Without the key, the channels cannot be read
> again. See [Operating](operations.md#back-up).

Settings go in a `.env` file: see [Configuration](configuration.md#the-compose-stack) and
`.env.example`. For a real name, set `NETPROBE_DOMAIN` and `NETPROBE_PUBLIC_URL`: the proxy gets a
certificate from Let's Encrypt, which needs ports 80 and 443 to reach the host.

## Cloudflare Tunnel (optional)

To publish the UI and the edge API with no open port, create a tunnel in the Cloudflare
dashboard, give it the public hostname of `NETPROBE_PUBLIC_URL` with the service
`http://proxy:8088`, keep its token in a file `cloudflared_token` next to `compose.yaml`, and
start with the profile:

```sh
NETPROBE_BIND=127.0.0.1 NETPROBE_PUBLIC_URL=https://netprobe.example.com docker compose --profile cloudflared up -d
```

`NETPROBE_BIND=127.0.0.1` keeps the proxy off the network. Cloudflare holds the certificate; the
proxy listens in plain HTTP on port 8088 for the tunnel, inside the compose network only, and the
address of the client is the one `cloudflared` reports, believed from that one container. Put
Cloudflare Access in front of the UI if it should not be public. The `cloudflared` image is
Cloudflare's, pinned by digest; it is not one of the images this project builds and scans.

The full walk-through, with Grafana's name, is in [Deploying](deploy.md).

## An edge in a container

An edge goes on the machine it measures from, with `deploy/edge/compose.yaml`. It needs the token
printed when the edge is added, in a file `edge_token`. If the central uses a private authority,
such as the local one of the proxy, give the edge its certificate with `--ca-file`:

```sh
docker compose cp proxy:/data/caddy/pki/authorities/local/root.crt ./root.crt
```

More in [Edges](edges.md#in-a-container).

## Upgrading

`docker compose pull && docker compose up -d` for a release, or `docker compose up -d --build`
from the source. The central migrates the schema and updates the TimescaleDB extension when it
starts. See [Operating](operations.md#upgrade).

---

Previous: [API](api.md) | Next: [Security](security.md)
