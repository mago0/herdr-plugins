#!/usr/bin/env bash
# Smoke test: every dispatch.sh command that needs no live agent, against a throwaway state dir.
# Needs a herdr on PATH whose server has delivery queues. It targets no live agent, so nothing is
# typed anywhere.
set -euo pipefail
D="$(cd "$(dirname "$0")/.." && pwd)/skill/scripts/fleet.sh"
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
# A reader never sees half a message. It waits for a writer that holds the lock, and skips a line a
# writer left half done; the next message starts a line of its own.
HB="$XDG_STATE_HOME/half.jsonl"
printf '%s\n' '{"id":"h1","ts":"t","from":"w","type":"status","subject":"a","body":""}' >"$HB"
( flock 9; printf '%s' '{"id":"h2","ts":"t","from":"w","type":"worker_done",' >>"$HB"; sleep 1
  printf '%s\n' '"subject":"done","body":""}' >>"$HB" ) 9>"$HB.lock" &
sleep 0.3
[ "$("$M" read --inbox "$HB" --types worker_done | jq -r .id)" = h2 ] || fail "a reader did not wait for a writer that holds the lock"
wait
printf '%s' '{"id":"h3","ts":"t","from":"w","type":"worker_done","subj' >>"$HB"
rc=0; "$M" wait --inbox "$HB" --types question --timeout 1 >/dev/null 2>&1 || rc=$?; [ "$rc" = 124 ] || fail "wait on a half line exited $rc"
[ "$("$M" read --inbox "$HB" --unacked | jq -r .id | paste -sd,)" = h1,h2 ] || fail "unacked with a half line"
H4=$("$M" send --inbox "$HB" --from w --type status --subject after | jq -r .sent)
[ "$("$M" read --inbox "$HB" --id "$H4" | jq -r .subject)" = after ] || fail "a message after a half line is not readable"
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

# Release closes the parent workspace Herdr opened for the worktree when it carries Herdr's mark,
# or the token that a server without the mark gets. A shim stands in for herdr.
PS="$XDG_STATE_HOME/parent-shim" REPO="$XDG_STATE_HOME/repo"; mkdir -p "$PS/bin"; git init -q "$REPO"
cat >"$PS/bin/herdr" <<EOF
#!/usr/bin/env bash
case "\$1 \$2" in
  "worktree remove") echo '{"result":{}}' ;;
  "workspace list") cat "$PS/list.json" ;;
  "agent list") echo '{"result":{"agents":[]}}' ;;
  "workspace close") echo "\$3" >>"$PS/closed" ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$PS/bin/herdr"
release_with_parent() {  # <name> <jq filter that marks the parent workspace>
  jq -n --arg root "$REPO" "{result: {workspaces: [{workspace_id: \"wp\", pane_count: 1,
    worktree: {checkout_path: \$root, repo_key: (\$root + \"/.git\"), is_linked_worktree: false}} | $2]}}" >"$PS/list.json"
  rm -f "$PS/closed"
  jq --arg d "d_p_$1" --arg n "p-$1" --arg r "$REPO" '. + [{dispatch: $d, attempt: 1, status: "working", agent: $n,
    workspace_id: "w-gone", after: [], launch: {repo: $r}}]' "$L" >"$L.new" && mv "$L.new" "$L"
  local id; id=$("$D" report --run smoke --from "p-$1" --type worker_done --outcome succeeded --subject done 2>/dev/null | jq -r .sent)
  PATH="$PS/bin:$PATH" "$D" ack --run smoke --id "$id" --decision release >/dev/null 2>&1 || fail "release p-$1"
}
release_with_parent mark '.opened_for_worktrees = true'
[ "$(cat "$PS/closed" 2>/dev/null)" = wp ] || fail "release left a parent with Herdr's mark open"
release_with_parent token '.tokens = {dispatch_parent: "1"}'
[ "$(cat "$PS/closed" 2>/dev/null)" = wp ] || fail "release left a parent with the dispatch_parent token open"
release_with_parent none '.'
[ ! -e "$PS/closed" ] || fail "release closed a parent that Herdr did not open for a worktree"
release_with_parent taken '.opened_for_worktrees = true | .pane_count = 2'
[ ! -e "$PS/closed" ] || fail "release closed a parent with a second pane"

# Settlement is safe to re-run. For a fake workspace (fk-*) the shim makes `herdr worktree remove`
# kill the command that called it (a crash between settle and release), fail as dirty, or remove.
WR="$XDG_STATE_HOME/wt-remove" WTS="$XDG_STATE_HOME/wt"
# Herdr's agent start and agent resume, as a server with them (mode new) or without (mode old) answers.
# Each call goes to launch-calls and its first prompt (stdin) to launch-prompt.
FL="$XDG_STATE_HOME/fake-launch"; echo new >"$XDG_STATE_HOME/herdr-mode"
cat >"$FL" <<EOF
#!/usr/bin/env bash
echo "\$*" >>"$XDG_STATE_HOME/launch-calls"
if [ "\$(cat "$XDG_STATE_HOME/herdr-mode")" = old ]; then
  [ "\$2" = start ] && echo "unknown option: --prompt-file" || echo "herdr agent commands:"; exit 2
fi
pane= session=
while [ \$# -gt 0 ]; do case "\$1" in --pane) pane=\$2; shift 2 ;; --agent-session) session=\$2; shift 2 ;; --) break ;; *) shift ;; esac; done
if [ -n "\$session" ]; then
  case "\$(jq -r .agent <<<"\$session")" in
    claude|codex|pi|omp) ;;
    *) echo '{"error":{"code":"agent_resume_unsupported","message":"herdr has no resume command for gemini sessions"}}'; exit 1 ;;
  esac
