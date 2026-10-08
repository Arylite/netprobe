[Docs](README.md) / Go deeper

# API

Everything the web UI does goes through an HTTP API that you can call too.

> **Level:** advanced | **Contract:** `GET /api/v1/openapi.yaml`

The UI API is a separate listener (`--api-listen`) from the edge API (`--edge-listen`), so one can
face the internet and the other an intranet. The web UI is a separate application: it is hosted
wherever you like and calls this API.

## Try it

From the source, with a database running (`make build dev-db`):

```sh
export NETPROBE_DATABASE_URL='postgres://netprobe:netprobe@127.0.0.1:5432/netprobe?sslmode=disable'

printf '%s\n' 'a long admin password' | bin/netprobe-central user add --username alice --role admin
bin/netprobe-central serve        # edge API on 127.0.0.1:8080, UI API on 127.0.0.1:8081
```

Then do what the web UI does:

```sh
API=http://127.0.0.1:8081
TOKEN=$(curl -s $API/api/v1/login -d '{"username":"alice","password":"a long admin password"}' | jq -r .token)
EDGE=$(curl -s $API/api/v1/edges -H "Authorization: Bearer $TOKEN" -d '{"name":"paris"}' | jq -r .token)
curl -s $API/api/v1/checks -H "Authorization: Bearer $TOKEN" -d '{"id":"web","kind":"http","target":"https://example.com","interval_seconds":30}'

NETPROBE_TOKEN=$EDGE bin/netprobe-edge --central http://127.0.0.1:8080

curl -s "$API/api/v1/checks/web/results?limit=5" -H "Authorization: Bearer $TOKEN"
```

## How it works

| Topic | Behaviour |
|---|---|
| Sign in | `POST /api/v1/login` trades a username and a password for an opaque session token |
| Using the token | send it as `Authorization: Bearer ...` |
| Lifetime | 12 hours by default (`--session-ttl`, `NETPROBE_SESSION_TTL`) |
| First account | `GET` and `POST /api/v1/setup` create the first administrator of an empty central |
| CSRF | none to worry about: there are no cookies, so there is nothing for another site to forge |
| Roles | `viewer` reads everything; `admin` also manages edges, checks, channels and users and reads the audit log |
| Protections | nobody can delete their own account or the last administrator |
| Browsers on another origin | must be named in `--cors-origins` (`scheme://host[:port]`, no wildcard); by default none is allowed |
| One call for a dashboard | `GET /api/v1/status` summarises each check on each active edge over the last day |

The edge API (`/v1/assignments`, `/v1/results`, `/healthz`) is a different surface, made for the
edge program and authenticated with the edge token. See [Architecture](architecture.md).

---

Previous: [Web UI](web-ui.md) | Next: [Docker stack](docker.md)
