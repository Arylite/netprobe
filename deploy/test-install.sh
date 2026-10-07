#!/bin/sh
# Tests install.sh against a fake release, with busybox sh. Run it in Alpine:
#   docker run --rm -v "$PWD:/repo:ro" alpine:3 sh /repo/deploy/test-install.sh

set -eu
apk add --no-cache curl tar coreutils >/dev/null

W=/work
REPO=/repo
rm -rf "$W" && mkdir -p "$W/rel/v1.2.3" "$W/src" "$W/shim"
cd "$W/src"

# The fake release: an edge that only reports how it was called, and the bundle.
for arch in amd64 arm64; do
  d="netprobe_v1.2.3_linux-$arch"
  mkdir "$d"
  printf '#!/bin/sh\necho "fake edge: $* central=$NETPROBE_CENTRAL"\n' >"$d/netprobe-edge"
  chmod +x "$d/netprobe-edge"
  tar -czf "$W/rel/v1.2.3/$d.tar.gz" "$d"
done
b=netprobe-deploy_v1.2.3
mkdir -p "$b/deploy/systemd" "$b/deploy/edge"
cp "$REPO/deploy/systemd/netprobe-edge.service" "$b/deploy/systemd/"
cp "$REPO/deploy/edge/compose.yaml" "$b/deploy/edge/"
tar -czf "$W/rel/v1.2.3/$b.tar.gz" "$b"
(cd "$W/rel/v1.2.3" && sha256sum ./*.tar.gz | sed 's# \./# #' >SHA256SUMS)

printf '#!/bin/sh\necho "systemctl $*"\n' >"$W/shim/systemctl"
chmod +x "$W/shim/systemctl"
PATH="$W/shim:$PATH"
export PATH
export NETPROBE_BASE_URL="file://$W/rel" NETPROBE_PREFIX="$W/out/usr" NETPROBE_ETC="$W/out/etc"
export NETPROBE_SYSTEMD_DIR="$W/out/unit" NETPROBE_FORCE_SYSTEMD=1 NETPROBE_YES=1 NETPROBE_ALLOW_NONROOT=1

fails=0
# check "what" expected-text -- command...: the command's output must hold the text.
check() {
  what="$1"
  want="$2"
  shift 3
  out=$("$@" 2>&1) || true
  case "$out" in
    *"$want"*) echo "ok    $what" ;;
    *)
      echo "FAIL  $what: wanted \"$want\" in:"
      echo "$out"
      fails=$((fails + 1))
      ;;
  esac
}
install_sh="sh $REPO/install.sh"

check "help" "usage: install.sh" -- $install_sh help
check "unknown command" "unknown command" -- $install_sh nope
check "no token without a terminal" "give the token" -- $install_sh edge --version 1.2.3 --central https://n.example.com
check "central must be https" "must be an https" -- $install_sh edge --version 1.2.3 --central http://n.example.com --token-file /dev/null

printf 'xx_bad' >"$W/tok"
check "a token must start with np_" "starts with np_" -- $install_sh edge --version 1.2.3 --central https://n.example.com --token-file "$W/tok"
[ ! -e "$W/out/usr/bin/netprobe-edge" ] && echo "ok    nothing installed before the checks pass" || { echo "FAIL  installed with a bad token"; fails=$((fails + 1)); }

printf 'np_abc123\n' >"$W/tok"
check "edge install" "fake edge: doctor central=https://n.example.com" -- $install_sh edge --version 1.2.3 --central https://n.example.com --token-file "$W/tok"
[ -x "$W/out/usr/bin/netprobe-edge" ] && echo "ok    binary installed" || { echo "FAIL  binary"; fails=$((fails + 1)); }
[ "$(cat "$W/out/etc/edge_token")" = "np_abc123" ] && echo "ok    token stored" || { echo "FAIL  token"; fails=$((fails + 1)); }
[ "$(stat -c %a "$W/out/etc/edge_token")" = "600" ] && echo "ok    token mode 600" || { echo "FAIL  token mode"; fails=$((fails + 1)); }
grep -q "ExecStart=$W/out/usr/bin/netprobe-edge" "$W/out/unit/netprobe-edge.service" && echo "ok    unit points at the binary" || { echo "FAIL  unit"; fails=$((fails + 1)); }
grep -q "LoadCredential=token:$W/out/etc/edge_token" "$W/out/unit/netprobe-edge.service" && echo "ok    unit points at the token" || { echo "FAIL  unit token"; fails=$((fails + 1)); }

check "upgrade keeps the settings" "fake edge: doctor central=https://n.example.com" -- $install_sh edge --version 1.2.3
[ "$(cat "$W/out/etc/edge_token")" = "np_abc123" ] && echo "ok    token kept on upgrade" || { echo "FAIL  token lost"; fails=$((fails + 1)); }

a="$W/rel/v1.2.3/netprobe_v1.2.3_linux-amd64.tar.gz"
cp "$a" "$a.bak"
echo tamper >>"$a"
check "a changed archive is refused" "checksum" -- $install_sh edge --version 1.2.3
mv "$a.bak" "$a"

echo '[{"tag_name": "v1.3.0-rc.1"},{"tag_name": "v1.2.3"},{"tag_name": "v1.2.2"}]' >"$W/rel/releases.json"
NETPROBE_RELEASES_URL="file://$W/rel/releases.json"
export NETPROBE_RELEASES_URL
check "the latest stable release is chosen" "netprobe v1.2.3" -- $install_sh edge --no-start

check "uninstall keeps the token" "is kept" -- $install_sh uninstall edge
[ -f "$W/out/etc/edge_token" ] && echo "ok    token kept" || { echo "FAIL  token removed"; fails=$((fails + 1)); }
check "purge deletes it" "removed, with its settings" -- $install_sh uninstall edge --purge
[ ! -e "$W/out/etc/edge_token" ] && echo "ok    token gone" || { echo "FAIL  token kept"; fails=$((fails + 1)); }

[ "$fails" = 0 ] || { echo "$fails failed"; exit 1; }
echo "all passed"