fi
[ "\$pane" != dispatch:check ] || { echo '{"error":{"code":"agent_pane_not_found","message":"no pane"}}'; exit 1; }
cat >"$XDG_STATE_HOME/launch-prompt"; echo '{"result":{}}'
EOF
chmod +x "$FL"
cat >"$XDG_STATE_HOME/bin/herdr" <<EOF
#!/usr/bin/env bash
if [ "\$1 \$2" = "agent get" ] && [ -f "$FA/\$3" ]; then cat "$FA/\$3"; exit 0; fi
case "\$1 \$2" in "agent resume"|"agent start") exec "$FL" "\$@" ;; esac
if [ "\$1 \$2 \$3" = "worktree remove --workspace" ] && [ "\${4#fk-}" != "\$4" ]; then
  case "\$(cat "$WR")" in
    crash) kill -KILL -- -"\$(ps -o pgid= -p \$\$ | tr -d ' ')" ;;
    dirty) echo '{"error":{"code":"dirty_worktree_requires_force","message":"dirty"}}'; exit 1 ;;
    *) rm -rf "$WTS/\$4"; echo '{"result":{}}'; exit 0 ;;
  esac
fi
exec $(printf %q "$(command -v herdr)") "\$@"
EOF
fakews() {  # <dispatch> <agent> <status> [extra json]
  local x=${4:-}; [ -n "$x" ] || x="{}"
  mkdir -p "$WTS/fk-$2"
  jq --arg d "$1" --arg a "$2" --arg s "$3" --arg wt "$WTS/fk-$2" --argjson x "$x" \
    '. + [{dispatch: $d, attempt: 1, status: $s, agent: $a, workspace_id: ("fk-" + $a), worktree: $wt, after: [], launch: {}} + $x]' \
    "$L" >"$L.new" && mv "$L.new" "$L"
}
crash() { echo crash >"$WR"; ! (PATH="$XDG_STATE_HOME/bin:$PATH" setsid -w "$D" "$@" >/dev/null 2>&1; exit $?) 2>/dev/null; }
dsp() { PATH="$XDG_STATE_HOME/bin:$PATH" "$D" "$@"; }
row() { "$D" ps --run smoke --json | jq -c --arg d "$1" '.[] | select(.dispatch == $d)'; }
acked() { "$D" read --run smoke --id "$1" >/dev/null && ! "$(dirname "$D")/mail.sh" read --inbox "$ST/runs/smoke/inbox.jsonl" --unacked | jq -e --arg i "$1" 'select(.id == $i)' >/dev/null; }

# A crash after the ledger settled and before release: ps names the re-run, which releases and acks.
fakews d_crash crasher working
CD=$("$D" report --run smoke --from crasher --type worker_done --outcome succeeded --subject done 2>/dev/null | jq -r .sent)
crash ack --run smoke --id "$CD" --decision release || fail "the crash shim did not crash ack"
"$D" show --run smoke --dispatch d_crash | jq -e --arg i "$CD" '.status == "settled" and .settled_by == $i and .released == null' >/dev/null || fail "crash state: $("$D" show --run smoke --dispatch d_crash)"
acked "$CD" && fail "the crashed ack acked the mail"
row d_crash | jq -e --arg i "$CD" '.attention == "unreleased" and (.next | test("ack --id " + $i + " --decision release"))' >/dev/null || fail "ps does not name the re-run: $(row d_crash)"
SETTLED_AT=$("$D" show --run smoke --dispatch d_crash | jq -r .settled)
dsp ack --run smoke --id "$CD" --decision retain --reason r --until u >/dev/null 2>&1 && fail "a re-run changed the decision"
echo ok >"$WR"
dsp ack --run smoke --id "$CD" --decision release 2>/dev/null | jq -e '.dispatch == "d_crash" and .decision == "release"' >/dev/null || fail "re-run of ack"
"$D" show --run smoke --dispatch d_crash | jq -e --arg s "$SETTLED_AT" '.released != null and .settled == $s' >/dev/null || fail "the re-run did not release"
[ ! -d "$WTS/fk-crasher" ] || fail "the re-run left the worktree"
acked "$CD" || fail "the re-run did not ack the mail"
row d_crash | jq -e '.attention != "unreleased"' >/dev/null || fail "ps still flags a released attempt"

# A dirty worktree: ps shows the force command until the worktree is gone, and a re-run records it.
fakews d_dirty dirtier working
DD=$("$D" report --run smoke --from dirtier --type worker_done --outcome succeeded --subject done 2>/dev/null | jq -r .sent)
echo dirty >"$WR"; dsp ack --run smoke --id "$DD" --decision release >/dev/null 2>&1 || fail "ack with a dirty worktree"
row d_dirty | jq -e '.attention == "unreleased" and .release_error == "dirty_worktree_requires_force"
  and (.next | test("--workspace fk-dirtier --force"))' >/dev/null || fail "ps does not show a dirty worktree: $(row d_dirty)"
rm -rf "$WTS/fk-dirtier"
dsp ack --run smoke --id "$DD" --decision release >/dev/null 2>&1 || fail "re-run after a forced remove"
"$D" show --run smoke --dispatch d_dirty | jq -e '.released != null and .release_error == null' >/dev/null || fail "a removed worktree was not recorded as released"

# An entry that an older version settled has no settled_by: the re-run still finds it.
LD=$("$D" report --run smoke --from legacy --type worker_done --outcome succeeded --subject done 2>/dev/null | jq -r .sent)
fakews d_legacy legacy settled '{"outcome": "succeeded", "decision": "release", "settled": "2999-01-01T00:00:00Z"}'
row d_legacy | jq -e --arg i "$LD" '.attention == "unreleased" and (.next | test("ack --id " + $i))' >/dev/null || fail "ps on an old unreleased entry: $(row d_legacy)"
echo ok >"$WR"; dsp ack --run smoke --id "$LD" --decision release 2>/dev/null | jq -e '.dispatch == "d_legacy"' >/dev/null || fail "re-run on an old entry"
"$D" show --run smoke --dispatch d_legacy | jq -e '.released != null' >/dev/null || fail "an old entry was not released"

