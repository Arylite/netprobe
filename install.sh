#!/bin/sh
# netprobe installer: sets up an edge on this machine, or the central with Docker.
#
#   curl -fsSL https://github.com/Arylite/netprobe/releases/download/vX.Y.Z/install.sh | sudo sh -s -- edge
#   curl -fsSL https://github.com/Arylite/netprobe/releases/download/vX.Y.Z/install.sh | sudo sh -s -- central
#
# Run `sh install.sh help` for the options. Everything it downloads is checked
# against the SHA256SUMS of the release. The whole script is one function that
# runs on its last line, so a download cut short runs nothing.

set -eu

REPO="Arylite/netprobe"
BASE_URL="${NETPROBE_BASE_URL:-https://github.com/$REPO/releases/download}"
LIST_URL="${NETPROBE_RELEASES_URL:-https://api.github.com/repos/$REPO/releases?per_page=30}"
PREFIX="${NETPROBE_PREFIX:-/usr/local}"
ETC="${NETPROBE_ETC:-/etc/netprobe}"
UNIT_DIR="${NETPROBE_SYSTEMD_DIR:-/etc/systemd/system}"
CENTRAL_DIR="${NETPROBE_DIR:-/opt/netprobe}"
EDGE_DIR="${NETPROBE_EDGE_DIR:-/opt/netprobe-edge}"

TMP=""
TAG=""
VERSION=""

say() { printf '%s\n' "$*"; }
warn() { printf 'install.sh: %s\n' "$*" >&2; }
die() {
  warn "$*"
  exit 1
}

cleanup() {
  if [ -n "$TMP" ] && [ -d "$TMP" ]; then rm -rf "$TMP"; fi
}

usage() {
  cat <<'EOF'
usage: install.sh <command> [options]

commands:
  edge       install an edge on this machine (Linux, as root)
  central    run the central, its database, the web UI and Grafana with Docker
  uninstall  remove an edge or the central: install.sh uninstall edge|central [--purge]
  help       this text

edge options:
  --central URL       the https address of the central (or NETPROBE_CENTRAL)
  --token-file FILE   the file that holds the token of the edge; else NETPROBE_TOKEN,
                      else it is asked for (the token is never a command line argument)
  --ca-file FILE      a private authority that signed the certificate of the central
  --docker            run it as a container instead of a binary and a systemd service
  --no-start          install, but do not start it

central options:
  --dir DIR           where to install (default /opt/netprobe)
  --domain NAME       the name of the UI (default localhost)
  --grafana-domain N  the name of Grafana (default grafana.<domain>)
  --tunnel FILE       publish through a Cloudflare Tunnel, with its token in FILE
                      (or - for NETPROBE_TUNNEL_TOKEN, or a prompt); no port is opened

any command:
  --version X.Y.Z     the release to install (default: the latest, or NETPROBE_VERSION)

Other settings: NETPROBE_BASE_URL (a mirror of the releases), NETPROBE_PREFIX,
NETPROBE_YES=1 (never ask).
EOF
}

# set_env FILE KEY VALUE: sets KEY in an env file, keeping the rest.
set_env() {
  awk -v k="$2" -v v="$3" 'BEGIN { FS = OFS = "=" } $1 == k { print k "=" v; found = 1; next } { print } END { if (!found) print k "=" v }' "$1" >"$1.new"
  mv "$1.new" "$1"
}

# have_tty: there is a terminal to ask at, even when stdin is the script.
have_tty() { (exec </dev/tty) 2>/dev/null; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is needed and was not found"; }

# fetch URL DEST
fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 --connect-timeout 15 -o "$2" "$1" || die "cannot download $1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$2" "$1" || die "cannot download $1"
  else
    die "curl or wget is needed"
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d ' ' -f 1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d ' ' -f 1
  else
    die "sha256sum or shasum is needed"
  fi
}

