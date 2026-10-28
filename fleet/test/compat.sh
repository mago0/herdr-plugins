#!/usr/bin/env bash
# Compat test: a full start, report, reply and ack cycle through each old dispatch path and the new
# fleet paths, and across the state layouts of the switch from agent-dispatch to agent-fleet.
# A Herdr shim stands in for the server, so it needs no live Herdr and types nothing anywhere.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
export XDG_STATE_HOME="$TMP/state" HERDR_ENV=1
unset HERDR_PANE_ID FLEET_RUN DISPATCH_RUN
fail() { echo "FAIL: $*" >&2; exit 1; }
NEW="$ROOT/fleet/skill/scripts"

# The Herdr shim. Each agent start records the agent's pane in agents/<name>, each delivery goes
# to delivered as "<target> <line>", and pane get knows only the supervisor pane sup:1.
SH="$TMP/shim"; mkdir -p "$SH/bin" "$SH/agents"; echo 0 >"$SH/n"
cat >"$SH/bin/herdr" <<EOF
#!/usr/bin/env bash
case "\$1 \$2" in
  "status "*) echo "server: running" ;;
  "agent deliveries") echo '{"result":{"deliveries":[]}}' ;;
  "agent resume") echo '{"error":{"code":"agent_pane_not_found","message":"no pane"}}'; exit 1 ;;
  "agent list") echo '{"result":{"agents":[]}}' ;;
  "workspace list") echo '{"result":{"workspaces":[]}}' ;;
  "worktree create") n=\$((\$(cat "$SH/n") + 1)); echo "\$n" >"$SH/n"
    echo "{\"result\":{\"root_pane\":{\"pane_id\":\"p\$n:1\"},\"workspace\":{\"workspace_id\":\"ws\$n\"}}}" ;;
  "agent start") cat >"$SH/prompt"; p=; a=\$3; shift 3
    while [ \$# -gt 0 ]; do case "\$1" in --pane) p=\$2; shift 2 ;; *) shift ;; esac; done
    echo "\$p" >"$SH/agents/\$a"; echo '{"result":{}}' ;;
  "agent get") [ -f "$SH/agents/\$3" ] || { echo '{"error":{"code":"agent_not_found"}}'; exit 1; }
    p=\$(cat "$SH/agents/\$3")
    echo "{\"result\":{\"agent\":{\"name\":\"\$3\",\"pane_id\":\"\$p\",\"workspace_id\":\"ws\${p%%:*}\",\"agent_status\":\"working\"}}}" ;;
  "pane get") [ "\$3" = sup:1 ] || { echo '{"error":{"code":"pane_not_found"}}'; exit 1; }
    echo '{"result":{"pane":{"pane_id":"sup:1","tokens":{}}}}' ;;
  "agent deliver") echo "\$3 \$4" >>"$SH/delivered"; echo '{"result":{"delivery":{}}}' ;;
  *) echo '{"result":{}}' ;;
esac
EOF
chmod +x "$SH/bin/herdr"
export PATH="$SH/bin:$PATH"

REPO="$TMP/repo"; git init -q "$REPO"; git -C "$REPO" -c user.name=t -c user.email=t@t commit -q --allow-empty -m init
TASK="$TMP/task.md"; echo "the task" >"$TASK"

# begin <script> <run> <agent>: bind a run from sup:1 and start one worker. Sets TF (its task file),
# REP (the fleet.sh its reporting rules name) and WMAIL (the mail.sh they name).
begin() {
  local out
  HERDR_PANE_ID=sup:1 "$1" run --name "$2" >/dev/null || fail "run through $1"
  out=$(HERDR_PANE_ID=sup:1 "$1" start --run "$2" --repo "$REPO" --branch "$3" --name "$3" --kind claude --task "$TASK" 2>&1) \
    || fail "start through $1: $out"
  TF=$(jq -r .task_file <<<"$out")
  grep -q "the task" "$TF" && grep -q "You are fleet worker \`$3\`" "$TF" || fail "task file of $3: $(cat "$TF")"
  REP=$(grep -m1 " report --run $2 --from $3 " "$TF" | awk '{print $1}')
  WMAIL=$(grep -o '`[^` ]*/mail.sh read --inbox [^`]*`' "$TF" | head -1 | tr -d '`' | awk '{print $1}')
  WINBOX=$(grep -o '`[^` ]*/mail.sh read --inbox [^`]*`' "$TF" | head -1 | tr -d '`' | awk '{print $4}')
}