# Stop has the same shape: a crash after marking stopped is finished by stop again.
fakews d_stop stopper working
crash stop --run smoke --dispatch d_stop || fail "the crash shim did not crash stop"
row d_stop | jq -e '.status == "stopped" and .attention == "unreleased" and (.next | test("stop --dispatch d_stop"))' >/dev/null || fail "ps on a stopped, unreleased entry: $(row d_stop)"
STOPPED_AT=$("$D" show --run smoke --dispatch d_stop | jq -r .settled)
echo ok >"$WR"; dsp stop --run smoke --dispatch d_stop >/dev/null || fail "re-run of stop"
"$D" show --run smoke --dispatch d_stop | jq -e --arg s "$STOPPED_AT" '.released != null and .settled == $s' >/dev/null || fail "re-run of stop did not release"

# A kept attempt that a later attempt took over holds nothing: the same agent started later, or an
# attempt down its supersedes chain. An abandoned later attempt takes nothing over.
krow() { dsp ps --run smoke --json | jq -c --arg d "$1" '.[] | select(.dispatch == $d)'; }
KEPT='{"status": "settled", "outcome": "succeeded", "decision": "retain", "decision_reason": "r", "release_when": "u"}'
T1='{"started": "2026-01-01T00:00:00Z"}' T2='{"started": "2026-01-02T00:00:00Z"}'
kept() { jq -c ". + {$1: $2}" <<<"$KEPT"; }
fakews d_kr1 kr settled "$(jq -c '. + {decision: "reuse"}' <<<"$KEPT" | jq -c ". + $T1")"
fakews d_kr2 kr settled "$(jq -cn "$T2 + {outcome: \"succeeded\", decision: \"release\", released: \"2026-01-02T01:00:00Z\"}")"
krow d_kr1 | jq -e '.replaced_by == "d_kr2" and .attention == "none" and .next == "replaced by d_kr2"' >/dev/null || fail "a kept attempt of an agent that started again is still kept: $(krow d_kr1)"
fakews d_kc1 kc1 settled "$(jq -c ". + $T1" <<<"$KEPT")"
fakews d_kc2 kc2 abandoned "$(jq -cn "$T2 + {supersedes: \"d_kc1\"}")"
fakews d_kc3 kc3 working "$(jq -cn "{started: \"2026-01-03T00:00:00Z\", supersedes: \"d_kc2\"}")"
krow d_kc1 | jq -e '.replaced_by == "d_kc3"' >/dev/null || fail "a kept attempt down a supersedes chain is still kept: $(krow d_kc1)"
fakews d_ka1 ka settled "$(jq -c ". + $T1" <<<"$KEPT")"
fakews d_ka2 ka abandoned "$T2"
krow d_ka1 | jq -e '.replaced_by == null and .attention == "none" and (.next | startswith("kept (retain)"))' >/dev/null || fail "an abandoned later attempt replaced a kept one: $(krow d_ka1)"
TK=$(dsp ps --run smoke --tree --json)
jq -e '[.rows[].dispatch] | index("d_kr1") == null and index("d_ka1") != null' <<<"$TK" >/dev/null || fail "ps --tree shows a replaced kept attempt"
dsp ps --run smoke --tree --all --json | jq -e '[.rows[].dispatch] | index("d_kr1") != null' >/dev/null || fail "ps --tree --all hides a replaced kept attempt"

# A kept attempt whose worktree is gone and whose agent does not run on its pane holds nothing.
# release records it; on a worktree that is still there it removes it.
fakews d_kg kg settled "$(kept pane_id '"kg:1"')"; rm -rf "$WTS/fk-kg"
krow d_kg | jq -e '.attention == "worktree gone" and (.next | test("release --dispatch d_kg$"))' >/dev/null || fail "a kept attempt with no worktree: $(krow d_kg)"
fakews d_kl kl settled "$(kept pane_id '"kl:1"')"; rm -rf "$WTS/fk-kl"
jq -cn '{result: {agent: {name: "kl", pane_id: "kl:1"}}}' >"$FA/kl"
krow d_kl | jq -e '.attention == "none" and (.next | startswith("kept (retain)"))' >/dev/null || fail "a kept agent that runs lost its row: $(krow d_kl)"
jq -cn '{result: {agent: {name: "kl", pane_id: "kl:2"}}}' >"$FA/kl"
krow d_kl | jq -e '.attention == "worktree gone"' >/dev/null || fail "an agent of the same name on another pane kept a row: $(krow d_kl)"
rm -f "$FA/kl"
fakews d_kw kw settled "$(kept pane_id '"kw:1"')"
krow d_kw | jq -e '.attention == "none" and (.next | startswith("kept (retain)"))' >/dev/null || fail "a kept worktree with no agent lost its row: $(krow d_kw)"
dsp release --run smoke --dispatch d_kg | jq -e '.dispatch == "d_kg" and .released != null' >/dev/null || fail "release of a gone worktree"
krow d_kg | jq -e '.attention == "none" and (.next | test("wait"))' >/dev/null || fail "a released kept attempt: $(krow d_kg)"
REL=$("$D" show --run smoke --dispatch d_kg | jq -r .released)
dsp release --run smoke --dispatch d_kg | jq -e --arg r "$REL" '.released == $r' >/dev/null || fail "a re-run of release changed it"
dsp release --run smoke --dispatch d_kr1 >/dev/null 2>&1 && fail "release of a replaced attempt"
dsp release --run smoke --dispatch d_kr2 >/dev/null 2>&1 && fail "release of an attempt that is not kept"
[ -d "$WTS/fk-kr" ] || fail "a refused release removed a worktree"
jq -cn '{result: {agent: {name: "kw", pane_id: "kw:2", workspace_id: "fk-kw"}}}' >"$FA/kw"
dsp release --run smoke --dispatch d_kw >/dev/null 2>&1 && fail "release of a worktree whose agent name runs on another pane"
rm -f "$FA/kw"; [ -d "$WTS/fk-kw" ] || fail "a refused release removed a worktree"
echo dirty >"$WR"; dsp release --run smoke --dispatch d_kw >/dev/null 2>&1 || fail "release with a dirty worktree"
krow d_kw | jq -e '.attention == "unreleased" and (.next | test("--force; then .*release --dispatch d_kw$"))' >/dev/null || fail "a failed release of a kept attempt: $(krow d_kw)"
echo ok >"$WR"; dsp release --run smoke --dispatch d_kw | jq -e '.released != null and .release_error == null' >/dev/null || fail "release of a kept worktree"
[ ! -d "$WTS/fk-kw" ] || fail "release left the worktree"

