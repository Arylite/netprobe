# netprobe documentation

Everything you need to measure your network from the places that matter, and to be told
when it breaks. Start at the top and go down: each step assumes only the ones before it.

> [!TIP]
> New here? Read [Concepts](concepts.md) (five minutes, no commands), then follow
> [Getting started](getting-started.md).

## Start here

The ideas, then a working setup. No prior knowledge needed.

| | Page | You will |
|---|---|---|
| 1 | [Concepts](concepts.md) | understand edges, the central, checks and incidents in plain words |
| 2 | [Getting started](getting-started.md) | run netprobe, add a check and get a notification, in ten minutes |

## Set it up

Put it on real machines. Each guide stands alone, in the order you will likely need them.

| Page | For |
|---|---|
| [Installing with the script](install.md) | one command for an edge or for the central |
| [Edges](edges.md) | putting an agent on a machine: script, Docker, or systemd |
| [Checks](checks.md) | choosing what to measure among eleven kinds, with examples |
| [Alerts](alerting.md) | Slack, Discord and other channels, and when an incident opens |
| [Grafana](grafana.md) | the dashboards, images of panels, a Grafana of your own |
| [Deploying](deploy.md) | a server on the internet: names, certificates, a Cloudflare Tunnel, a checklist |
| [Operating](operations.md) | backups, restores, upgrades, and what to do when something is wrong |

## Go deeper

For when you want to know exactly how it works, or to change it.

| Page | For |
|---|---|
| [Architecture](architecture.md) | how the parts talk, and how the code is organised |
| [Configuration](configuration.md) | every setting, for the central and for the edge |
| [Command line](cli.md) | the commands of both programs, and `doctor` |
| [Web UI](web-ui.md) | hosting the interface, and working on it |
| [API](api.md) | the HTTP API of the interface, with examples |
| [Docker stack](docker.md) | what `compose.yaml` runs, and how it is locked down |
| [Security](security.md) | the guarantees, and how they are kept |
| [Development](development.md) | building, testing, CI and releases |

## Quick answers

| I want to | Go to |
|---|---|
| try it on my laptop | [Getting started](getting-started.md) |
| install on a Linux server in one command | [Installing with the script](install.md) |
| measure from another site | [Edges](edges.md) |
| know if a certificate is about to expire | [Checks: tls](checks.md#tls) |
| get a message in Slack | [Alerts](alerting.md#slack-discord-mattermost) |
| back it up | [Operating: back up](operations.md#back-up) |
| find out why something fails | [Operating: when something is wrong](operations.md#when-something-is-wrong) |