# ask VARIABLE "question" default: the variable if it is set, else what is typed
# at the terminal, else the default.
ask() {
  eval "current=\${$1:-}"
  if [ -n "$current" ]; then return 0; fi
  answer=""
  if [ "${NETPROBE_YES:-}" != "1" ] && have_tty; then
    printf '%s [%s]: ' "$2" "$3" >/dev/tty
    IFS= read -r answer </dev/tty || answer=""
  fi
  if [ -z "$answer" ]; then answer="$3"; fi
  eval "$1=\$answer"
}

# read_secret "prompt": what is typed, not shown.
read_secret() {
  have_tty || return 1
  printf '%s' "$1" >/dev/tty
  stty -echo </dev/tty 2>/dev/null || true
  secret=""
  IFS= read -r secret </dev/tty || secret=""
  stty echo </dev/tty 2>/dev/null || true
  printf '\n' >/dev/tty
  printf '%s' "$secret"
}

require_root() {
  if [ "$(id -u)" != "0" ] && [ "${NETPROBE_ALLOW_NONROOT:-}" != "1" ]; then
    die "run it as root, for example: curl -fsSL ... | sudo sh -s -- $1"
  fi
}

# resolve_version: sets TAG (vX.Y.Z) and VERSION (X.Y.Z), from --version, from
# NETPROBE_VERSION, or from the latest release. A pre-release is taken only when
# there is no other.
resolve_version() {
  wanted="${WANTED_VERSION:-${NETPROBE_VERSION:-}}"
  if [ -z "$wanted" ]; then
    fetch "$LIST_URL" "$TMP/releases.json"
    tags=$(grep -o '"tag_name": *"[^"]*"' "$TMP/releases.json" | sed 's/.*"\([^"]*\)"$/\1/')
    [ -n "$tags" ] || die "no release found: say which one with --version"
    wanted=$(printf '%s\n' "$tags" | grep -v -- '-' | head -n 1 || true)
    if [ -z "$wanted" ]; then
      wanted=$(printf '%s\n' "$tags" | head -n 1)
      warn "no stable release yet: installing the pre-release $wanted"
    fi
  fi
  VERSION="${wanted#v}"
  case "$VERSION" in
    *[!A-Za-z0-9._+-]* | "") die "\"$wanted\" is not a version" ;;
  esac
  TAG="v$VERSION"
}

# download NAME: gets a file of the release into $TMP and checks it against the
# SHA256SUMS of the release.
download() {
  if [ ! -s "$TMP/SHA256SUMS" ]; then
    fetch "$BASE_URL/$TAG/SHA256SUMS" "$TMP/SHA256SUMS"
  fi
  fetch "$BASE_URL/$TAG/$1" "$TMP/$1"
  want=$(awk -v f="$1" '$2 == f || $2 == "*" f { print $1 }' "$TMP/SHA256SUMS" | head -n 1)
  [ -n "$want" ] || die "$1 is not in the SHA256SUMS of $TAG"
  got=$(sha256 "$TMP/$1")
  [ "$want" = "$got" ] || die "the checksum of $1 is not the one of the release: nothing was installed"
  say "checked $1"
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) ARCH=amd64 ;;
    aarch64 | arm64) ARCH=arm64 ;;
    *) die "there is no build of the edge for $(uname -m): take another from the releases" ;;
  esac
}

valid_url() {
  case "$1" in
    https://[A-Za-z0-9]* | http://localhost* | http://127.0.0.1*) ;;
    *) die "the address of the central must be an https:// address, not \"$1\"" ;;
  esac
  case "$1" in
    *[[:space:]]* | *\"* | *\'* | *\\* | *\`* | *\$*) die "the address of the central has characters it should not" ;;
  esac
}

valid_name() {
  case "$1" in
    *[!A-Za-z0-9.-]* | "" | -* | .*) die "\"$1\" is not a host name" ;;
  esac
}

# ---------------------------------------------------------------- edge

