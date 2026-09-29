#!/usr/bin/env bash
# Claude Code Stop hook: queues a status refresh when a status pane is open for this herdr pane.
cat > /dev/null
[ -z "${HERDR_STATUS_FORK:-}" ] || exit 0
[ "${HERDR_ENV:-}" = 1 ] && [ -n "${HERDR_PANE_ID:-}" ] || exit 0

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "$root/bin/lib.sh"
viewer_alive "$(pane_dir "$HERDR_PANE_ID")" || exit 0

setsid -f "$root/bin/status-refresh" "$HERDR_PANE_ID" > /dev/null 2>&1 < /dev/null
exit 0
