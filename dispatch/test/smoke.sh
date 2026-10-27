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
"$D" ack --run smoke --id "$DONE" --decision retain 2>/dev/null && fail "retain without --reason and --until"
"$D" ack --run smoke --id "$DONE" --decision release --until x 2>/dev/null && fail "release took --until"
"$D" ack --run smoke --id "$DONE" --decision retain --reason r --until u 2>/dev/null | jq -e '.type == "worker_done"' >/dev/null || fail "ack worker_done"
# Unacked keeps inbox order and leaves out exactly the acked ids.
M="$(dirname "$D")/mail.sh" MB="$XDG_STATE_HOME/mb.jsonl"
for s in a b c d; do "$M" send --inbox "$MB" --from w --type status --subject "$s" >/dev/null; done
"$M" ack --inbox "$MB" --id "$(jq -r 'select(.subject == "b") | .id' "$MB")" --id "$(jq -r 'select(.subject == "d") | .id' "$MB")" >/dev/null
[ "$("$M" read --inbox "$MB" --unacked | jq -r .subject | paste -sd,)" = a,c ] || fail "unacked set"
[ "$("$M" read --inbox "$MB" --unacked --types question | wc -l)" = 0 ] || fail "unacked type filter"
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

# A kept worker shows why and until when, in show and ps. An entry from before has no reason.
L="$ST/runs/smoke/ledger.json"
jq '. + [{dispatch: "d_keep", attempt: 1, status: "working", agent: "keeper", after: [], launch: {}},
         {dispatch: "d_old", attempt: 1, status: "settled", outcome: "succeeded", decision: "retain", agent: "old", after: [], launch: {}}]' "$L" >"$L.new" && mv "$L.new" "$L"
KD=$("$D" report --run smoke --from keeper --type worker_done --outcome succeeded --subject done 2>/dev/null | jq -r .sent)
"$D" ack --run smoke --id "$KD" --decision retain --reason "re-review pending" --until "the re-review lands" >/dev/null 2>&1 || fail "ack retain with a reason"
"$D" show --run smoke --dispatch d_keep | jq -e '.decision_reason == "re-review pending" and .release_when == "the re-review lands"' >/dev/null || fail "show has no reason"
"$D" ps --run smoke --json | jq -e '.[] | select(.dispatch == "d_keep") | .reason == "re-review pending" and (.next | test("the re-review lands"))' >/dev/null || fail "ps has no reason"
"$D" ps --run smoke | grep -q "kept (retain): re-review pending" || fail "ps table has no reason"
"$D" ps --run smoke --json | jq -e '.[] | select(.dispatch == "d_old") | .reason == null and (.next | test("no reason recorded"))' >/dev/null || fail "ps on an entry with no reason"
SD=$("$D" report --run smoke --from keeper --type status --subject s | jq -r .sent)
"$D" ack --run smoke --id "$SD" --reason r 2>/dev/null && fail "--reason on a status ack"

# An attempt keeps Herdr's session reference of its agent, and the transcript file only when that
# file exists. A shim answers `herdr agent get` for fake panes and passes every other call on.
FA="$XDG_STATE_HOME/fake-agents"; mkdir -p "$FA" "$XDG_STATE_HOME/bin"
printf '#!/usr/bin/env bash\nif [ "$1 $2" = "agent get" ] && [ -f "%s/$3" ]; then cat "%s/$3"; exit 0; fi\nexec %q "$@"\n' \
  "$FA" "$FA" "$(command -v herdr)" >"$XDG_STATE_HOME/bin/herdr"