cmd_edge() {
  CENTRAL="${NETPROBE_CENTRAL:-}"
  TOKEN_FILE=""
  CA_FILE=""
  USE_DOCKER=0
  START=1
  while [ $# -gt 0 ]; do
    case "$1" in
      --central) CENTRAL="${2:?--central needs an address}"; shift 2 ;;
      --token-file) TOKEN_FILE="${2:?--token-file needs a file}"; shift 2 ;;
      --ca-file) CA_FILE="${2:?--ca-file needs a file}"; shift 2 ;;
      --docker) USE_DOCKER=1; shift ;;
      --no-start) START=0; shift ;;
      --version) WANTED_VERSION="${2:?--version needs a version}"; shift 2 ;;
      *) die "unknown option for edge: $1" ;;
    esac
  done
  [ "$(uname -s)" = "Linux" ] || die "this installs an edge on Linux; for another system take the archive from the releases"
  require_root edge

  TMP=$(mktemp -d)
  resolve_version
  say "netprobe $TAG, edge"

  settings="$ETC/edge.env"
  tokenfile="$ETC/edge_token"
  if [ "$USE_DOCKER" = 1 ]; then
    settings="$EDGE_DIR/.env"
    tokenfile="$EDGE_DIR/edge_token"
  fi

  # What is there already is kept: running it again upgrades.
  if [ -z "$CENTRAL" ] && [ -r "$settings" ]; then
    CENTRAL=$(sed -n 's/^NETPROBE_CENTRAL=//p' "$settings" | head -n 1)
  fi
  ask CENTRAL "Address of the central (https://...)" ""
  [ -n "$CENTRAL" ] || die "say where the central is: --central https://netprobe.example.com"
  valid_url "$CENTRAL"

  # The token is read and checked before anything is installed.
  TOKEN_VALUE=""
  if [ -n "$TOKEN_FILE" ]; then
    [ -r "$TOKEN_FILE" ] || die "cannot read $TOKEN_FILE"
    TOKEN_VALUE=$(tr -d ' \r\n' <"$TOKEN_FILE")
  elif [ -n "${NETPROBE_TOKEN:-}" ]; then
    TOKEN_VALUE=$(printf '%s' "$NETPROBE_TOKEN" | tr -d ' \r\n')
  elif [ ! -s "$tokenfile" ]; then
    TOKEN_VALUE=$(read_secret "Token of the edge (np_...): ") || die "give the token in NETPROBE_TOKEN or in --token-file: it is not asked for without a terminal"
    TOKEN_VALUE=$(printf '%s' "$TOKEN_VALUE" | tr -d ' \r\n')
  fi
  if [ -z "$TOKEN_VALUE" ] && [ ! -s "$tokenfile" ]; then
    die "no token: give it in NETPROBE_TOKEN or in --token-file"
  fi
  if [ -n "$TOKEN_VALUE" ]; then
    case "$TOKEN_VALUE" in
      np_?*) ;;
      *) die "that is not the token of an edge: it starts with np_ and is shown once, when the edge is added" ;;
    esac
  fi

  if [ "$USE_DOCKER" = 1 ]; then
    edge_with_docker
  else
    edge_with_systemd
  fi
}

# put_token DEST: the token, readable by its owner alone; kept when none was given.
put_token() {
  [ -n "$TOKEN_VALUE" ] || return 0
  (
    umask 077
    printf '%s' "$TOKEN_VALUE" >"$1"
  )
}

