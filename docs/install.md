# Installing with the script

`install.sh` sets up an edge on a machine, or the central with Docker, in one command. It
needs a release that carries it, `v1.0.0` or later: it is one of the files of each
release, next to the binaries and `SHA256SUMS`.

```sh
# an edge, on the machine to measure from
curl -fsSL https://github.com/Arylite/netprobe/releases/download/v1.0.0/install.sh \
  | sudo sh -s -- edge --central https://netprobe.example.com

# the central, with its database, the web UI and Grafana
curl -fsSL https://github.com/Arylite/netprobe/releases/download/v1.0.0/install.sh \
  | sudo sh -s -- central --domain netprobe.example.com
```

`https://github.com/Arylite/netprobe/releases/latest/download/install.sh` always points at the
newest stable release.

## Is it safe to pipe it into a shell?

You are running a script from the internet as root, so it is fair to ask. What the script
does to make that reasonable:

- **Read it first.** It is a plain text file. Download it, read it, then run it:

  ```sh
  curl -fsSLo install.sh https://github.com/Arylite/netprobe/releases/download/v1.0.0/install.sh
  less install.sh
  sudo sh install.sh edge --central https://netprobe.example.com
  ```

- **A cut download runs nothing.** The whole script is one function, called on its last
  line.
- **Everything it installs is checked.** Binaries and bundles are compared with the
  `SHA256SUMS` of the release, and it stops at the first difference, before anything is
  written. Pin what you trust with `--version`, and check the file itself with
  `gh attestation verify install.sh --repo Arylite/netprobe`.
- **The token is not an argument.** A command line is visible to every user of the machine.
  The token comes from a file (`--token-file`), from `NETPROBE_TOKEN`, or from a prompt that
  does not echo. It is kept in a file that only root can read.
- It is `shellcheck` clean, and tested against a fake release in CI-like conditions with
  busybox `sh`, the strictest it is meant to run under.

## An edge

```sh
sudo sh install.sh edge --central https://netprobe.example.com --token-file ./edge_token
```

It downloads the binary for the machine (amd64 or arm64), puts it in
`/usr/local/bin`, writes `/etc/netprobe/edge.env` and `/etc/netprobe/edge_token`, installs
the hardened systemd unit, starts it, and runs `netprobe-edge doctor` so you see at once if
the way to the central is open.

| Option | |
|---|---|
| `--central URL` | the `https://` address of the central (or `NETPROBE_CENTRAL`) |
| `--token-file FILE` | where the token is; without it, `NETPROBE_TOKEN`, then a prompt |
| `--ca-file FILE` | a private authority that signed the certificate of the central |
| `--docker` | run it as a container in `/opt/netprobe-edge` instead of a binary and a unit |
| `--no-start` | install, but do not start |
| `--version X.Y.Z` | a release; the default is the latest stable one, or a pre-release if there is no other |

Run it again to upgrade: the settings and the token stay, only the binary changes.

## The central

```sh
sudo sh install.sh central --domain netprobe.example.com --grafana-domain netprobe-grafana.example.com
```

It needs Docker with the compose plugin. It puts the compose files in `/opt/netprobe`, writes
`.env`, pulls the images of the release, starts everything, waits until it is healthy, and
prints the address of the UI, the way to read the password of Grafana, and the setup code that
creates the administrator.

| Option | |
|---|---|
| `--dir DIR` | where to install (default `/opt/netprobe`) |
| `--domain NAME` | the name of the UI and the edge API (default `localhost`) |
| `--grafana-domain NAME` | the name of Grafana (default `grafana.<domain>`) |
| `--tunnel FILE` | publish through a Cloudflare Tunnel, with its token in `FILE` (`-` reads `NETPROBE_TUNNEL_TOKEN`, or asks) |
| `--version X.Y.Z` | a release |

Run it again to upgrade: the `.env` and the volumes stay, the version moves.

### Behind a Cloudflare Tunnel

With `--tunnel`, nothing listens on the internet: the machine dials out to Cloudflare. Make the
tunnel first (Zero Trust, Networks, Tunnels, type *Cloudflared*), save its token in a file, and give
both public names:

```sh
sudo sh install.sh central --domain netprobe.example.com   --grafana-domain netprobe-grafana.example.com --tunnel ./tunnel_token
```

The script keeps the token in `/opt/netprobe/cloudflared_token`, turns the `cloudflared` profile on
for good (`COMPOSE_PROFILES` in `.env`, so a later `docker compose up` keeps it), keeps the proxy off
the network, and prints the two public hostnames to add to the tunnel at the end:

| Public hostname | Service |
|---|---|
| `netprobe.example.com` | `HTTP` `proxy:8088` |
| `netprobe-grafana.example.com` | `HTTP` `proxy:8089` |

Edges then use `https://netprobe.example.com`. [Deploying](deploy.md) has the details, and what to
put behind Cloudflare Access.

## Taking it away

```sh
sudo sh install.sh uninstall edge             # keeps /etc/netprobe, the token included
sudo sh install.sh uninstall edge --purge
sudo sh install.sh uninstall central          # stops it, keeps the database and the secrets
sudo sh install.sh uninstall central --purge  # deletes them too, after you type "delete"
```

## Other settings

| Variable | |
|---|---|
| `NETPROBE_VERSION` | the release, as `--version` |
| `NETPROBE_YES=1` | never ask: for automation |
| `NETPROBE_BASE_URL` | a mirror of the release downloads, for a machine with no access to GitHub |
| `NETPROBE_PREFIX`, `NETPROBE_ETC`, `NETPROBE_DIR` | where the binary, the settings and the central go |
