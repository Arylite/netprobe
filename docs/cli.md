[Docs](README.md) / Go deeper

# Command line

Everything the web UI does can be done from a terminal, and a few things only from here.

> **Level:** intermediate | **Use it for:** scripts, servers without a browser, repairs

## The central

`netprobe-central` takes a command:

| Command | What it does |
|---|---|
| `serve` | runs the central (edge API and UI API) |
| `edge add\|list\|revoke` | manage edges; `add` prints the token once |
| `check add\|list\|remove` | manage checks |
| `channel add\|list\|remove\|test` | manage notification channels |
| `incident list` | show incidents, open and past |
| `audit list` | read the audit trail |
| `user add\|list\|passwd\|delete` | manage accounts |
| `secret-key` | make the key that encrypts channel secrets |
| `secrets encrypt` | encrypt channels added before there was a key |
| `doctor` | check the database, schema, edges and exposure |

Run a command with `--help` for its flags. In a container:
`docker compose exec central /netprobe-central <command>`.

Passwords and secrets are read from standard input, never from a flag, because a command line is
visible to every user of the machine:

```sh
printf '%s\n' 'a long admin password' | netprobe-central user add --username alice --role admin
printf '%s\n' 'the secret' | netprobe-central channel add --name ops --url https://hooks.example.com/x --secret-stdin
```

## The edge

`netprobe-edge` runs the agent, with `--central` and a token (`NETPROBE_TOKEN` or
`--token-file`, never a flag). `netprobe-edge doctor` checks the way to the central. All the
settings are in [Configuration](configuration.md#the-edge).

## doctor

When something does not work, run `doctor` where the problem is. It checks what the program needs,
in order, stops at the first failure and says what to try:

```
$ netprobe-edge doctor --central https://central.example.com
ok    config  central https://central.example.com, token set
ok    dns     central.example.com -> 203.0.113.7
ok    tcp     connected to central.example.com:443 in 21 ms
FAIL  tls     TLS handshake failed: x509: certificate signed by unknown authority
              -> the certificate is not signed by an authority this machine trusts: ...
skip  health  not run: an earlier step failed
```

| Side | It checks |
|---|---|
| `netprobe-edge doctor` | the settings, the name, the network path, the certificate (and when it expires), the central's health, the token and the clocks |
| `netprobe-central doctor` | the database, TimescaleDB and its version, the schema, the edges and checks, edges that stopped reporting, open incidents, whether channel secrets are encrypted, the retention, and how each surface is exposed (or when its certificate expires) |

Neither changes anything: the central's check reads the database without migrating it. The exit
status is 1 when a step failed, so it works in scripts and monitoring.

---

Previous: [Configuration](configuration.md) | Next: [Web UI](web-ui.md)
