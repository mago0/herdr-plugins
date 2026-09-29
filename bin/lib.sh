# Shared paths and helpers for the status pane scripts.

STATE_ROOT="${XDG_STATE_HOME:-$HOME/.local/state}/herdr-status-pane"
HERDR_BIN="${HERDR_BIN_PATH:-herdr}"

# Per-agent-pane state directory; pane ids contain ':' so flatten them.
pane_dir() {
  printf '%s/%s' "$STATE_ROOT" "${1//:/_}"
}

# True while a status viewer is open for the pane in $1 (a pane_dir).
viewer_alive() {
  local pid
  pid="$(cat "$1/enabled" 2>/dev/null)" || return 1
  [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null
}