edge_with_systemd() {
  detect_arch
  need tar
  archive="netprobe_${TAG}_linux-${ARCH}.tar.gz"
  download "$archive"
  download "netprobe-deploy_${TAG}.tar.gz"

  mkdir -p "$TMP/x"
  tar -xzf "$TMP/$archive" -C "$TMP/x" --strip-components=1 "netprobe_${TAG}_linux-${ARCH}/netprobe-edge"
  tar -xzf "$TMP/netprobe-deploy_${TAG}.tar.gz" -C "$TMP/x" --strip-components=1 "netprobe-deploy_${TAG}/deploy/systemd/netprobe-edge.service"

  # A private authority given before is kept.
  if [ -z "$CA_FILE" ] && [ -r "$ETC/edge.env" ] && grep -q '^NETPROBE_CA_FILE=' "$ETC/edge.env"; then
    CA_FILE=$(sed -n 's/^NETPROBE_CA_FILE=//p' "$ETC/edge.env" | head -n 1)
  fi

  mkdir -p "$PREFIX/bin"
  install -m 0755 "$TMP/x/netprobe-edge" "$PREFIX/bin/netprobe-edge"
  install -d -m 0750 "$ETC"
  if [ -n "$CA_FILE" ] && [ "$CA_FILE" != "$ETC/ca.pem" ]; then
    install -m 0644 "$CA_FILE" "$ETC/ca.pem"
    CA_FILE="$ETC/ca.pem"
  fi
  {
    printf 'NETPROBE_CENTRAL=%s\n' "$CENTRAL"
    if [ -n "$CA_FILE" ]; then printf 'NETPROBE_CA_FILE=%s\n' "$CA_FILE"; fi
  } >"$ETC/edge.env"
  chmod 0640 "$ETC/edge.env"
  put_token "$ETC/edge_token"
  say "installed $PREFIX/bin/netprobe-edge, with its settings in $ETC"

  if [ -d /run/systemd/system ] || [ "${NETPROBE_FORCE_SYSTEMD:-}" = "1" ]; then
    mkdir -p "$UNIT_DIR"
    sed "s#/usr/local/bin/netprobe-edge#$PREFIX/bin/netprobe-edge#; s#/etc/netprobe/#$ETC/#g" \
      "$TMP/x/deploy/systemd/netprobe-edge.service" >"$UNIT_DIR/netprobe-edge.service"
    chmod 0644 "$UNIT_DIR/netprobe-edge.service"
    if [ "$START" = 1 ]; then
      systemctl daemon-reload
      systemctl enable --now netprobe-edge
      say "the edge runs: journalctl -u netprobe-edge -f"
    else
      say "not started: systemctl enable --now netprobe-edge"
    fi
  else
    warn "this machine does not run systemd: start it yourself, with NETPROBE_CENTRAL and NETPROBE_TOKEN_FILE=$ETC/edge_token set"
  fi

  say "checking the way to the central:"
  NETPROBE_CENTRAL="$CENTRAL" NETPROBE_TOKEN_FILE="$ETC/edge_token" NETPROBE_CA_FILE="$CA_FILE" \
    "$PREFIX/bin/netprobe-edge" doctor || warn "doctor found something: it says what to try, above"
}

edge_with_docker() {
  need docker
  docker compose version >/dev/null 2>&1 || die "the docker compose plugin is needed"
  need tar
  download "netprobe-deploy_${TAG}.tar.gz"
  mkdir -p "$EDGE_DIR"
  tar -xzf "$TMP/netprobe-deploy_${TAG}.tar.gz" -C "$EDGE_DIR" --strip-components=3 "netprobe-deploy_${TAG}/deploy/edge/compose.yaml"
  (
    umask 077
    printf 'NETPROBE_CENTRAL=%s\nNETPROBE_VERSION=%s\n' "$CENTRAL" "$VERSION" >"$EDGE_DIR/.env"
  )
  put_token "$EDGE_DIR/edge_token"
  say "installed in $EDGE_DIR"
  if [ "$START" = 1 ]; then
    (cd "$EDGE_DIR" && docker compose pull --quiet && docker compose up -d)
    say "the edge runs: cd $EDGE_DIR && docker compose logs -f"
  else
    say "not started: cd $EDGE_DIR && docker compose up -d"
  fi
}

# ------------------------------------------------------------- central