# finish <script> <run> <agent> <scripts dir the supervisor's commands name>: the worker asks, the
# supervisor replies, the worker reports worker_done and the supervisor acks it with release, each
# through the paths the other side printed.
finish() {
  local q line cmd id
  : >"$SH/delivered"
  q=$("$REP" report --run "$2" --from "$3" --type question --key q --subject "go on?" 2>/dev/null | jq -r .sent)
  [ -n "$q" ] && [ "$q" != null ] || fail "question from $3 through $REP"
  HERDR_PANE_ID=sup:1 "$1" reply --run "$2" --id "$q" --body yes >/dev/null 2>&1 || fail "reply through $1"
  "$WMAIL" read --inbox "$WINBOX" --unacked | jq -e '.type == "reply" and .body == "yes"' >/dev/null || fail "the reply to $3 is not in $WINBOX"
  line=$(grep -m1 "^$3 Your supervisor answered your question. Read it: " "$SH/delivered") || fail "no nudge to $3: $(cat "$SH/delivered")"
  cmd=${line##*Read it: }
  [ "${cmd%% *}" = "$4/mail.sh" ] || fail "the nudge to $3 names $cmd"
  $cmd | jq -e '.type == "reply"' >/dev/null || fail "the nudge command of $3 does not read the reply"
  "$REP" report --run "$2" --from "$3" --type worker_done --key done --outcome succeeded --subject done >/dev/null 2>&1 || fail "worker_done from $3"
  line=$(grep -m1 "^sup:1 MAIL|[^|]*|worker_done|$3|" "$SH/delivered") || fail "no wake for the worker_done of $3: $(cat "$SH/delivered")"
  id=$(cut -d'|' -f2 <<<"$line")
  cmd=${line##* - handle it: }
  [ "$cmd" = "$(dirname "$REP")/fleet.sh read --run $2 --id $id" ] || fail "the wake of $3 names: $cmd"
  $cmd | jq -e '.type == "worker_done"' >/dev/null || fail "the wake command of $3 does not read the message"
  HERDR_PANE_ID=sup:1 "$1" ack --run "$2" --id "$id" --decision release 2>/dev/null | jq -e '.decision == "release"' >/dev/null || fail "ack through $1"
  "$1" show --run "$2" --dispatch "$("$1" list --run "$2" | jq -r --arg a "$3" '.[] | select(.agent == $a) | .dispatch')" \
    | jq -e '.status == "settled" and .outcome == "succeeded" and .released != null' >/dev/null || fail "$3 was not settled and released"
  "$1" ps --run "$2" --json | jq -e --arg w "$4/fleet.sh wait" '.[0].next == $w' >/dev/null \
    || fail "ps next for $3: $("$1" ps --run "$2" --json | jq -r '.[0].next')"
}

# cycle <script> <run> <agent> <scripts dir the printed commands name>
cycle() {
  begin "$1" "$2" "$3"
  [ "$REP" = "$4/fleet.sh" ] || fail "the reporting rules through $1 name $REP, not $4/fleet.sh"
  [ "$WMAIL" = "$4/mail.sh" ] || fail "the inbox rules through $1 name $WMAIL, not $4/mail.sh"
  finish "$1" "$2" "$3" "$4"
}

# Paths. SK is a skills directory as it is now: dispatch links to the plugin's skill by its old
# name. SN is one after the switch: fleet links to the skill, and dispatch is a directory that
# holds only a scripts link, so no harness lists a second skill.
SK="$TMP/skills-now"; mkdir -p "$SK"; ln -s "$ROOT/dispatch/skill" "$SK/dispatch"
SN="$TMP/skills-new"; mkdir -p "$SN/dispatch"; ln -s "$ROOT/fleet/skill" "$SN/fleet"; ln -s ../fleet/scripts "$SN/dispatch/scripts"
[ ! -e "$SN/dispatch/SKILL.md" ] && [ -f "$SN/fleet/SKILL.md" ] || fail "skills layout"

# Fresh state: agent-fleet.
cycle "$NEW/fleet.sh" c-new w-new "$NEW"
[ -d "$XDG_STATE_HOME/agent-fleet/runs/c-new" ] && [ ! -e "$XDG_STATE_HOME/agent-dispatch" ] || fail "a fresh state is not agent-fleet"
grep -qF "$XDG_STATE_HOME/agent-fleet/runs/c-new/workers/w-new/inbox.jsonl" "$TF" || fail "the inbox of w-new is not under agent-fleet"
cycle "$ROOT/dispatch/skill/scripts/dispatch.sh" c-old-repo w-old-repo "$NEW"
cycle "$SN/dispatch/scripts/dispatch.sh" c-old-skill w-old-skill "$SN/fleet/scripts"
cycle "$SN/fleet/scripts/fleet.sh" c-new-skill w-new-skill "$SN/fleet/scripts"
cycle "$SK/dispatch/scripts/dispatch.sh" c-now-skill w-now-skill "$SK/dispatch/scripts"
"$ROOT/dispatch/skill/scripts/mail.sh" read --inbox "$XDG_STATE_HOME/agent-fleet/runs/c-new/inbox.jsonl" | grep -q . || fail "mail.sh by the old repo path"
"$SN/dispatch/scripts/mail.sh" read --inbox "$XDG_STATE_HOME/agent-fleet/runs/c-new/inbox.jsonl" | grep -q . || fail "mail.sh by the old skills path"
rc=0; "$ROOT/dispatch/skill/scripts/herdr-dispatch.sh" --check-session '{"agent":"claude","kind":"id","value":"x"}' || rc=$?
[ "$rc" = 0 ] || fail "herdr-dispatch.sh by the old path (exit $rc)"
mkdir -p "$TMP/bin"; ln -s "$ROOT/dispatch/bin/fleet" "$TMP/bin/fleet"
"$TMP/bin/fleet" ps --run c-new --json | jq -e 'length == 1' >/dev/null || fail "the fleet command through the old plugin path"
FLEET_RUN=c-new "$NEW/fleet.sh" list | jq -e 'length == 1' >/dev/null || fail "FLEET_RUN"
DISPATCH_RUN=c-new "$NEW/fleet.sh" list | jq -e 'length == 1' >/dev/null || fail "DISPATCH_RUN"

# Before the switch: only agent-dispatch. Runs stay there and agent-fleet is not made.
rm -rf "$XDG_STATE_HOME"; mkdir -p "$XDG_STATE_HOME/agent-dispatch"
cycle "$SK/dispatch/scripts/dispatch.sh" b-old wb-old "$SK/dispatch/scripts"
cycle "$NEW/fleet.sh" b-new wb-new "$NEW"
[ ! -e "$XDG_STATE_HOME/agent-fleet" ] || fail "agent-fleet was made while agent-dispatch was the only state"
[ -d "$XDG_STATE_HOME/agent-dispatch/runs/b-new" ] || fail "a run before the switch is not under agent-dispatch"
# A worker that starts before the switch and finishes after it.
begin "$SK/dispatch/scripts/dispatch.sh" across w-across
grep -qF "$XDG_STATE_HOME/agent-dispatch/runs/across/workers/w-across/inbox.jsonl" "$TF" || fail "the inbox before the switch"

# The switch: agent-dispatch moves to agent-fleet and a link stays at its old name.
mv -T "$XDG_STATE_HOME/agent-dispatch" "$XDG_STATE_HOME/agent-fleet"
ln -s agent-fleet "$XDG_STATE_HOME/agent-dispatch"
finish "$SN/fleet/scripts/fleet.sh" across w-across "$SN/fleet/scripts"
"$NEW/fleet.sh" ps --run b-old --json | jq -e '.[0].status == "settled"' >/dev/null || fail "a run from before the switch"
cycle "$SN/dispatch/scripts/dispatch.sh" a-old wa-old "$SN/fleet/scripts"
cycle "$NEW/fleet.sh" a-new wa-new "$NEW"
grep -qF "$XDG_STATE_HOME/agent-fleet/runs/a-new/workers/wa-new/inbox.jsonl" "$TF" || fail "the inbox after the switch is not under agent-fleet"
[ -L "$XDG_STATE_HOME/agent-dispatch" ] && [ ! -L "$XDG_STATE_HOME/agent-fleet" ] || fail "the layout after the switch changed"

# After the drain: only agent-fleet. Nothing makes agent-dispatch again.
rm "$XDG_STATE_HOME/agent-dispatch"
"$NEW/fleet.sh" ps --run a-new --json | jq -e '.[0].status == "settled"' >/dev/null || fail "a run after the drain"
cycle "$NEW/fleet.sh" d-new wd-new "$NEW"
cycle "$ROOT/dispatch/skill/scripts/dispatch.sh" d-old wd-old "$NEW"
[ ! -e "$XDG_STATE_HOME/agent-dispatch" ] || fail "agent-dispatch was made again after the drain"

echo "compat ok"