# A Herdr shim that keeps each wake line in delivered and each supervise call in supervised, and
# answers pane get for the panes in alive and agent get for each agents/<name> (its pane id).
WK="$XDG_STATE_HOME/wake-shim"; mkdir -p "$WK/bin" "$WK/agents"; : >"$WK/alive"
cat >"$WK/bin/herdr" <<EOF
#!/usr/bin/env bash
case "\$1 \$2" in
  "agent deliver") echo "\$3 \$4" >>"$WK/delivered"; echo '{"result":{"delivery":{}}}' ;;
  "pane get") grep -qxF -- "\$3" "$WK/alive" || { echo '{"error":{"code":"pane_not_found"}}'; exit 1; }
    jq -cn --arg p "\$3" '{result: {pane: {pane_id: \$p, tokens: {label: "x"}}}}' ;;
  "pane supervise") echo "\$*" >>"$WK/supervised"; echo '{"result":{}}' ;;
  "agent get") [ -f "$WK/agents/\$3" ] || exit 1
    jq -cn --arg n "\$3" --arg p "\$(cat "$WK/agents/\$3")" '{result: {agent: {name: \$n, pane_id: \$p, agent_status: "working"}}}' ;;
  *) echo '{"result":{}}' ;;
esac
EOF
chmod +x "$WK/bin/herdr"
wk() { PATH="$WK/bin:$PATH" "$D" "$@"; }

# A report is sent once per key. A repeat returns the first id and appends nothing. While the first
# is un-acked, the repeat wakes the supervisor again for it: the first wake can be lost.
INB="$ST/runs/smoke/inbox.jsonl"; echo sup:1 >"$ST/runs/smoke/supervisor"; : >"$WK/delivered"
K1=$(wk report --run smoke --from keyed --type worker_done --key done --outcome succeeded --subject done 2>/dev/null | jq -r .sent)
N=$(wc -l <"$INB")
OUT=$(wk report --run smoke --from keyed --type worker_done --key done --outcome succeeded --subject "done, again" 2>&1)
jq -e --arg i "$K1" '.sent == $i and .duplicate == true and .acked == false' <<<"$(head -1 <<<"$OUT")" >/dev/null || fail "a repeated key was sent again: $OUT"
[ "$(wc -l <"$INB")" = "$N" ] || fail "a repeat was appended"
[ "$(grep -c "^sup:1 MAIL|$K1|worker_done|keyed|done - " "$WK/delivered")" = 2 ] || fail "an un-acked repeat did not wake the supervisor again for the first message: $(cat "$WK/delivered")"
"$D" ack --run smoke --id "$K1" --decision release >/dev/null 2>&1
: >"$WK/delivered"
wk report --run smoke --from keyed --type worker_done --key done --outcome succeeded --subject done 2>/dev/null | jq -e --arg i "$K1" '.sent == $i and .acked == true' >/dev/null || fail "a key repeated after the ack was sent again"
[ ! -s "$WK/delivered" ] || fail "an acked repeat woke the supervisor: $(cat "$WK/delivered")"
rm "$ST/runs/smoke/supervisor"

# A run whose supervisor pane is gone moves to the pane that binds it, with its live workers. ps
# names a gone pane. A run whose supervisor pane is open moves only with --take-over.
echo sup:old >"$WK/alive"; : >"$WK/supervised"
HERDR_PANE_ID=sup:old wk run --name takeover >/dev/null || fail "run from a pane"
TL="$ST/runs/takeover/ledger.json"
jq '. + [{dispatch: "d_t1", attempt: 1, status: "working", agent: "tw1", pane_id: "tp:1", started: "2026-01-01T00:00:00Z", after: [], launch: {}},
         {dispatch: "d_t2", attempt: 1, status: "settled", decision: "release", agent: "tw2", pane_id: "tp:2", after: [], launch: {}}]' "$TL" >"$TL.new" && mv "$TL.new" "$TL"
echo tp:1 >"$WK/agents/tw1"; echo tp:2 >"$WK/agents/tw2"
HERDR_PANE_ID=sup:new wk run --name takeover >/dev/null 2>&1 && fail "a run moved from a supervisor pane that is still open"
[ "$(cat "$ST/runs/takeover/supervisor")" = sup:old ] || fail "a refused run changed the supervisor"
wk ps --run takeover 2>&1 >/dev/null | grep -q "is gone" && fail "ps warned about an open supervisor pane"
: >"$WK/alive"
wk ps --run takeover 2>&1 >/dev/null | grep -q "supervisor pane sup:old of run 'takeover' is gone.*run --name takeover$" || fail "ps does not name a gone supervisor pane"
echo sup:new >"$WK/alive"
HERDR_PANE_ID=sup:new wk run --name takeover 2>/dev/null | jq -e '.moved_from == "sup:old" and .linked == 1' >/dev/null || fail "run did not take over from a gone pane"
[ "$(cat "$ST/runs/takeover/supervisor")" = sup:new ] || fail "the supervisor was not moved"
grep -qx "pane supervise tp:1 --by sup:new --report blocked,exited,stalled" "$WK/supervised" || fail "a live worker was not linked to the new supervisor: $(cat "$WK/supervised")"
grep -q "tp:2" "$WK/supervised" && fail "a released worker was linked"
wk ps --run takeover 2>&1 >/dev/null | grep -q "is gone" && fail "ps still warns after the take-over"
echo sup:third >>"$WK/alive"
HERDR_PANE_ID=sup:third wk run --name takeover --take-over 2>/dev/null | jq -e '.moved_from == "sup:new"' >/dev/null || fail "--take-over did not move the run"
HERDR_PANE_ID=sup:third wk run --name takeover 2>/dev/null | jq -e '.moved_from == null' >/dev/null || fail "a re-bind from the same pane moved the run"
"$D" report --run smoke --from keyed --type worker_done --key done-msg_1 --outcome succeeded --subject done 2>/dev/null | jq -e --arg i "$K1" '.sent != $i and .duplicate == null' >/dev/null || fail "a new key was not sent"
"$D" report --run smoke --from other --type status --key done --subject s 2>/dev/null | jq -e --arg i "$K1" '.sent != $i' >/dev/null || fail "a key of another agent counted"
# A later attempt of the agent may use a key again once the first message with it is acked.
"$D" report --run smoke --from keyed2 --type worker_done --key done --outcome succeeded --subject done >/dev/null 2>&1
jq '. + [{dispatch: "d_k2", attempt: 2, status: "working", agent: "keyed2", created: "2999-01-01T00:00:00Z", after: [], launch: {}}]' "$L" >"$L.new" && mv "$L.new" "$L"
K2=$("$D" report --run smoke --from keyed2 --type worker_done --key done --outcome succeeded --subject done 2>/dev/null | jq -r .sent)
"$D" report --run smoke --from keyed2 --type worker_done --key done --outcome succeeded --subject done 2>/dev/null | jq -e --arg i "$K2" '.sent == $i' >/dev/null || fail "an un-acked key from an earlier attempt was sent again"
"$D" ack --run smoke --id "$K2" --decision release >/dev/null 2>&1
"$D" report --run smoke --from keyed2 --type worker_done --key done --outcome succeeded --subject done 2>/dev/null | jq -e --arg i "$K2" '.sent != $i' >/dev/null || fail "an acked key from an earlier attempt blocked this attempt's report"

