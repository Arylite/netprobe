# Operating

Backups, upgrades, and what to do when something is wrong. The commands run in the folder of
`compose.yaml` (`/opt/netprobe` if the [script](install.md) installed it).

## What to keep

| What | Where | Without it |
|---|---|---|
| The results, checks, edges, users, channels | the `db-data` volume | everything is lost |
| The key of the channels, the database password | the `secrets` volume | the channels cannot be read again |
| The password and key of Grafana | the `grafana-secrets` volume | Grafana starts again with new ones; its own settings in `grafana-data` are not readable |
| Your settings | `.env`, `compose.override.yaml`, `cloudflared_token` | you write them again |

The names of the volumes start with the name of the project, the name of the folder: `netprobe_db-data`
for `/opt/netprobe`.

## Back up

A dump of the database is small and safe to take while it runs:

```sh
docker compose exec -T db pg_dump -U netprobe -Fc netprobe > netprobe-$(date +%F).dump
```

The two volumes with secrets, as an archive:

```sh
docker run --rm -v netprobe_secrets:/s:ro -v netprobe_grafana-secrets:/g:ro -v "$PWD":/b alpine \
  sh -c 'tar czf /b/secrets-$(date +%F).tgz -C / s g'
```

**Keep the two apart.** The dump holds the data and the channels (encrypted); the archive holds the key that
opens them. Together they are everything. Store them where only you can read them. Take the dump on a
schedule, for example from cron:

```sh
0 3 * * *  cd /opt/netprobe && docker compose exec -T db pg_dump -U netprobe -Fc netprobe > /backups/netprobe-$(date +\%F).dump
```

## Restore

On a machine with the stack installed but not yet used (or after `docker compose down -v`):

```sh
# 1. The secrets first, so that the stack uses them instead of making new ones.
docker volume create netprobe_secrets && docker volume create netprobe_grafana-secrets
docker run --rm -v netprobe_secrets:/s -v netprobe_grafana-secrets:/g -v "$PWD":/b alpine \
  sh -c 'tar xzf /b/secrets-DATE.tgz -C /'

# 2. Only the database, empty, with the extension.
docker compose up -d db
docker compose exec -T db psql -U netprobe -d postgres -c "DROP DATABASE IF EXISTS netprobe WITH (FORCE)" \
  -c "CREATE DATABASE netprobe TEMPLATE template0"
docker compose exec -T db psql -U netprobe -d netprobe -c "CREATE EXTENSION timescaledb" -c "SELECT timescaledb_pre_restore()"

# 3. The data.
docker compose exec -T db pg_restore -U netprobe -d netprobe --no-owner --exit-on-error < netprobe-DATE.dump
docker compose exec -T db psql -U netprobe -d netprobe -c "SELECT timescaledb_post_restore()"

# 4. Everything else.
docker compose up -d
```

The TimescaleDB steps (`pre_restore` and `post_restore`) are not optional: without them a restore of a
hypertable fails or loses its policies. The central migrates the schema when it starts, so a dump from an
older version restores into a newer one.

Try a restore once, on another machine, **before** you need it.

## Upgrade

```sh
docker compose pull && docker compose up -d       # to the version in .env
```

Change `NETPROBE_VERSION` in `.env` to move to another release (`1.0.0`, or `latest`); the script does it too:
`sh install.sh central --version X.Y.Z`. The central applies its migrations when it starts and brings the
TimescaleDB extension to the version of the server. A new major version of PostgreSQL is a dump and a restore,
not an upgrade in place. Take a dump first, and read the notes of the release.

Edges are upgraded one by one, by the same script or by pulling the image again; an edge keeps working
with a newer central, and learns a new kind of check only when it is upgraded: until then the result of that
check is a failure that says `unsupported check kind`. (Edges of `1.0.0-rc.1` refuse the whole list when
it holds a kind they do not know: upgrade them first.)

## Lost the password of the administrator

From the machine of the central, with the password on its input, not on the command line:

```sh
printf '%s\n' 'a new long password' | docker compose exec -T central /netprobe-central user passwd --username alice
```

It ends every session of that user. Without any administrator left, create one with `user add --role admin`.

## Looking at it

```sh
docker compose ps                       # every service should be healthy
docker compose logs -f central          # what the central does
docker compose exec central /netprobe-central doctor
```

`doctor` checks the database, TimescaleDB and the schema, the edges and checks, the edges that stopped
reporting, the open incidents, whether the secrets of the channels are encrypted, the retention, and how each
surface is exposed. It changes nothing.

## When something is wrong

| You see | It is | Do |
|---|---|---|
| `setup_code` does not appear in the log | the central is already set up | sign in, or `user add` to make an account |
| Caddy cannot get a certificate | the name does not point at the server, or 80/443 are closed | `docker compose logs proxy`; with a tunnel, there is nothing to get |
| The UI loads but the login fails with a network error | `NETPROBE_PUBLIC_URL` is not the address you use | fix it in `.env`, `docker compose up -d` |
| Edges say `the central refused the token` | revoked, or from another central | add the edge again |
| An edge is **Silent** | it stopped for more than 5 minutes | the machine, its network or its clock: `netprobe-edge doctor` |
| Grafana says `no data` | the data source cannot read, or the range is empty | **Connections**, **Data sources**, **Save & test**; widen the range |
| The database is full | the results are kept for ever | set `NETPROBE_RETENTION`; compression already runs after 7 days |
| A channel never receives | the address is denied, or the tool answers non-2xx | **Channels**, **Test**: the error is shown |
| `docker compose up` asks for a secret that is not there | the `secrets` volume is new | restore it, or accept new secrets: the old channels cannot be read |
| A `traceroute` or `icmp` check fails at once | the edge may not send ICMP | see [Checks](checks.md#icmp) |

## Removing it

`docker compose down` stops it and keeps the volumes. `docker compose down -v` deletes the database and the
secrets: only after a backup. With the script: `sh install.sh uninstall central [--purge]`.
