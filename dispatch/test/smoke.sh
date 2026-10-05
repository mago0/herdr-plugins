#!/usr/bin/env bash
# Smoke test: every dispatch.sh command that needs no live agent, against a throwaway state dir.
# Needs herdr on PATH. It targets no live agent, so nothing is typed anywhere.
set -euo pipefail
D="$(cd "$(dirname "$0")/.." && pwd)/skill/scripts/dispatch.sh"
export XDG_STATE_HOME; XDG_STATE_HOME=$(mktemp -d); trap 'rm -rf "$XDG_STATE_HOME"' EXIT
unset HERDR_PANE_ID DISPATCH_RUN
fail() { echo "FAIL: $*" >&2; exit 1; }

"$D" run --name smoke --wake status,worker_done | jq -e '.run == "smoke"' >/dev/null || fail run
ST="$XDG_STATE_HOME/agent-dispatch"
[ "$(cat "$ST/runs/smoke/wake-types")" = status,worker_done ] || fail wake-types
ID=$("$D" report --run smoke --from w1 --type status --subject "hello | pipe" | jq -r .sent); [ -n "$ID" ] || fail "report status"
"$D" report --run smoke --from w1 --type heartbeat --subject x 2>/dev/null && fail "report accepted an unknown type"
"$D" read --run smoke --id "$ID" | jq -e '.type == "status"' >/dev/null || fail read
"$D" wait --run smoke --timeout 2 | grep -q "^MAIL|$ID|status|w1|" || fail wait
"$D" ack --run smoke --id "$ID" | jq -e '.type == "status"' >/dev/null || fail ack
DONE=$("$D" report --run smoke --from w1 --type worker_done --outcome succeeded --subject done | jq -r .sent)
"$D" ack --run smoke --id "$DONE" 2>/dev/null && fail "ack of worker_done without --decision"
"$D" ack --run smoke --id "$DONE" --decision retain 2>/dev/null | jq -e '.type == "worker_done"' >/dev/null || fail "ack worker_done"
"$D" track --run smoke --pane zz:p1 --name loop --notify nobody
jq -e '."zz:p1" == {run: "smoke", role: "tracked", agent: "loop", supervisor: "nobody"}' "$ST/panes.json" >/dev/null || fail track
rc=0; "$D" notify --to no-such-agent --line "x" || rc=$?; [ "$rc" = 3 ] || fail "notify to a missing agent should exit 3, got $rc"
rc=0; "$D" notify --run smoke --line "x" || rc=$?; [ "$rc" = 3 ] || fail "notify with no supervisor should exit 3, got $rc"
"$D" ps --run smoke >/dev/null || fail ps
"$D" list --run smoke | jq -e 'type == "array"' >/dev/null || fail list
echo "smoke ok"