# With no key, the same content is a repeat while the first is un-acked, and new after its ack.
U1=$("$D" report --run smoke --from nokey --type question --subject "go on?" | jq -r .sent)
"$D" report --run smoke --from nokey --type question --subject "go on?" | jq -e --arg i "$U1" '.sent == $i and .duplicate' >/dev/null || fail "an un-acked repeat with no key was sent again"
"$D" report --run smoke --from nokey --type question --subject "go on, now?" | jq -e --arg i "$U1" '.sent != $i' >/dev/null || fail "other content counted as a repeat"
"$D" ack --run smoke --id "$U1" >/dev/null
"$D" report --run smoke --from nokey --type question --subject "go on?" | jq -e --arg i "$U1" '.sent != $i' >/dev/null || fail "a question asked again after its answer was dropped"
# Messages from before keys existed have no key and still read.
echo '{"id":"msg_old","ts":"2026-01-01T00:00:00Z","from":"keyed","type":"status","subject":"old","body":""}' >>"$INB"
"$D" report --run smoke --from keyed --type status --key old --subject old | jq -e '.sent != "msg_old"' >/dev/null || fail "a message with no key matched a key"

# A task runs at most 5 attempts, counted through the supersedes chain, unless forced.
jq '. + [range(1; 6) as $i | {dispatch: "d_r\($i)", attempt: $i, supersedes: (if $i == 1 then null else "d_r\($i - 1)" end),
         status: "failed_start", agent: null, after: [], launch: {}}]' "$L" >"$L.new" && mv "$L.new" "$L"
OUT=$("$D" retry --run smoke --dispatch d_r5 2>&1) && fail "a sixth attempt was started"
grep -q "has run 5 attempts (d_r1 d_r2 d_r3 d_r4 d_r5); the limit is 5. Pass --force" <<<"$OUT" || fail "cap message: $OUT"
"$D" retry --run smoke --dispatch d_r2 >/dev/null 2>&1 && fail "a retry of an earlier attempt escaped the cap"
"$D" retry --run smoke --dispatch d_r5 --force >/dev/null 2>&1 || true
"$D" list --run smoke | jq -e '[.[] | select(.supersedes == "d_r5")] | length == 1' >/dev/null || fail "--force did not start an attempt"
jq '. + [{dispatch: "d_q1", attempt: 1, status: "failed_start", agent: null, after: [], launch: {}}]' "$L" >"$L.new" && mv "$L.new" "$L"
DISPATCH_MAX_ATTEMPTS=1 "$D" retry --run smoke --dispatch d_q1 >/dev/null 2>&1 && fail "the limit is not read from DISPATCH_MAX_ATTEMPTS"
"$D" retry --run smoke --dispatch d_q1 >/dev/null 2>&1 || true
"$D" list --run smoke | jq -e '[.[] | select(.supersedes == "d_q1")] | length == 1' >/dev/null || fail "an entry with no supersedes field was not retried"

# Resume continues an attempt's own session in its worktree, with Herdr's resume command for the
# session's kind. --dry-run asks Herdr whether it takes the session and opens nothing.
SID=11111111-2222-3333-4444-555555555555
resumable() {  # <dispatch> <agent> <session agent> <session value> [extra json]
  local x=${5:-}; [ -n "$x" ] || x="{}"
  mkdir -p "$WTS/$2"
  jq --arg d "$1" --arg a "$2" --arg k "$3" --arg v "$4" --arg wt "$WTS/$2" --argjson x "$x" \
    '. + [{dispatch: $d, attempt: 1, supersedes: null, status: "working", agent: $a, pane_id: "fk:none", worktree: $wt,
           prompt_file: "/prompts/first.md", after: [], session: (if $v == "" then null else {agent: $k, kind: "id", value: $v} end),
           launch: {task: "/t.md", kind: "", agent_args: ["--model", "m"]}} + $x]' "$L" >"$L.new" && mv "$L.new" "$L"
}
plan() { dsp resume --run smoke --dispatch "$1" --dry-run; }
resumable d_rc rc claude "$SID"
resumable d_rx rx codex "$SID"
resumable d_rp rp pi "$PT"
resumable d_ro ro omp "$PT"
: >"$XDG_STATE_HOME/launch-calls"
plan d_rc | jq -e --arg s "$SID" --arg wt "$WTS/rc" '.agent_args == ["--model", "m"] and .session.value == $s and .kind == "claude"
  and .agent == "rc" and .worktree == $wt and .pane == "fk:none" and .task == "/prompts/first.md" and .attempts == 1' >/dev/null || fail "claude resume plan: $(plan d_rc)"
