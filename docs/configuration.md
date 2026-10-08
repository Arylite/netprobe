[Docs](README.md) / Go deeper

# Configuration

Every flag has a `NETPROBE_*` variable, with the same names everywhere.

> **Level:** advanced | **Use it for:** looking up a setting

Values that hold secrets can also be read from a file: `NETPROBE_DATABASE_URL_FILE`,
`NETPROBE_SECRET_KEY_FILE`, `NETPROBE_TOKEN_FILE`.

## The central

`netprobe-central serve`:

| Variable | What |
|---|---|
| `NETPROBE_DATABASE_URL` or `_FILE` | the PostgreSQL URL |
| `NETPROBE_EDGE_LISTEN`, `NETPROBE_API_LISTEN` | the addresses of the two surfaces (edge API, UI API) |
| `NETPROBE_EDGE_TLS_CERT`, `_KEY`, `NETPROBE_EDGE_CLIENT_CA` | TLS of the edge API, and the CA of the edges |
| `NETPROBE_API_TLS_CERT`, `_KEY` | TLS of the UI API |
| `NETPROBE_TRUSTED_PROXIES` | the proxies trusted to say who the client is |
| `NETPROBE_CORS_ORIGINS` | browser origins allowed to call the UI API |
| `NETPROBE_SESSION_TTL` | the length of a login to the web UI (default 12 hours) |
| `NETPROBE_SECRET_KEY_FILE` | the key that encrypts the secrets of the channels |
| `NETPROBE_ALERT_FAILURES`, `NETPROBE_EDGE_SILENCE`, `NETPROBE_ALERT_INTERVAL` | when an incident opens, and how often it is looked for (3, 5 minutes, 30 seconds) |
| `NETPROBE_WEBHOOK_DENY` | where the webhooks may not connect |
| `NETPROBE_RETENTION`, `NETPROBE_AUDIT_RETENTION` | how long results and the audit trail are kept (results for ever, audit a year) |
| `NETPROBE_GRAFANA_ROLE` | the database role the central grants read access to |
| `NETPROBE_LOG_LEVEL` | debug, info, warn or error |

## The edge

`netprobe-edge`:

| Variable | What |
|---|---|
| `NETPROBE_CENTRAL` | the address of the central |
| `NETPROBE_TOKEN` or `NETPROBE_TOKEN_FILE` | the token of the edge |
| `NETPROBE_CA_FILE` | a private authority that signed the certificate of the central |
| `NETPROBE_CLIENT_CERT`, `NETPROBE_CLIENT_KEY` | the certificate of the edge, when the central asks for one |
| `NETPROBE_POLL_INTERVAL`, `NETPROBE_REPORT_INTERVAL` | how often it asks for checks (30 s) and sends results (10 s) |
| `NETPROBE_DENY` | ranges never probed, CIDRs separated by commas, or `none`; it replaces the built-in list |
| `NETPROBE_LOG_LEVEL` | debug, info, warn or error |

## The web image

| Variable | What |
|---|---|
| `NETPROBE_API_URL` | where the central's UI API is; the image writes it to `config.json` |

## The compose stack

`compose.yaml` reads a `.env` file. Everything has a default; copy `.env.example` to change it.

| Variable | What |
|---|---|
| `NETPROBE_DOMAIN`, `NETPROBE_PUBLIC_URL` | the name people type, and the address the web UI uses to reach the API. Caddy gets a certificate for the name |
| `NETPROBE_GRAFANA_DOMAIN`, `NETPROBE_GRAFANA_URL` | the same for Grafana, which needs a name of its own |
| `NETPROBE_UI_ALLOW` | who may see the UI and Grafana: addresses and ranges, or `private_ranges` |
| `NETPROBE_HTTP_PORT`, `NETPROBE_HTTPS_PORT` | ports of the host (Caddy needs 80 and 443 for a real name) |
| `NETPROBE_BIND` | the address the proxy listens on; `127.0.0.1` keeps it off the network, as a tunnel needs |
| `NETPROBE_TIMEZONE` | the time zone of the images Grafana makes |
| `NETPROBE_RETENTION` | how long results are kept: `2160h` is 90 days, `0` keeps them for ever |
| `NETPROBE_VERSION` | the version of the images, a release such as `1.0.0`, or `latest` |

See also [Docker stack](docker.md) and [Deploying](deploy.md).

---

Previous: [Architecture](architecture.md) | Next: [Command line](cli.md)
