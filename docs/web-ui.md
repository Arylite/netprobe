[Docs](README.md) / Go deeper

# Web UI

The interface you use to manage netprobe: edges, checks, channels, users, incidents.

> **Level:** intermediate to advanced | **Use it for:** hosting the UI yourself, or working on it

## What it does

`web/` is a React app that manages the central:

- sign in, and an overview of every check on each edge;
- the incidents;
- pages to add and remove edges, checks, notification channels and users;
- the audit log, and changing your own password.

It draws no charts: [Grafana](grafana.md) does that. The light theme comes first, and a switch in
the header moves to the dark one.

## First start

A new central has no account. The UI then opens on a welcome page that creates the first
administrator, with the six digit setup code the central writes to its log when it starts
(`setup_code`).

- The page works once.
- Ten wrong codes, from anyone, close it for 15 minutes.
- A restart makes a new code.

An administrator also gets a short "get started" guide on the overview until an edge, a check and
a channel exist.

Signing in lasts 12 hours by default (`NETPROBE_SESSION_TTL`). The session token lives in
`sessionStorage`, so it also goes when the tab closes.

## Hosting it

It is static files, so host it anywhere. Three ways to get them:

| Way | How |
|---|---|
| A release | `netprobe-ui_vX.Y.Z.tar.gz` |
| From the source | `make web`, into `web/dist` |
| The image | the `web` image (the compose stack uses it) |

Say where the central's UI API is in `config.json`, next to `index.html` (the image makes it from
`NETPROBE_API_URL`):

```json
{ "apiUrl": "https://central.example.com:8081" }
```

Then name the origin that serves the UI in the central's `--cors-origins`. The compose stack puts
everything on one name, so it needs no CORS.

The page loads only its own scripts (a Content-Security-Policy is part of `index.html`). A host
that wants `connect-src` narrowed to the central can send its own header, as the image does.

## Working on it

```sh
cd web
npm ci
npm run dev       # http://127.0.0.1:5173 (add that origin to --cors-origins)
npm test          # its tests
npm run api       # regenerate the typed client after a change of internal/central/webapi/openapi.yaml
```

---

Previous: [Command line](cli.md) | Next: [API](api.md)