for k in rx:codex rp:pi ro:omp; do
  plan "d_${k%%:*}" | jq -e --arg k "${k#*:}" '.kind == $k and .session.agent == $k and .agent_args == ["--model", "m"]' >/dev/null || fail "${k#*:} resume plan: $(plan "d_${k%%:*}")"
done
grep -q -- "--pane dispatch:check" "$XDG_STATE_HOME/launch-calls" || fail "--dry-run did not ask Herdr"
grep -v -- "--pane dispatch:check" "$XDG_STATE_HOME/launch-calls" | grep -q . && fail "--dry-run started an agent"
"$D" list --run smoke | jq -e '[.[] | select(.resume != null)] | length == 0' >/dev/null || fail "--dry-run wrote the ledger"
"$D" ps --run smoke --json | jq -e '.[] | select(.dispatch == "d_ro") | .attention == "exited" and (.next | test("resume --dispatch d_ro"))' >/dev/null || fail "ps does not offer resume"

# Refusals, each pointing at retry where retry is the way on.
refuses() {  # <dispatch> <pattern>
  local out n; n=$("$D" list --run smoke | jq --arg d "$1" '[.[] | select(.supersedes == $d)] | length')
  out=$(dsp resume --run smoke --dispatch "$1" 2>&1) && fail "resume of $1 was not refused"
  grep -q -- "$2" <<<"$out" || fail "resume of $1: $out"
  "$D" list --run smoke | jq -e --arg d "$1" --argjson n "$n" '[.[] | select(.supersedes == $d)] | length == $n' >/dev/null || fail "a refused resume of $1 wrote an attempt"
}
resumable d_rm rm gemini "$SID"
refuses d_rm "Herdr cannot resume this session: herdr has no resume command for gemini sessions; start the task again from its prompt instead: .*retry --dispatch d_rm"
resumable d_rn rn claude ""
refuses d_rn "has no recorded session; .*retry --dispatch d_rn"
resumable d_rg rg claude "$SID"; rmdir "$WTS/rg"
refuses d_rg "worktree of d_rg is gone .*retry --dispatch d_rg"
resumable d_rl rl claude "$SID"; fake fk:rl rl claude id "$SID"; cp "$FA/fk:rl" "$FA/rl"
refuses d_rl "agent rl is still running"
resumable d_r6 r6 claude "$SID" '{"supersedes": "d_r5"}'
refuses d_r6 "has run [0-9]* attempts .*the limit is 5. Pass --force"
dsp resume --run smoke --dispatch d_r6 --force --dry-run | jq -e '.attempts > 5' >/dev/null || fail "--force did not lift the cap on resume"
resumable d_rs rs claude "$SID"
jq '. + [{dispatch: "d_rs2", attempt: 2, supersedes: "d_rs", status: "working", agent: "rs", after: [], launch: {}}]' "$L" >"$L.new" && mv "$L.new" "$L"
refuses d_rs "already has a newer attempt d_rs2"
# A Herdr without agent resume: the refusal names what Herdr needs and does not offer retry.
echo old >"$XDG_STATE_HOME/herdr-mode"
refuses d_rx "Dispatch needs a Herdr build with herdr agent resume"
dsp resume --run smoke --dispatch d_rx 2>&1 | grep -q "retry --dispatch" && fail "an old Herdr refusal offered retry"
echo new >"$XDG_STATE_HOME/herdr-mode"

# A real resume writes a linked attempt and the notice, then the launcher stops before Herdr
# (no HERDR_ENV), so the attempt is failed_start.
HERDR_ENV= dsp resume --run smoke --dispatch d_rc >/dev/null 2>&1 || true
"$D" list --run smoke | jq -e --arg s "$SID" '(.[] | select(.dispatch == "d_rc") | .status == "abandoned")
  and ([.[] | select(.supersedes == "d_rc")] | length == 1 and (.[0] | .attempt == 2 and .resume.from == "d_rc"
       and .resume.session.value == $s and .launch.kind == "claude" and .status == "failed_start"))' >/dev/null || fail "resume ledger: $("$D" list --run smoke | jq -c '.[] | select(.supersedes == "d_rc")')"
NOTE=$("$D" list --run smoke | jq -r '.[] | select(.supersedes == "d_rc") | .resume.notice')
grep -q 'git status' "$NOTE" && grep -qF "$WTS/rc" "$NOTE" && grep -q "/prompts/first.md" "$NOTE" || fail "resume notice: $(cat "$NOTE")"
rc=0; HERDR_ENV= "$(dirname "$D")/herdr-fleet.sh" --resume "$WTS/rc" --session "{\"agent\":\"claude\",\"kind\":\"id\",\"value\":\"$SID\"}" \
  --name rc --prompt-file "$NOTE" --run-dir "$ST/runs/smoke" 2>/dev/null || rc=$?
[ "$rc" = 1 ] || fail "the launcher did not take --resume in place of --repo and --branch (exit $rc)"

# The launcher against a Herdr shim: start and resume hand Herdr the first prompt on stdin and type
# nothing themselves, and a Herdr without agent resume is refused before a worktree opens.
LS="$XDG_STATE_HOME/launch-shim" LR="$XDG_STATE_HOME/launch-repo"; mkdir -p "$LS/bin"
git init -q "$LR"; git -C "$LR" -c user.name=t -c user.email=t@t commit -q --allow-empty -m init
git -C "$LR" worktree add -q "$LR/_worktrees/lw" -b lw
cat >"$LS/bin/herdr" <<EOF
#!/usr/bin/env bash
case "\$1 \$2" in
  "agent resume"|"agent start") exec "$FL" "\$@" ;;
  "status "*) echo "server: running" ;;
  "agent list") echo '{"result":{"agents":[]}}' ;;
  "agent get") echo '{"result":{"agent":{"name":"lw","pane_id":"lp:1","agent_status":"idle"}}}' ;;
  "workspace list") echo '{"result":{"workspaces":[]}}' ;;
  "worktree open"|"worktree create") echo "\$*" >>"$LS/opened"; echo '{"result":{"root_pane":{"pane_id":"lp:1"},"workspace":{"workspace_id":"lw"}}}' ;;
  "pane run") echo "\$*" >>"$LS/typed"; echo '{"result":{}}' ;;
  *) echo '{"result":{}}' ;;
