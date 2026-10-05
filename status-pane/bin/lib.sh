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

# True when the screen is locked; an unknown lock state counts as unlocked.
screen_locked() {
  if command -v omarchy-hyprland-session-locked > /dev/null 2>&1 && omarchy-hyprland-session-locked; then
    return 0
  fi
  if command -v loginctl > /dev/null 2>&1 \
    && [ "$(loginctl show-session "${XDG_SESSION_ID:-auto}" -p LockedHint --value 2>/dev/null)" = yes ]; then
    return 0
  fi
  pgrep -x 'hyprlock|swaylock|gtklock|waylock|i3lock' > /dev/null 2>&1
}

# True when the viewer for the pane dir in $1 is in herdr's focused tab and the screen is unlocked.
# A viewer herdr can't find (moved, or state unknown) counts as visible.
viewer_visible() {
  local viewer snap
  screen_locked && return 1
  viewer="$(cat "$1/viewer_pane" 2>/dev/null)"
  [ -n "$viewer" ] || return 0
  snap="$("$HERDR_BIN" api snapshot 2>/dev/null)" || return 0
  jq -e --arg p "$viewer" '.result.snapshot as $s
    | ([$s.panes[] | select(.pane_id == $p) | .tab_id] | first) as $tab
    | $tab == null or $tab == $s.focused_tab_id' <<<"$snap" > /dev/null
}