cmd_central() {
  DOMAIN="${NETPROBE_DOMAIN:-}"
  GRAFANA_DOMAIN="${NETPROBE_GRAFANA_DOMAIN:-}"
  TUNNEL_FILE=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --dir) CENTRAL_DIR="${2:?--dir needs a directory}"; shift 2 ;;
      --domain) DOMAIN="${2:?--domain needs a name}"; shift 2 ;;
      --grafana-domain) GRAFANA_DOMAIN="${2:?--grafana-domain needs a name}"; shift 2 ;;
      --tunnel) TUNNEL_FILE="${2:?--tunnel needs the file of the token}"; shift 2 ;;
      --version) WANTED_VERSION="${2:?--version needs a version}"; shift 2 ;;
      *) die "unknown option for central: $1" ;;
    esac
  done
  need docker
  need tar
  docker compose version >/dev/null 2>&1 || die "the docker compose plugin is needed"
  docker info >/dev/null 2>&1 || die "docker does not answer: is it running, and may you use it?"

  TMP=$(mktemp -d)
  resolve_version
  say "netprobe $TAG, central, in $CENTRAL_DIR"

  download "netprobe-deploy_${TAG}.tar.gz"
  mkdir -p "$CENTRAL_DIR"
  tar -xzf "$TMP/netprobe-deploy_${TAG}.tar.gz" -C "$CENTRAL_DIR" --strip-components=1

  first=0
  [ -f "$CENTRAL_DIR/.env" ] || first=1
  env_file="$CENTRAL_DIR/.env"
  touch "$env_file"
  set_env "$env_file" NETPROBE_VERSION "$VERSION"

  if [ "$first" = 1 ]; then
    if [ -n "$TUNNEL_FILE" ]; then
      ask DOMAIN "Public name of the UI, as in the tunnel" ""
      [ -n "$DOMAIN" ] || die "a tunnel needs the public names: --domain netprobe.example.com"
    else
      ask DOMAIN "Name of the UI" "localhost"
    fi
    valid_name "$DOMAIN"
    if [ -z "$GRAFANA_DOMAIN" ]; then GRAFANA_DOMAIN="grafana.$DOMAIN"; fi
    valid_name "$GRAFANA_DOMAIN"
    # With a tunnel Cloudflare holds the certificates: the proxy has no name to ask for one.
    if [ -z "$TUNNEL_FILE" ]; then
      set_env "$env_file" NETPROBE_DOMAIN "$DOMAIN"
      set_env "$env_file" NETPROBE_GRAFANA_DOMAIN "$GRAFANA_DOMAIN"
    fi
    set_env "$env_file" NETPROBE_PUBLIC_URL "https://$DOMAIN"
    set_env "$env_file" NETPROBE_GRAFANA_URL "https://$GRAFANA_DOMAIN"
  fi

  if [ -n "$TUNNEL_FILE" ]; then
    if [ "$TUNNEL_FILE" != "-" ]; then
      [ -r "$TUNNEL_FILE" ] || die "cannot read $TUNNEL_FILE"
      TUNNEL_VALUE=$(cat "$TUNNEL_FILE")
    elif [ -n "${NETPROBE_TUNNEL_TOKEN:-}" ]; then
      TUNNEL_VALUE="$NETPROBE_TUNNEL_TOKEN"
    else
      TUNNEL_VALUE=$(read_secret "Token of the Cloudflare tunnel: ") || die "give the token in a file (--tunnel FILE) or in NETPROBE_TUNNEL_TOKEN"
    fi
    TUNNEL_VALUE=$(printf '%s' "$TUNNEL_VALUE" | tr -d ' \r\n')
    [ -n "$TUNNEL_VALUE" ] || die "the token of the tunnel is empty"
    (
      umask 077
      printf '%s' "$TUNNEL_VALUE" >"$CENTRAL_DIR/cloudflared_token"
    )
    # The container runs as another user and must read it: the directory keeps the others out.
    chmod 0444 "$CENTRAL_DIR/cloudflared_token"
    chmod 0750 "$CENTRAL_DIR"
    # Kept in .env, so that a later docker compose up keeps the tunnel too.
    set_env "$env_file" COMPOSE_PROFILES cloudflared
    set_env "$env_file" NETPROBE_BIND 127.0.0.1
  fi

  cd "$CENTRAL_DIR"
  docker compose pull --quiet || die "the images of $TAG cannot be pulled: are they published?"
  docker compose up -d

  say "waiting for the services to be healthy..."
  tries=0
  while :; do
    states=$(docker compose ps --format '{{.Service}} {{.Status}}')
    if printf '%s
