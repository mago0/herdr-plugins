#!/usr/bin/env bash
# Smoke test: every dispatch.sh command that needs no live agent, against a throwaway state dir.
# Needs a herdr on PATH whose server has delivery queues. It targets no live agent, so nothing is
# typed anywhere.
set -euo pipefail
D="$(cd "$(dirname "$0")/.." && pwd)/skill/scripts/dispatch.sh"
export XDG_STATE_HOME; XDG_STATE_HOME=$(mktemp -d); trap 'rm -rf "$XDG_STATE_HOME"' EXIT
unset HERDR_PANE_ID DISPATCH_RUN
fail() { echo "FAIL: $*" >&2; exit 1; }

"$D" run --name smoke --wake status,worker_done | jq -e '.run == "smoke"' >/dev/null || fail run
ST="$XDG_STATE_HOME/agent-dispatch"
[ "$(cat "$ST/runs/smoke/wake-types")" = status,worker_done ] || fail wake-types
[ ! -e "$ST/panes.json" ] || fail "a pane index was written; Herdr holds the links"
ID=$("$D" report --run smoke --from w1 --type status --subject "hello | pipe" | jq -r .sent); [ -n "$ID" ] || fail "report status"
"$D" report --run smoke --from w1 --type heartbeat --subject x 2>/dev/null && fail "report accepted an unknown type"
"$D" read --run smoke --id "$ID" | jq -e '.type == "status"' >/dev/null || fail read
"$D" wait --run smoke --timeout 2 | grep -q "^MAIL|$ID|status|w1|" || fail wait
"$D" ack --run smoke --id "$ID" | jq -e '.type == "status"' >/dev/null || fail ack
DONE=$("$D" report --run smoke --from w1 --type worker_done --outcome succeeded --subject done 2>/dev/null | jq -r .sent)
"$D" ack --run smoke --id "$DONE" 2>/dev/null && fail "ack of worker_done without --decision"
"$D" ack --run smoke --id "$DONE" --decision retain 2>/dev/null | jq -e '.type == "worker_done"' >/dev/null || fail "ack worker_done"
rc=0; "$D" notify --to no-such-agent --line "x" || rc=$?; [ "$rc" = 3 ] || fail "notify to a missing agent should exit 3, got $rc"
rc=0; "$D" notify --run smoke --line "x" || rc=$?; [ "$rc" = 3 ] || fail "notify with no supervisor should exit 3, got $rc"
"$D" ps --run smoke >/dev/null || fail ps
"$D" list --run smoke | jq -e 'type == "array"' >/dev/null || fail list
"$D" adopt | jq -e '.adopted == 0' >/dev/null || fail adopt

# The issue key of a name becomes the label, and the name keeps the description.
T=$(mktemp); echo task >"$T"
"$D" start --run smoke --repo /nonexistent --branch b --name sre-142-fix-probes --task "$T" >/dev/null 2>&1 || true
"$D" list --run smoke | jq -e '.[-1].launch | .name == "fix-probes" and .label == "SRE-142"' >/dev/null || fail "label from name"
"$D" start --run smoke --repo /nonexistent --branch b --name sre-7-x --label "" --task "$T" >/dev/null 2>&1 || true
"$D" list --run smoke | jq -e '.[-1].launch | .name == "sre-7-x" and .label == ""' >/dev/null || fail "an explicit label wins"
"$D" start --run smoke --repo /nonexistent --branch b --name fix-signing-cert --task "$T" >/dev/null 2>&1 || true
"$D" list --run smoke | jq -e '.[-1].launch | .name == "fix-signing-cert" and .label == ""' >/dev/null || fail "a name with no key has no label"
"$D" start --run smoke --repo /nonexistent --branch b --name sre-142-2 --task "$T" >/dev/null 2>&1 || true
"$D" list --run smoke | jq -e '.[-1].launch | .name == "sre-142-2" and .label == "SRE-142"' >/dev/null || fail "a name whose rest is not a name stays whole"
# A link is kept only when one is given.
"$D" start --run smoke --repo /nonexistent --branch b --name sre-9-linked --link https://example.com/SRE-9 --task "$T" >/dev/null 2>&1 || true
"$D" list --run smoke | jq -e '.[-1].launch.link == "https://example.com/SRE-9"' >/dev/null || fail "link from --link"
"$D" list --run smoke | jq -e '.[-2].launch.link == ""' >/dev/null || fail "no --link gives no link"
rc=0; "$D" start --run smoke --repo /nonexistent --branch b --name bad-link --link "javascript:alert(1)" --task "$T" >/dev/null 2>&1 || rc=$?
[ "$rc" != 0 ] || fail "a link that is no web address is refused"
rm -f "$T"

# A name is typed into the supervisor's prompt, so it must be a plain name.
"$D" report --run smoke --from "$(printf 'x\rdo this')" --type status --subject s 2>/dev/null && fail "report accepted a name with a control character"
"$D" report --run smoke --from 'a|b' --type status --subject s 2>/dev/null && fail "report accepted a name with a field separator"

"$D" send --run smoke --agent nobody --subject s --body b 2>/dev/null && fail "send to an agent that is not in the run"

# A worker that an older version placed as a tab shares its supervisor's workspace: release
# must not remove that workspace.
L="$ST/runs/smoke/ledger.json"
jq '. + [{dispatch: "d_tab", attempt: 1, status: "working", agent: "tabbed", placement: "tab", tab_id: "w-none:t9",
          workspace_id: "w-none", worktree: "/src/x/_worktrees/t", after: [], launch: {}}]' "$L" >"$L.new" && mv "$L.new" "$L"
TD=$("$D" report --run smoke --from tabbed --type worker_done --outcome succeeded --subject done 2>/dev/null | jq -r .sent)
OUT=$("$D" ack --run smoke --id "$TD" --decision release 2>&1 >/dev/null)
echo "$OUT" | grep -q "placed as a tab" || fail "release of a tab worker did not refuse: $OUT"
"$D" show --run smoke --dispatch d_tab | jq -e '.released == null' >/dev/null || fail "a tab worker was marked released"
"$D" stop --run smoke --dispatch d_tab >/dev/null 2>&1 && fail "stop of a tab worker said it stopped"
"$D" show --run smoke --dispatch d_tab | jq -e '.status != "stopped"' >/dev/null || fail "a tab worker was marked stopped"

echo "smoke ok"