esac
EOF
chmod +x "$LS/bin/herdr"
T=$(mktemp); echo "the task" >"$T"
launch() { HERDR_ENV=1 PATH="$LS/bin:$PATH" "$(dirname "$D")/herdr-fleet.sh" "$@"; }
: >"$XDG_STATE_HOME/launch-calls"
OUT=$(launch --repo "$LR" --branch lw2 --name lw --kind claude --prompt-file "$T" --run-dir "$ST/runs/smoke" -- --model m 2>&1) || fail "start: $OUT"
jq -e '.status == "idle" and .kind == "claude"' <<<"$OUT" >/dev/null || fail "start receipt: $OUT"
grep -qx "agent start lw --kind claude --pane lp:1 --timeout 60000 --prompt-file - -- --model m" "$XDG_STATE_HOME/launch-calls" || fail "start call: $(cat "$XDG_STATE_HOME/launch-calls")"
TF=$(jq -r .task_file <<<"$OUT")
[ "$(cat "$XDG_STATE_HOME/launch-prompt")" = "Your task is in the file $TF - read it now and carry it out in full." ] || fail "start prompt: $(cat "$XDG_STATE_HOME/launch-prompt")"
grep -q "the task" "$TF" && grep -q "Reporting to your supervisor" "$TF" || fail "task file: $(cat "$TF")"
[ ! -e "$LS/typed" ] || fail "the launcher typed into the pane: $(cat "$LS/typed")"
S="{\"agent\":\"omp\",\"kind\":\"path\",\"value\":\"$PT\"}"
OUT=$(launch --resume "$LR/_worktrees/lw" --session "$S" --name lw --prompt-file "$T" --run-dir "$ST/runs/smoke" -- --model m 2>&1) || fail "resume: $OUT"
jq -e '.status == "idle" and .kind == "omp"' <<<"$OUT" >/dev/null || fail "resume receipt: $OUT"
grep -qxF "agent resume lw --agent-session $S --pane lp:1 --timeout 60000 --prompt-file - -- --model m" "$XDG_STATE_HOME/launch-calls" || fail "resume call: $(cat "$XDG_STATE_HOME/launch-calls")"
grep -q "^You were interrupted and this session is resumed. Read the file $(jq -r .task_file <<<"$OUT") now" "$XDG_STATE_HOME/launch-prompt" || fail "resume prompt: $(cat "$XDG_STATE_HOME/launch-prompt")"
echo old >"$XDG_STATE_HOME/herdr-mode"; rm -f "$LS/opened"
OUT=$(launch --repo "$LR" --branch lw3 --name lw --kind claude --prompt-file "$T" --run-dir "$ST/runs/smoke" 2>&1) && fail "start on an old Herdr: $OUT"
grep -q "Dispatch needs a Herdr build with herdr agent resume" <<<"$OUT" || fail "old Herdr message: $OUT"
[ ! -e "$LS/opened" ] || fail "a worktree opened on an old Herdr"
echo new >"$XDG_STATE_HOME/herdr-mode"; rm -f "$T"

# Nested runs. A Herdr shim answers agent get from tree-shim/agents/<name or pane> and pane get for
# the panes in alive. t-proj supervises issue-a, whose pane binds t-a; t-a supervises review-a,
# whose pane bound t-a-review before parents were recorded.
TS="$XDG_STATE_HOME/tree-shim"; mkdir -p "$TS/bin" "$TS/agents"; printf '%s\n' tsup:0 ta:1 ra:1 >"$TS/alive"
cat >"$TS/bin/herdr" <<EOF
#!/usr/bin/env bash
case "\$1 \$2" in
  "agent get") [ -f "$TS/agents/\$3" ] || exit 1; jq -c '{result: {agent: .}}' "$TS/agents/\$3" ;;
  "pane get") grep -qxF -- "\$3" "$TS/alive" || exit 1; jq -cn --arg p "\$3" '{result: {pane: {pane_id: \$p, tokens: {label: "x"}}}}' ;;
  *) echo '{"result":{}}' ;;
esac
EOF
chmod +x "$TS/bin/herdr"
ts() { PATH="$TS/bin:$PATH" "$D" "$@"; }
agent() {  # <name> <pane> <supervisor pane>
  jq -cn --arg n "$1" --arg p "$2" --arg s "$3" '{name: $n, pane_id: $p, agent_status: "working", supervisor_pane_id: $s}' >"$TS/agents/$1"
  cp "$TS/agents/$1" "$TS/agents/$2"
}
attempts() {  # <run> <jq expression for an array of attempts>
  local f="$ST/runs/$1/ledger.json"
  jq --argjson x "$(jq -nc "$2")" '. + [$x[] | {attempt: 1, after: [], launch: {}} + .]' "$f" >"$f.new" && mv "$f.new" "$f"
}
legacy() {  # <run> <supervisor pane> <bound at>: a run bound before parents were recorded
  mkdir -p "$ST/runs/$1"; echo '[]' >"$ST/runs/$1/ledger.json"; : >"$ST/runs/$1/inbox.jsonl"
  echo "$2" >"$ST/runs/$1/supervisor"; touch -d "$3" "$ST/runs/$1/supervisor"
}
tree() { ts ps --run "$1" --tree --json "${@:2}"; }
HERDR_PANE_ID=tsup:0 ts run --name t-proj | jq -e '.parent == null' >/dev/null || fail "a run bound from no attempt's pane got a parent"
jq -e '.dispatch == null and .pane == "tsup:0"' "$ST/runs/t-proj/parent" >/dev/null || fail "a top-level bind was not recorded"
attempts t-proj '[{dispatch: "d_ta", agent: "issue-a", pane_id: "ta:1", status: "working", started: "2026-01-01T00:00:00Z"},
  {dispatch: "d_tb", agent: "issue-b", pane_id: "tb:1", status: "settled", outcome: "succeeded", decision: "release",
   started: "2026-01-01T00:00:00Z", settled: "2026-01-03T00:00:00Z", released: "2026-01-03T00:00:00Z"},
  {dispatch: "d_tx", agent: "issue-x", pane_id: "tx:1", status: "settled", outcome: "succeeded", decision: "release",
   started: "2026-01-01T00:00:00Z", settled: "2026-01-02T00:00:00Z", released: "2026-01-02T00:00:00Z"}]'