chmod +x "$XDG_STATE_HOME/bin/herdr"
export CLAUDE_CONFIG_DIR="$XDG_STATE_HOME/claude" CODEX_HOME="$XDG_STATE_HOME/codex"
mkdir -p "$CLAUDE_CONFIG_DIR/projects/-wt" "$CODEX_HOME/sessions/2026/10/21" "$XDG_STATE_HOME/pi"
CT="$CLAUDE_CONFIG_DIR/projects/-wt/11111111-2222-3333-4444-555555555555.jsonl"; touch "$CT"
XT="$CODEX_HOME/sessions/2026/10/21/rollout-2026-10-21T22-36-36-aaaa-bbbb.jsonl"; touch "$XT"
PT="$XDG_STATE_HOME/pi/s.jsonl"; touch "$PT"
fake() {  # <pane> <name> <agent> <kind> <value>
  jq -cn --arg n "$2" --arg p "$1" --arg a "$3" --arg k "$4" --arg v "$5" \
    '{result: {agent: {name: $n, pane_id: $p, agent: $a, agent_session: {agent: $a, kind: $k, source: ("herdr:" + $a), value: $v}}}}' >"$FA/$1"
  jq --arg d "d_s_$2" --arg n "$2" --arg p "$1" '. + [{dispatch: $d, attempt: 1, status: "working", agent: $n, pane_id: $p, after: [], launch: {}}]' "$L" >"$L.new" && mv "$L.new" "$L"
}
fake fk:1 s-claude claude id 11111111-2222-3333-4444-555555555555
fake fk:2 s-codex codex id aaaa-bbbb
fake fk:3 s-pi pi path "$PT"
fake fk:4 s-cursor cursor id 11111111-2222-3333-4444-555555555555
fake fk:5 s-gone claude id 99999999-0000-0000-0000-000000000000
fake fk:6 s-moved claude id 11111111-2222-3333-4444-555555555555
jq '.result.agent.name = "someone-else"' "$FA/fk:6" >"$FA/x" && mv "$FA/x" "$FA/fk:6"
for n in claude codex pi cursor gone moved; do PATH="$XDG_STATE_HOME/bin:$PATH" "$D" abandon --run smoke --dispatch "d_s_s-$n" >/dev/null || fail "abandon s-$n"; done
tr_of() { "$D" show --run smoke --dispatch "d_s_s-$1" | jq -r '.transcript // ""'; }
[ "$(tr_of claude)" = "$CT" ] || fail "claude transcript"
[ "$(tr_of codex)" = "$XT" ] || fail "codex transcript"
[ "$(tr_of pi)" = "$PT" ] || fail "pi transcript"
[ -z "$(tr_of cursor)" ] || fail "a kind with no known transcript got one"
"$D" show --run smoke --dispatch d_s_s-cursor | jq -e '.session.value == "11111111-2222-3333-4444-555555555555"' >/dev/null || fail "session reference not kept"
[ -z "$(tr_of gone)" ] || fail "a transcript that does not exist was recorded"
"$D" show --run smoke --dispatch d_s_s-moved | jq -e '.session == null' >/dev/null || fail "a session of another agent was recorded"
"$D" ps --run smoke --json | jq -e --arg t "$CT" '.[] | select(.dispatch == "d_s_s-claude") | .transcript == $t' >/dev/null || fail "ps has no transcript"
unset CLAUDE_CONFIG_DIR CODEX_HOME

# A worker that an older version placed as a tab shares its supervisor's workspace: release
# must not remove that workspace.
jq '. + [{dispatch: "d_tab", attempt: 1, status: "working", agent: "tabbed", placement: "tab", tab_id: "w-none:t9",
          workspace_id: "w-none", worktree: "/src/x/_worktrees/t", after: [], launch: {}}]' "$L" >"$L.new" && mv "$L.new" "$L"
TD=$("$D" report --run smoke --from tabbed --type worker_done --outcome succeeded --subject done 2>/dev/null | jq -r .sent)
OUT=$("$D" ack --run smoke --id "$TD" --decision release 2>&1 >/dev/null)
echo "$OUT" | grep -q "placed as a tab" || fail "release of a tab worker did not refuse: $OUT"
"$D" show --run smoke --dispatch d_tab | jq -e '.released == null' >/dev/null || fail "a tab worker was marked released"
"$D" stop --run smoke --dispatch d_tab >/dev/null 2>&1 && fail "stop of a tab worker said it stopped"
"$D" show --run smoke --dispatch d_tab | jq -e '.status != "stopped"' >/dev/null || fail "a tab worker was marked stopped"

echo "smoke ok"
