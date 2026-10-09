#!/usr/bin/env bash
# Smoke test: the tree binary against an isolated Herdr session. Needs herdr, tmux and go.
set -euo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d /tmp/tree-smoke.XXXXXX)"
tmux_socket="tree-smoke-$$"

# Run a command against the isolated session only.
iso() {
  env -u HERDR_SOCKET_PATH -u HERDR_ENV -u HERDR_PANE_ID -u HERDR_TAB_ID -u HERDR_WORKSPACE_ID \
    -u HERDR_BIN_PATH -u HERDR_CONFIG_PATH -u TMUX XDG_CONFIG_HOME="$work/cfg" XDG_STATE_HOME="$work/state" "$@"
}

cleanup() {
  iso herdr server stop >/dev/null 2>&1 || true
  tmux -L "$tmux_socket" kill-server >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

mkdir -p "$work/cfg/herdr" "$work/state" "$work/a" "$work/b"
(cd "$here" && go build -o bin/tree ./cmd/tree)

iso tmux -L "$tmux_socket" new-session -d -s smoke -x 120 -y 30 -c "$work/a" herdr
sock="$work/cfg/herdr/herdr.sock"
for _ in $(seq 1 50); do [ -S "$sock" ] && break; sleep 0.1; done
[ -S "$sock" ] || { echo "FAIL: the isolated Herdr server did not start"; exit 1; }
[ "$(iso herdr plugin list | head -1)" = "No plugins installed." ] || { echo "FAIL: the session is not isolated"; exit 1; }

iso herdr workspace create --cwd "$work/b" --label second --no-focus >/dev/null

out="$(iso env HERDR_SOCKET_PATH="$sock" "$here/bin/tree" --once)"
echo "$out"
[ "$out" = " ▸ No agent (2)" ] || { echo "FAIL: expected one 'No agent (2)' line"; exit 1; }

if iso "$here/bin/tree" --once >/dev/null 2>&1; then
  echo "FAIL: tree must exit non-zero outside Herdr"; exit 1
fi
echo "PASS"
