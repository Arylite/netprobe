[Docs](README.md) / Set it up

# Grafana

netprobe draws no charts of its own. Grafana does, reading the results straight from the
database. The compose file runs one for you, already connected, with three dashboards and an
image renderer.

> **Level:** beginner for the dashboards, advanced for the rest | **Time:** 5 minutes

## Open the dashboards

Open <https://grafana.localhost> (the proxy answers for `*.localhost` with its local authority:
the browser warns once). Sign in as `admin`. The password is made at the first start:

```sh
docker compose exec grafana cat /grafana-secrets/grafana_admin_password
```

| Dashboard | For |
|---|---|
| **netprobe - Overview** | open incidents, success rate, median and p95 response time, a chart of both per check and edge, failed runs, and where every check stands now |
| **netprobe - A check** | one check in detail: percentiles, how the response time is spread, availability per edge, why it fails, its incidents |
| **netprobe - Incidents** | how many, where, for how long, and the mean time to resolve |

Each has a selector for the check and the edge, a period, and links to the others. They refresh
every 30 seconds, and `deploy/grafana/dashboards` holds their JSON.

## Get an image of a panel or a dashboard

Handy for a daily report or a chat message. The image renderer is a second container,
`renderer`, with a browser in it. Grafana calls it; it is on a network shared with Grafana alone
and has no way out. Only Grafana, with a token it holds, may ask it for an image.

Ask Grafana for the image of a whole dashboard, or of one panel (`d-solo`, with the number of the
panel in `panelId`):

```
https://grafana.localhost/render/d/netprobe-overview/_?width=1600&height=1300&kiosk&from=now-24h&to=now
https://grafana.localhost/render/d-solo/netprobe-overview/_?panelId=7&width=1000&height=450&from=now-6h&to=now
```

| Add | To |
|---|---|
| `var-check=web` | choose a check |
| `kiosk` | leave the menus out |
| `timeout=60` | wait longer for a slow one |

### From a script

A script should not hold the password of the administrator. Make an account that can only read,
and a token for it, once:

```sh
PW=$(docker compose exec -T grafana cat /grafana-secrets/grafana_admin_password)
curl -s -u "admin:$PW" -H 'Content-Type: application/json' \
  -d '{"name":"images","role":"Viewer"}' https://grafana.localhost/api/serviceaccounts
# {"id":2,...}
curl -s -u "admin:$PW" -H 'Content-Type: application/json' \
  -d '{"name":"images-token"}' https://grafana.localhost/api/serviceaccounts/2/tokens
# {"key":"glsa_..."}
```

Then, for example every morning from cron:

```sh
curl -s -H "Authorization: Bearer $KEY" -o overview.png \
  "https://grafana.localhost/render/d/netprobe-overview/_?width=1600&height=1300&kiosk&from=now-24h&to=now"
```

With the local authority of the proxy, add `-k`, or give `curl` its certificate. The token can
read and render, and nothing more: it is refused when it tries to make a dashboard.

### What it costs

An image takes a few seconds and the browser holds up to 1 GB while it works. Grafana asks for at
most five at a time (`GF_RENDERING_CONCURRENT_RENDER_REQUEST_LIMIT`). The time zone of the images
is `NETPROBE_TIMEZONE`. To do without the renderer, remove the `renderer` service and the
`GF_RENDERING_*` settings of Grafana.

## Where the data comes from

Results go to a TimescaleDB hypertable, `results` (`edge_id`, `check_id`, `at`, `ok`,
`rtt_millis`, `error`), compressed after 7 days. They are kept for ever unless `--retention`
(`NETPROBE_RETENTION`) says how long, for example `--retention 2160h` for 90 days (the compose
file does). Older data is dropped in the background, and the setting is applied each time the
central starts.

For example, the response time of a check, one line per edge:

```sql
SELECT $__timeGroup(r.at, $__interval) AS time, e.name AS metric, avg(r.rtt_millis) AS value
FROM results r JOIN edges e ON e.id = r.edge_id
WHERE $__timeFilter(r.at) AND r.check_id = '$check' AND r.ok
GROUP BY 1, 2 ORDER BY 1
```

## Advanced

### How Grafana is connected

Grafana reads the database as the role `grafana`, never as the central:

- It is created by the `grafana-init` service with a password of its own, and may do nothing but
  log in (10 connections at most), read (`default_transaction_read_only`) and run a query for 30
  seconds at most.
- The central gives it `SELECT` on `results`, `checks` and `incidents`, and on the id, name and
  dates of `edges`. Not on the hash of the tokens, not on the accounts, the sessions, the
  channels or the audit trail. It does so each time it starts (`NETPROBE_GRAFANA_ROLE`), and the
  grants can be repeated.
- The database is on a network with no way out, and Grafana is the only thing besides the central
  on it. The connection is not encrypted for that reason.

Grafana has a name of its own, and not a path under the UI, on purpose: another origin, so that
nothing it runs can read what the UI keeps in the browser.

### What is set, and why

Everything is in `compose.yaml`, under `grafana`:

| Setting | Why |
|---|---|
| Secure cookies, `SameSite=lax`, HSTS, a Content-Security-Policy, no embedding | it is served over HTTPS and should not be framed |
| No sign-up, no anonymous access, no public dashboards, no external snapshots | nothing leaves without someone signing in |
| No analytics, no update check, no news feed, no plugin installation from the screen | no traffic to Grafana Labs |
| Grafana alerting off | netprobe opens the incidents and calls the webhooks |
| Home page is the overview, light theme, 10 s minimum refresh | what people want first, and a database that cannot be hammered |
| Read-only root file system, no capability, no new privileges, 512 MB | like every other container here |
| Dashboards provisioned, not editable from the screen | what you see is what is in `deploy/grafana/dashboards`; to change one, copy it |

### A Grafana of your own

Create the role and give it what it needs. The central does the second half when
`NETPROBE_GRAFANA_ROLE` is set; this is the same by hand:

```sql
CREATE ROLE grafana LOGIN PASSWORD '...' CONNECTION LIMIT 10;
ALTER ROLE grafana SET statement_timeout = '30s';
ALTER ROLE grafana SET default_transaction_read_only = on;
GRANT CONNECT ON DATABASE netprobe TO grafana;
GRANT USAGE ON SCHEMA public TO grafana;
GRANT SELECT ON results, checks, incidents TO grafana;
GRANT SELECT (id, name, created_at, revoked_at) ON edges TO grafana;
```

> [!WARNING]
> Never `GRANT SELECT ON edges`: the table holds the hash of every token. The same goes for
> `users` and `channels` (secrets).

Add a PostgreSQL data source with the database, that role, TimescaleDB switched on, and the uid
`netprobe`. Copy `deploy/grafana/dashboards/*.json` in, and the dashboards work as they are: they
name the data source by that uid.

## When it does not work

| You see | It means | Do |
|---|---|---|
| The test of the data source fails | the role is missing or its password changed | `docker compose up -d grafana-init` makes it again |
| A panel says `permission denied for table` | the central has not granted the role yet | check that `NETPROBE_GRAFANA_ROLE` is set, and restart the central; its log says why |
| Panels are empty | no result in the period, or the check and edge selectors are empty | widen the period; add a check and an edge |
| An image times out | the page needs longer than the renderer waits | add `timeout=90` to the address |
| An image is 401 | no or a wrong token | use the token of a service account, as above |

---

Previous: [Alerts](alerting.md) | Next: [Deploying](deploy.md)