agent issue-a ta:1 tsup:0
HERDR_PANE_ID=ta:1 ts run --name t-a | jq -e '.parent == {run: "t-proj", dispatch: "d_ta"}' >/dev/null || fail "run from a worker's pane did not record its attempt"
attempts t-a '[{dispatch: "d_ra", agent: "review-a", pane_id: "ra:1", status: "working", started: "2026-01-05T00:00:00Z"},
  {dispatch: "d_la", agent: "lint-a", pane_id: "la:1", status: "settled", outcome: "succeeded", decision: "release",
   started: "2026-01-05T00:00:00Z", settled: "2026-01-05T01:00:00Z", released: "2026-01-05T01:00:00Z"}]'
agent review-a ra:1 ta:1
legacy t-a-review ra:1 2026-01-06T00:00:00Z
attempts t-a-review '[{dispatch: "d_lt", agent: "loadtest", pane_id: "lt:1", status: "working", started: "2026-01-06T00:00:00Z"}]'
agent loadtest lt:1 ra:1
Q=$(ts report --run t-a-review --from loadtest --type question --subject "raise the rate?" 2>/dev/null | jq -r .sent)
# issue-b bound t-b-old while it ran; issue-x was released before its pane bound t-later.
legacy t-b-old tb:1 2026-01-02T00:00:00Z
attempts t-b-old '[{dispatch: "d_bo", agent: "old", pane_id: "bo:1", status: "settled", outcome: "succeeded", decision: "release",
  started: "2026-01-02T00:00:00Z", settled: "2026-01-02T01:00:00Z", released: "2026-01-02T01:00:00Z"}]'
legacy t-later tx:1 2026-02-01T00:00:00Z
TJ=$(tree t-proj)
jq -e '.run == "t-proj" and .hidden == 2 and ([.rows[].dispatch] == ["d_ta"])' <<<"$TJ" >/dev/null || fail "done rows were not hidden: $TJ"
jq -e '.rows[0].runs[0] | .run == "t-a" and .parent.how == "recorded" and .hidden == 1 and ([.rows[].dispatch] == ["d_ra"])' <<<"$TJ" >/dev/null || fail "level 2: $TJ"
jq -e --arg q "$Q" '.rows[0].runs[0].rows[0].runs[0] | .run == "t-a-review" and .parent == {run: "t-a", dispatch: "d_ra", how: "inferred"}
  and (.rows[0] | .attention == "question" and (.next | test("reply --run t-a-review --id " + $q)))' <<<"$TJ" >/dev/null || fail "level 3: $TJ"
TA=$(tree t-proj --all)
jq -e '[.rows[].dispatch] == ["d_ta", "d_tb", "d_tx"] and .hidden == 0' <<<"$TA" >/dev/null || fail "--all hid a row: $TA"
jq -e '(.rows[] | select(.dispatch == "d_tb") | .runs[0].run) == "t-b-old"' <<<"$TA" >/dev/null || fail "a run bound while its attempt ran was not linked: $TA"
jq -e '[.. | .run? // empty] | index("t-later") == null' <<<"$TA" >/dev/null || fail "a run was linked to an attempt released before it was bound: $TA"
ts ps --run t-proj --tree | grep -q '^      review-a ' || fail "the table does not indent by depth: $(ts ps --run t-proj --tree)"
ts ps --run t-proj --tree | grep -q '^run t-proj .*2 done hidden' || fail "the table does not count hidden rows"
ts ps --run t-a --tree --json | jq -e '.parent.run == "t-proj" and .rows[0].runs[0].run == "t-a-review"' >/dev/null || fail "a tree from a middle run"
[ "$(ts ps --run t-proj | head -1 | awk '{print $1}')" = DISPATCH ] || fail "ps without --tree changed"
ts ps --run t-proj --all >/dev/null 2>&1 && fail "--all without --tree"
# Two runs held the same pane: with no Herdr link it is ambiguous, and Herdr's link picks one.
HERDR_PANE_ID=x1:0 ts run --name t-p1 >/dev/null; HERDR_PANE_ID=x2:0 ts run --name t-p2 >/dev/null
attempts t-p1 '[{dispatch: "d_p1", agent: "am1", pane_id: "am:1", status: "working", started: "2026-01-01T00:00:00Z"}]'
attempts t-p2 '[{dispatch: "d_p2", agent: "am2", pane_id: "am:1", status: "settled", decision: "retain", started: "2026-01-01T00:00:00Z"}]'
legacy t-amb am:1 2026-01-10T00:00:00Z
tree t-p1 | jq -e '.rows[0].runs == []' >/dev/null && tree t-p2 | jq -e '.rows[0].runs == []' >/dev/null || fail "an ambiguous pane was linked"
agent am2 am:1 x2:0
tree t-p2 | jq -e '.rows[0].runs[0].run == "t-amb"' >/dev/null || fail "Herdr's link did not settle an ambiguous pane"
tree t-p1 | jq -e '.rows[0].runs == []' >/dev/null || fail "a run was linked against Herdr's link"
# A run moved to a pane that is no attempt's is no longer a child.
echo tsup:9 >>"$TS/alive"
HERDR_PANE_ID=tsup:9 ts run --name t-a --take-over 2>/dev/null | jq -e '.parent == null' >/dev/null || fail "a moved run kept its parent"
tree t-proj | jq -e '.rows[0].runs == []' >/dev/null || fail "a run moved off its parent's pane is still under it"

echo "smoke ok"