' "$states" | grep -q '^central .*(healthy)' && ! printf '%s
' "$states" | grep -q 'health: starting'; then break; fi
    tries=$((tries + 1))
    [ "$tries" -le 80 ] || die "not healthy after 4 minutes: docker compose ps, then docker compose logs"
    sleep 3
  done

  url=$(sed -n 's/^NETPROBE_PUBLIC_URL=//p' .env | head -n 1)
  gurl=$(sed -n 's/^NETPROBE_GRAFANA_URL=//p' .env | head -n 1)
  code=$(docker compose logs --no-color central 2>/dev/null | sed -n 's/.*setup_code=\([0-9]*\).*/\1/p' | tail -n 1)
  say ""
  say "netprobe is running."
  say "  UI       ${url:-https://localhost}"
  say "  Grafana  ${gurl:-https://grafana.localhost}  (admin; the password: cd $CENTRAL_DIR && docker compose exec grafana cat /grafana-secrets/grafana_admin_password)"
  if [ -n "$code" ]; then
    say "  Setup code to create the administrator: $code"
  elif [ "$first" = 0 ]; then
    say "  Already set up: sign in."
  fi
  if grep -q '^COMPOSE_PROFILES=.*cloudflared' .env; then
    say ""
    say "Cloudflare Tunnel: in the tunnel, add two public hostnames (Zero Trust, Networks, Tunnels):"
    say "  ${url#https://}   ->  HTTP  proxy:8088"
    say "  ${gurl#https://}  ->  HTTP  proxy:8089"
    say "No port is open on this machine. Put Cloudflare Access in front of what should not be public."
  fi
  say ""
  say "Back up the volumes db-data, secrets and grafana-secrets before you rely on it."
}

# ----------------------------------------------------------- uninstall

cmd_uninstall() {
  what="${1:-}"
  [ -n "$what" ] || die "say what: install.sh uninstall edge|central"
  shift
  purge=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --purge) purge=1; shift ;;
      --dir) CENTRAL_DIR="${2:?--dir needs a directory}"; EDGE_DIR="$CENTRAL_DIR"; shift 2 ;;
      *) die "unknown option for uninstall: $1" ;;
    esac
  done
  case "$what" in
    edge)
      require_root uninstall
      if [ -d /run/systemd/system ] && [ -f "$UNIT_DIR/netprobe-edge.service" ]; then
        systemctl disable --now netprobe-edge 2>/dev/null || true
      fi
      rm -f "$UNIT_DIR/netprobe-edge.service" "$PREFIX/bin/netprobe-edge"
      if [ -d /run/systemd/system ]; then systemctl daemon-reload 2>/dev/null || true; fi
      if [ -f "$EDGE_DIR/compose.yaml" ]; then
        (cd "$EDGE_DIR" && docker compose down) || true
        if [ "$purge" = 1 ]; then rm -rf "$EDGE_DIR"; fi
      fi
      if [ "$purge" = 1 ]; then
        rm -rf "$ETC"
        say "edge removed, with its settings and its token"
      else
        say "edge removed; $ETC is kept, token included: add --purge to delete it"
      fi
      ;;
    central)
      [ -f "$CENTRAL_DIR/compose.yaml" ] || die "no central in $CENTRAL_DIR"
      cd "$CENTRAL_DIR"
      if [ "$purge" = 1 ]; then
        if [ "${NETPROBE_YES:-}" != "1" ]; then
          answer=""
          if have_tty; then
            printf 'This deletes the database and the secrets of %s. Type "delete" to go on: ' "$CENTRAL_DIR" >/dev/tty
            IFS= read -r answer </dev/tty || answer=""
          fi
          [ "$answer" = "delete" ] || die "not confirmed: nothing was deleted"
        fi
        docker compose --profile cloudflared down -v
        say "central removed, with its data"
      else
        docker compose --profile cloudflared down
        say "central stopped; its volumes, the database and the secrets, are kept: add --purge to delete them"
      fi
      ;;
    *) die "unknown: $what (edge or central)" ;;
  esac
}

# ---------------------------------------------------------------- main

main() {
  trap cleanup EXIT
  command="${1:-help}"
  if [ $# -gt 0 ]; then shift; fi
  case "$command" in
    edge) cmd_edge "$@" ;;
    central) cmd_central "$@" ;;
    uninstall) cmd_uninstall "$@" ;;
    help | -h | --help) usage ;;
    *)
      usage >&2
      die "unknown command: $command"
      ;;
  esac
}

main "$@"
