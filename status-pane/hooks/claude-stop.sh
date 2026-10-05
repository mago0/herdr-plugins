#!/usr/bin/env bash
# Claude Code Stop hook: queues a status refresh when a status pane is open for this herdr pane.
cat > /dev/null
[ -z "${HERDR_STATUS_FORK:-}" ] || exit 0
[ "${HERDR_ENV:-}" = 1 ] && [ -n "${HERDR_PANE_ID:-}" ] || exit 0

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "$root/bin/lib.sh"
dir="$(pane_dir "$HERDR_PANE_ID")"
viewer_alive "$dir" || exit 0
# Turns since the last refresh, shown while the pane is stale.
echo $(($(cat "$dir/behind" 2>/dev/null || echo 0) + 1)) > "$dir/behind"

setsid -f "$root/bin/status-refresh" "$HERDR_PANE_ID" > /dev/null 2>&1 < /dev/null
exit 0
