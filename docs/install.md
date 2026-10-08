[Docs](README.md) / Set it up

# Installing with the script

One command sets up an edge on a machine, or the central with Docker.

> **Level:** beginner to intermediate | **Time:** 5 minutes | **You need:** a Linux machine and root

`install.sh` is one of the files of every release (`v1.1.0` or later), next to the binaries
and `SHA256SUMS`.

## The two commands

```sh
# an edge, on the machine to measure from
curl -fsSL https://github.com/Arylite/netprobe/releases/download/v1.1.0/install.sh \
  | sudo sh -s -- edge --central https://netprobe.example.com

# the central, with its database, the web UI and Grafana
curl -fsSL https://github.com/Arylite/netprobe/releases/download/v1.1.0/install.sh \
  | sudo sh -s -- central --domain netprobe.example.com
```

`https://github.com/Arylite/netprobe/releases/latest/download/install.sh` always points at the
newest stable release.

| You want | Jump to |
|---|---|
| a sensor on a machine | [An edge](#an-edge) |
| the server | [The central](#the-central) |
| no open port at all | [Behind a Cloudflare Tunnel](#behind-a-cloudflare-tunnel) |
| to remove it | [Taking it away](#taking-it-away) |

## Is it safe to pipe it into a shell?

You are running a script from the internet as root, so it is fair to ask. What makes it
reasonable:

| Guarantee | How |
|---|---|
| You can read it first | It is a plain text file (see below) |
| A cut download runs nothing | The whole script is one function, called on its last line |
| Everything it installs is checked | Binaries and bundles are compared with the release's `SHA256SUMS`; it stops at the first difference, before anything is written |
| The token is never an argument | A command line is visible to every user. The token comes from a file (`--token-file`), `NETPROBE_TOKEN`, or a prompt that does not echo, and is kept in a file only root can read |
| It is tested | `shellcheck` clean, and tested against a fake release with busybox `sh`, the strictest shell it is meant to run under |

To read it first, then run it:

```sh
curl -fsSLo install.sh https://github.com/Arylite/netprobe/releases/download/v1.1.0/install.sh
less install.sh
sudo sh install.sh edge --central https://netprobe.example.com
```

Pin what you trust with `--version`, and check the file itself with
`gh attestation verify install.sh --repo Arylite/netprobe`.

## An edge

```sh
sudo sh install.sh edge --central https://netprobe.example.com --token-file ./edge_token
```

It downloads the binary for the machine (amd64 or arm64) into `/usr/local/bin`, writes
`/etc/netprobe/edge.env` and `/etc/netprobe/edge_token`, installs the hardened systemd unit,
starts it, and runs `netprobe-edge doctor` so you see at once whether the way to the central is
open.

| Option | Meaning |
|---|---|
| `--central URL` | the `https://` address of the central (or `NETPROBE_CENTRAL`) |
| `--token-file FILE` | where the token is; without it, `NETPROBE_TOKEN`, then a prompt |
| `--ca-file FILE` | a private authority that signed the certificate of the central |
| `--docker` | run it as a container in `/opt/netprobe-edge` instead of a binary and a unit |
| `--no-start` | install, but do not start |
| `--version X.Y.Z` | a release; the default is the latest stable one, or a pre-release if there is no other |

> [!TIP]
> Run it again to upgrade: the settings and the token stay, only the binary changes.

## The central

```sh
sudo sh install.sh central --domain netprobe.example.com --grafana-domain netprobe-grafana.example.com
```

It needs Docker with the compose plugin. It puts the compose files in `/opt/netprobe`, writes
`.env`, pulls the images of the release, starts everything and waits until it is healthy. Then
it prints the address of the web UI, how to read Grafana's password, and the **setup code** that
creates the administrator.

| Option | Meaning |
|---|---|
| `--dir DIR` | where to install (default `/opt/netprobe`) |
| `--domain NAME` | the name of the web UI and the edge API (default `localhost`) |
| `--grafana-domain NAME` | the name of Grafana (default `grafana.<domain>`) |
| `--tunnel FILE` | publish through a Cloudflare Tunnel, with its token in `FILE` (`-` reads `NETPROBE_TUNNEL_TOKEN`, or asks) |
| `--version X.Y.Z` | a release |

> [!TIP]
> Run it again to upgrade: the `.env` and the volumes stay, the version moves.

### Behind a Cloudflare Tunnel

With `--tunnel`, nothing listens on the internet: the machine dials out to Cloudflare.

1. Make the tunnel first (Zero Trust, Networks, Tunnels, type *Cloudflared*) and save its token
   in a file.
2. Run the script with both public names:

   ```sh
   sudo sh install.sh central --domain netprobe.example.com \
     --grafana-domain netprobe-grafana.example.com --tunnel ./tunnel_token
   ```

3. Add the two public hostnames it prints to the tunnel:

   | Public hostname | Service |
   |---|---|
   | `netprobe.example.com` | `HTTP` `proxy:8088` |
   | `netprobe-grafana.example.com` | `HTTP` `proxy:8089` |

The script keeps the token in `/opt/netprobe/cloudflared_token`, turns the `cloudflared` profile
on for good (`COMPOSE_PROFILES` in `.env`, so a later `docker compose up` keeps it) and keeps
the proxy off the network. Edges then use `https://netprobe.example.com`.
[Deploying](deploy.md#with-a-cloudflare-tunnel-no-open-port) has the details, and what to put
behind Cloudflare Access.

## Taking it away

```sh
sudo sh install.sh uninstall edge             # keeps /etc/netprobe, the token included
sudo sh install.sh uninstall edge --purge
sudo sh install.sh uninstall central          # stops it, keeps the database and the secrets
sudo sh install.sh uninstall central --purge  # deletes them too, after you type "delete"
```

## Other settings

| Variable | Meaning |
|---|---|
| `NETPROBE_VERSION` | the release, as `--version` |
| `NETPROBE_YES=1` | never ask: for automation |
| `NETPROBE_BASE_URL` | a mirror of the release downloads, for a machine with no access to GitHub |
| `NETPROBE_PREFIX`, `NETPROBE_ETC`, `NETPROBE_DIR` | where the binary, the settings and the central go |

---

Previous: [Getting started](getting-started.md) | Next: [Edges](edges.md)
