#!/usr/bin/env bash
# Fleet, the supervisor front-end for Herdr workers: a per-run ledger of dispatch attempts, an ack-based
# mailbox, off-pane replies, dependency launches, and a fleet view with liveness + next action.
#
#   fleet.sh pause      [--run <run>]   # checkpoint and fence a subtree, retaining ownership
#   fleet.sh pause-status [--run <run>] # confirmed, pending and failed coverage
#   fleet.sh unpause    --authorization <reconciliation.json> [--run <run>]
#   fleet.sh pause-release --authorization <reconciliation.json> [--run <run>] # authorize existing cleanup; no automatic work
#   fleet.sh run        [--name <slug>] [--wake <types>] [--label <text>] [--link <url>] [--take-over]
#                       bind (create or resume) a run; later commands default to it. This pane becomes the
#                       supervisor and the run's live workers are linked to it. A run whose supervisor pane is
#                       still open moves only with --take-over
#                       --wake: mail types that wake the supervisor (default question,escalation,worker_done)
#                       --label: label of the supervisor's row in the sidebar tree, such as an issue key
#   fleet.sh start      --repo <path> --branch <name> --name <agent> --task <file>
#                       [--label <text>] [--link <url>] [--kind <herdr kind>] [--base <ref>] [--after <dispatch>]... [-- <agent args>]
#                       --label: label of the worker's row. A name that starts with an issue key
#                       (sre-142-fix-probes) gives the label SRE-142 and the name fix-probes.
#                       --link: web address the sidebar tree opens for the row, such as that of the issue
#   fleet.sh wait       [--types t,t] [--timeout <s>]           block until an un-acked message of a wanted type; prints MAIL| lines
#   fleet.sh read       --id <msg>                               full message JSON
#   fleet.sh ack        --id <msg> [--decision reuse|retain|release] [--reason <why>] [--until <condition>]
#                       settle; worker_done requires --decision, and retain or reuse also --reason and --until
#   fleet.sh reply      --id <question> (--body <text> | --body-file <f>)   answer into the worker's inbox
#   fleet.sh send       --agent <name> --subject <s> (--body <text> | --body-file <f>)   follow-up into the worker's inbox
#   fleet.sh ps         [--json] [--tree [--all]]                fleet: status, liveness, attention, next action
#                       --tree: also the runs that this run's workers supervise, at every depth, indented under
#                       their attempt; done rows are hidden unless --all
#   fleet.sh retry      --dispatch <d> [--force]                 new attempt of the same task; old attempt abandoned
#                       retry and resume refuse once the task has run $FLEET_MAX_ATTEMPTS attempts (default 5) unless --force
#   fleet.sh resume     --dispatch <d> [--force] [--dry-run]     new attempt that resumes the old one's conversation in its
#                       worktree, for every kind Herdr can resume; --dry-run prints the plan and writes nothing
#   fleet.sh release    --dispatch <d>                           end a kept (retain or reuse) attempt: remove its worktree, which
#                       kills a live agent, and record the release. A worktree that is already gone is only recorded
#   fleet.sh stop       --dispatch <d>                           kill the worker (removes its worktree) and mark stopped
#   fleet.sh abandon    --dispatch <d>                           mark abandoned; worker left running for inspection
#   fleet.sh show       --dispatch <d> | list                    an attempt's session and transcript are kept after release
#   fleet.sh report     --from <agent> --type <t> --subject <s> [--key <k>] [--body <text> | --body-file <f>] [--outcome <o>]
#                       worker side: mail the supervisor, and wake it for the run's wake types. A repeat is not sent
#                       again and returns the first id: the same --key still un-acked or sent in the agent's current
#                       attempt, or with no key the same content still un-acked. A repeat of an un-acked message
#                       wakes the supervisor again for that message
#   fleet.sh notify     --line <text> [--to <agent|pane>] [--title <toast title>]
#                       type one line into the supervisor (or --to) from any process; exit 3 = no such agent
#   fleet.sh adopt                                               link the live workers of every run to their
#                       supervisor in Herdr, label them and record their sessions; for runs started before Herdr kept the links
#
# Run selection: --run <id> anywhere, else $FLEET_RUN (or $DISPATCH_RUN), else the run last bound from this pane.
# Ledger: <state>/agent-fleet/runs/<run>/ledger.json. Mailbox: <run>/inbox.jsonl (worker -> supervisor),
# <run>/workers/<agent>/inbox.jsonl (supervisor -> worker).
# Herdr holds the rest: who supervises which pane, the wake lines that wait for a safe moment, and
# the reports of a blocked, stalled or gone worker.
set -euo pipefail

# A directory by its fleet name, when the caller came in through an old dispatch name that leads
# to the same place. The commands this script prints then name fleet.
fleet_dir() {  # <dir>
  local c; c=$(printf '%s' "$1" | sed -E 's#(.*)/dispatch/#\1/fleet/#')
  if [ "$c" != "$1" ] && [ "$c" -ef "$1" ]; then printf '%s' "$c"; else printf '%s' "$1"; fi
}
HERE=$(fleet_dir "$(cd "$(dirname "$0")" && pwd)")
SELF="$HERE/fleet.sh"
MAIL="$HERE/mail.sh"
# State lives in agent-fleet, or in agent-dispatch while that is the only one.
STATE="${XDG_STATE_HOME:-$HOME/.local/state}/agent-fleet"
[ -e "$STATE" ] || [ ! -e "${STATE%/*}/agent-dispatch" ] || STATE="${STATE%/*}/agent-dispatch"
RUNS="$STATE/runs"
# What Herdr reports to a supervisor about a worker that has not finished.
REPORTS=blocked,exited,stalled
POINTER="$STATE/current-run${HERDR_PANE_ID:+.${HERDR_PANE_ID//:/_}}"

die() { echo "fleet.sh: $*" >&2; exit 1; }
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
new_id() { echo "d_$(date +%Y%m%d%H%M%S)_$RANDOM"; }
for bin in jq herdr flock; do command -v "$bin" >/dev/null || die "$bin not on PATH"; done

CMD=${1:-}; shift || true
[ -n "$CMD" ] || { sed -n '2,/^set -euo pipefail/{/^set -euo pipefail/!p}' "$0" >&2; exit 2; }

# --run may appear anywhere; strip it before per-command parsing.
RUN=${FLEET_RUN:-${DISPATCH_RUN:-}}
ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in --run) RUN=$2; shift 2 ;; *) ARGS+=("$1"); shift ;; esac
done
set -- "${ARGS[@]+"${ARGS[@]}"}"

bind_run() {
  [ -n "$RUN" ] || RUN=$(cat "$POINTER" 2>/dev/null || true)
  [ -n "$RUN" ] || die "no run bound: run '$SELF run' first, or pass --run <id>"
  RUN_DIR="$RUNS/$RUN"; LEDGER="$RUN_DIR/ledger.json"; INBOX="$RUN_DIR/inbox.jsonl"
  [ -f "$LEDGER" ] || die "run '$RUN' has no ledger at $LEDGER"
}

# ledger_mod [jq args...] '<filter over the array>'  -- atomic read-modify-write.
ledger_mod() {
  ( flock 9
    TMP=$(mktemp "$LEDGER.XXXXXX"); jq "$@" "$LEDGER" >"$TMP" && mv "$TMP" "$LEDGER"
  ) 9>"$LEDGER.lock"
}
entry() { jq -c --arg d "$1" '.[] | select(.dispatch == $d)' "$LEDGER"; }

# The attempts of a task, oldest first: every entry whose supersedes chain leads to the same first attempt.
MAX_ATTEMPTS=${FLEET_MAX_ATTEMPTS:-${DISPATCH_MAX_ATTEMPTS:-5}}
task_attempts() {  # <dispatch>
  jq -r --arg d "$1" '(map({key: .dispatch, value: .supersedes}) | from_entries) as $up
    | def first_attempt: if ($up[.] // null) == null then . else $up[.] | first_attempt end;
    ($d | first_attempt) as $f | .[] | select(.dispatch | first_attempt == $f) | .dispatch' "$LEDGER"
}
# Refuse a new attempt of a task that has run the limit, unless forced.
check_attempts() {  # <dispatch> <force 0|1>
  local ids n
  ids=$(task_attempts "$1"); n=$(grep -c . <<<"$ids")
  [ "$2" = 1 ] || [ "$n" -lt "$MAX_ATTEMPTS" ] \
    || die "this task has run $n attempts ($(paste -sd' ' <<<"$ids")); the limit is $MAX_ATTEMPTS. Pass --force to start another"
}

# The attempt that took over a kept attempt's agent and worktree, or null: the same agent started later,
# or an attempt down its supersedes chain, that ran. An abandoned or failed attempt takes nothing over.
# Input: the ledger.
REPLACED_JQ='def replaced_by($e):
    (map({key: .dispatch, value: .supersedes}) | from_entries) as $up
    | def above: ($up[.] // null) | if . == null then empty else ., above end;
    [.[] | select(.dispatch != $e.dispatch and .started != null
        and (.status == "working" or .status == "settled" or .status == "stopped")
        and ((.agent == $e.agent and $e.agent != null and .started > ($e.started // ""))
             or (.dispatch | [above] | index($e.dispatch)) != null))]
    | sort_by(.started) | last | .dispatch;'

# Dependencies of a dispatch are met when every `after` is settled with outcome succeeded.
deps_met() {
  [ "$(jq -r --arg d "$1" '
    (map(select(.status == "settled" and .outcome == "succeeded") | .dispatch)) as $ok
    | .[] | select(.dispatch == $d) | ((.after // []) | all(. as $a | $ok | any(. == $a)))' "$LEDGER")" = true ]
}

# The issue key at the start of a name, upper case, or nothing: sre-142-fix-probes -> SRE-142.
ticket_of() { printf '%s' "$1" | sed -nE 's/^([A-Za-z]{2,6})-([0-9]{1,5})(-.*)?$/\1-\2/p' | tr '[:lower:]' '[:upper:]'; }

# Set or clear Herdr's well-known label token of a pane, shown at the right edge of its row.
label_pane() {  # <pane> <text>
  [ -n "$1" ] || return 0
  if [ -n "$2" ]; then herdr pane report-metadata "$1" --source fleet --token "label=$2" >/dev/null 2>&1 || true
  else herdr pane report-metadata "$1" --source fleet --clear-token label >/dev/null 2>&1 || true; fi
}

# Set Herdr's well-known link token of a pane: the web address a sidebar opens for its row.
# No address sets nothing.
link_pane() {  # <pane> <url>
  [ -n "$1" ] && [ -n "$2" ] || return 0
  herdr pane report-metadata "$1" --source fleet --token "link=$2" >/dev/null 2>&1 || true
}

# Refuse a link that is not an http or https address.
check_link() {  # <url>
  case "$1" in ''|http://?*|https://?*) ;; *) die "--link takes an http or https address" ;; esac
  case "$1" in *[[:space:]]*) die "--link takes an address with no spaces" ;; esac
}

# Make the run's supervisor the supervisor of a pane in Herdr. With reports, Herdr tells the
# supervisor when the worker blocks, stalls or goes away.
supervise() {  # <pane|agent> [reports]
  local sup; sup=$(supervisor)
  [ -n "$1" ] && [ -n "$sup" ] || return 0
  herdr pane supervise "$1" --by "$sup" ${2:+--report "$2"} >/dev/null 2>&1 || true
}

# Hand one line to Herdr for an agent's pane. Herdr types it at once when that is safe, and holds
# it otherwise. A toast tells a person who is in the pane; an agent that works needs none, because
# it gets the line when its turn ends. Status 1 means Herdr did not take the line, and
# DELIVER_ERROR says why.
DELIVER_ERROR=
deliver() {  # <agent|pane> <prompt line> <toast title> <toast body>
  local out
  DELIVER_ERROR="no pane"
  [ -n "$1" ] || return 1
  if fleet_pause_active "$1"; then DELIVER_ERROR="subtree_paused"; return 1; fi
  out=$(herdr agent deliver "$1" "$2" --source fleet 2>&1) || {
    DELIVER_ERROR=$(jq -r '.error.code // empty' <<<"$out" 2>/dev/null || true)
    [ -n "$DELIVER_ERROR" ] || DELIVER_ERROR="herdr did not answer"
    return 1
  }
  case "$(jq -r '.result.delivery.held_by // empty' <<<"$out")" in
    focused|unsent_input) herdr notification show "$3" --body "$4" --sound request >/dev/null 2>&1 || true ;;
  esac
}
supervisor() { cat "$RUN_DIR/supervisor" 2>/dev/null || true; }
# The run's supervisor pane when Herdr no longer has it, or nothing.
gone_supervisor() {
  local sup; sup=$(supervisor)
  [ -z "$sup" ] || herdr pane get "$sup" >/dev/null 2>&1 || printf '%s' "$sup"
}
notify_supervisor() {  # <prompt line> <toast title> <toast body>
  deliver "$(supervisor)" "$@" || echo "fleet.sh: note: the supervisor was not woken ($DELIVER_ERROR); the message is in the run inbox" >&2
}

# Tell a worker it has mail. The message is in its inbox either way.
nudge() {  # <agent> <prompt line>
  deliver "$1" "$2" "fleet: mail for $1" "$2" && return 0
  echo "fleet.sh: note: $1 was not nudged ($DELIVER_ERROR); the message is in its inbox" >&2
}

launch_dispatch() {
  local D=$1 spec res
  spec=$(entry "$D" | jq -c .launch)
  if fleet_pause_active "$(jq -r '.name // empty' <<<"$spec")"; then
    echo "fleet.sh: $D remains pending: subtree paused" >&2
    return 0
  fi
  [ -n "$spec" ] && [ "$spec" != null ] || { echo "fleet.sh: no launch spec for $D" >&2; return 1; }
  res=$(entry "$D" | jq -c '.resume // empty')
  local args=() aargs=() x
  if [ -n "$res" ]; then
    args=(--resume "$(jq -r .worktree <<<"$res")" --session "$(jq -c .session <<<"$res")" --name "$(jq -r .agent <<<"$res")" \
          --prompt-file "$(jq -r .notice <<<"$res")" --run-dir "$RUN_DIR")
    x=$(jq -r '.pane // ""' <<<"$res"); [ -z "$x" ] || args+=(--pane "$x")
    while IFS= read -r x; do aargs+=("$x"); done < <(jq -r '.agent_args[]' <<<"$res")
  else
    args=(--repo "$(jq -r .repo <<<"$spec")" --branch "$(jq -r .branch <<<"$spec")" \
          --name "$(jq -r .name <<<"$spec")" \
          --prompt-file "$(jq -r .task <<<"$spec")" --run-dir "$RUN_DIR")
    # No kind in the spec means inherit the caller's: the launcher resolves it.
    x=$(jq -r '.kind // ""' <<<"$spec"); [ -z "$x" ] || args+=(--kind "$x")
    x=$(jq -r '.base // ""' <<<"$spec"); [ -z "$x" ] || args+=(--base "$x")
    while IFS= read -r x; do aargs+=("$x"); done < <(jq -r '.agent_args[]?' <<<"$spec")
  fi
  [ ${#aargs[@]} -eq 0 ] || args+=(-- "${aargs[@]}")

  local receipt rc=0 t0; t0=$(now)
  receipt=$("$HERE/herdr-fleet.sh" "${args[@]}") || rc=$?
  if [ -z "$receipt" ]; then
    ledger_mod --arg d "$D" --arg ts "$(now)" 'map(if .dispatch == $d then .status = "failed_start" | .started = $ts else . end)'
    echo "fleet.sh: launcher failed for $D (see stderr above)" >&2; return 1
  fi
  local st ls; st=$(jq -r .status <<<"$receipt")
  case "$st" in working|idle|done|blocked|unknown) ls=working ;; *) ls=failed_start ;; esac
  ledger_mod --arg d "$D" --arg ts "$(now)" --arg ls "$ls" --argjson r "$receipt" \
    'map(if .dispatch == $d then . + {launch: (.launch + {kind: $r.kind}),
          status: $ls, started: $ts, agent: $r.agent,
          workspace_id: $r.workspace_id, pane_id: $r.pane_id, worktree: $r.worktree, branch: $r.branch,
          prompt_file: $r.task_file, worker_inbox: $r.worker_inbox, agent_status: $r.status} else . end)'
  local ag pane; ag=$(jq -r .agent <<<"$receipt"); pane=$(jq -r '.pane_id // empty' <<<"$receipt")
  [ "$ls" != working ] || record_session "$D"
  label_pane "$pane" "$(jq -r '.label // ""' <<<"$spec")"
  link_pane "$pane" "$(jq -r '.link // ""' <<<"$spec")"
  # A fast worker can report worker_done before the launcher returns; it then gets no reports.
  if [ "$ls" = working ] && [ -z "$("$MAIL" read --inbox "$INBOX" --types worker_done | jq -c --arg a "$ag" --arg t "$t0" 'select(.from == $a and .ts >= $t)')" ]; then
    supervise "$pane" "$REPORTS"
  else
    supervise "$pane"
  fi
  jq -c --arg d "$D" '. + {dispatch: $d}' <<<"$receipt"
  return $rc
}

# Launch every pending dispatch whose dependencies are now met.
launch_ready() {
  local D
  for D in $(jq -r '.[] | select(.status == "pending") | .dispatch' "$LEDGER"); do
    deps_met "$D" && { launch_dispatch "$D" || true; }
  done
}

# Close the parent workspace Herdr opened on <repo root> for a worktree, once it is unused:
# marked, no linked worktree workspace left, one pane, no agent.
close_idle_parent() {  # <repo path>
  local root list parent
  root=$(git -C "$1" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || return 0
  root=$(dirname "$root")
  list=$(herdr workspace list 2>/dev/null) || return 0
  [ "$(jq --arg k "$root/.git" '[.result.workspaces[]? | select(.worktree.repo_key == $k and .worktree.is_linked_worktree)] | length' <<<"$list")" = 0 ] || return 0
  # The mark is Herdr's. The token marks a parent that a server without the mark opened.
  parent=$(jq -r --arg root "$root" 'first(.result.workspaces[]? | select(.worktree.checkout_path == $root
      and .worktree.is_linked_worktree == false and .pane_count == 1
      and (.opened_for_worktrees == true or .tokens.fleet_parent == "1" or .tokens.dispatch_parent == "1")) | .workspace_id) // empty' <<<"$list")
  [ -n "$parent" ] || return 0
  [ "$(herdr agent list 2>/dev/null | jq --arg w "$parent" '[.result.agents[]? | select(.workspace_id == $w)] | length')" = 0 ] || return 0
  # A plain close: Herdr refuses it if the group still has a worktree workspace.
  herdr workspace close "$parent" >/dev/null 2>&1 || true
}

# Claude Code's per-cwd temp dir (tool output, subagent files, scratchpad) for a removed worktree.
# The dir name is the cwd with every non-alphanumeric character replaced by "-".
clear_claude_tmp() {
  local wt=$1 dir
  [ -n "$wt" ] || return 0
  dir="${CLAUDE_CODE_TMPDIR:-/tmp}/claude-$(id -u)/$(printf '%s' "$wt" | sed 's/[^A-Za-z0-9]/-/g')"
  [ -d "$dir" ] && rm -rf -- "$dir"
  return 0
}

# The transcript file a Herdr session reference names, or nothing. Only a file that exists counts:
# no pointer is fine, a wrong one is not.
transcript_of() {  # <agent_session json>
  local agent kind val f=() d x
  agent=$(jq -r '.agent // ""' <<<"$1"); kind=$(jq -r '.kind // ""' <<<"$1"); val=$(jq -r '.value // ""' <<<"$1")
  case "$agent:$kind" in
    pi:path|omp:path) case "$val" in /*) [ -f "$val" ] && printf '%s' "$val" ;; esac; return 0 ;;
  esac
  # An id names one file by itself, so it is safe to glob for when it holds no glob characters.
  case "$val" in ""|*[!A-Za-z0-9-]*) return 0 ;; esac
  [ "$kind" = id ] || return 0
  case "$agent" in
    claude) d="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/projects"; for x in "$d"/*/"$val".jsonl; do [ -f "$x" ] && f+=("$x"); done ;;
    codex) d="${CODEX_HOME:-$HOME/.codex}/sessions"; for x in "$d"/*/*/*/rollout-*-"$val".jsonl; do [ -f "$x" ] && f+=("$x"); done ;;
  esac
  [ ${#f[@]} -ne 1 ] || printf '%s' "${f[0]}"
}

# Record Herdr's session reference of an attempt's agent, and its transcript, on the attempt.
# The transcript outlives release, so `show` leads to it later.
record_session() {  # <dispatch>
  local E agent pane info ref tr
  E=$(entry "$1"); agent=$(jq -r '.agent // empty' <<<"$E"); pane=$(jq -r '.pane_id // empty' <<<"$E")
  [ -n "$agent" ] && [ -n "$pane" ] || return 0
  info=$(herdr agent get "$pane" 2>/dev/null) || return 0
  # The pane must still hold this attempt's agent.
  [ "$(jq -r '.result.agent.name // empty' <<<"$info" 2>/dev/null)" = "$agent" ] || return 0
  ref=$(jq -c '.result.agent.agent_session // empty' <<<"$info" 2>/dev/null) || return 0
  [ -n "$ref" ] || return 0
  tr=$(transcript_of "$ref")
  ledger_mod --arg d "$1" --argjson ref "$ref" --arg tr "$tr" 'map(if .dispatch == $d then
      .transcript = (if $tr != "" then $tr elif .session.value == $ref.value then .transcript else null end)
      | .session = $ref else . end)'
}

# Release = post-settlement cleanup: removes the worktree, which also kills a live agent.
# Safe to run again: a worktree that is already gone is recorded as released.
release_worker() {
  local agent=$1 D=$2 ws out E wt
  E=$(entry "$D")
  [ "$(jq -r '.released // empty' <<<"$E")" = "" ] || return 0
  wt=$(jq -r '.worktree // empty' <<<"$E")
  # A worker that an older version placed as a tab shares its supervisor's workspace, so the
  # workspace must not be removed for it.
  if [ "$(jq -r '.placement // "workspace"' <<<"$E")" = tab ]; then
    echo "fleet.sh: warning: $agent was placed as a tab by an older version; nothing removed" >&2
    echo "fleet.sh: remove it by hand. Find its tab with: herdr agent get $agent" >&2
    echo "fleet.sh: then: herdr tab close <tab>; git worktree remove $(printf '%q' "$(jq -r '.worktree // "<worktree>"' <<<"$E")")" >&2
    return 0
  fi
  ws=$(herdr agent get "$agent" 2>/dev/null | jq -r '.result.agent.workspace_id // empty' || true)
  [ -n "$ws" ] || ws=$(jq -r '.workspace_id // empty' <<<"$E")
  if [ -n "$wt" ] && [ ! -d "$wt" ]; then
    out=
  elif [ -z "$ws" ]; then
    echo "fleet.sh: warning: no workspace known for $agent; nothing removed" >&2; return 0
  elif ! out=$(herdr worktree remove --workspace "$ws" 2>&1); then
    echo "fleet.sh: warning: release of $agent failed: $out" >&2
    echo "fleet.sh: inspect the worktree, then: herdr worktree remove --workspace $ws --force" >&2
    # ps shows the failure until the worktree is gone.
    ledger_mod --arg d "$D" --arg err "$(jq -r '.error.code // empty' <<<"$out" 2>/dev/null || true)" \
      'map(if .dispatch == $d then .release_error = (if $err == "" then "release failed" else $err end) else . end)'
    return 0
  fi
  ledger_mod --arg d "$D" --arg ts "$(now)" 'map(if .dispatch == $d then .released = $ts | del(.release_error) else . end)'
  close_idle_parent "$(jq -r '.launch.repo // empty' <<<"$E")"
  clear_claude_tmp "$wt"
}

# Link the live workers of the bound run to its supervisor in Herdr, label them from their names
# and record their sessions. Prints how many it linked.
link_workers() {
  local sup n=0 disp agent status ledger_pane started pane fin key
  sup=$(supervisor)
  herdr pane get "$sup" >/dev/null 2>&1 || { echo 0; return 0; }
  while IFS=$'\t' read -r disp agent status ledger_pane started; do
    pane=$(herdr agent get "$agent" 2>/dev/null | jq -r '.result.agent.pane_id // empty' || true)
    # The name must still be on the pane the ledger has. A later run can own the name now.
    [ -n "$pane" ] && [ "$pane" = "$ledger_pane" ] && [ "$pane" != "$sup" ] || continue
    # A worker that said it is done is not watched, also while that mail waits for its ack.
    # An earlier attempt can have had the same name, so only mail since this one started counts.
    fin=$("$MAIL" read --inbox "$RUN_DIR/inbox.jsonl" --types worker_done | jq -c --arg a "$agent" --arg t "$started" 'select(.from == $a and .ts >= $t)' | head -1 || true)
    if [ "$status" = working ] && [ -z "$fin" ]; then supervise "$pane" "$REPORTS"; else supervise "$pane"; fi
    record_session "$disp"
    key=$(ticket_of "$agent"); [ -n "$key" ] || key=$(ticket_of "$RUN")
    [ -z "$key" ] || [ -n "$(herdr pane get "$pane" | jq -r '.result.pane.tokens.label // empty')" ] || label_pane "$pane" "$key"
    n=$((n + 1))
  done < <(jq -r '.[] | select(.agent != null and (.status == "working" or .decision == "retain" or .decision == "reuse")) | [.dispatch, .agent, .status, (.pane_id // "-"), (.started // "")] | @tsv' "$LEDGER")
  echo "$n"
}

# The fleet rows of the bound run, one JSON object per attempt. <run arg> follows the command word of
# every fleet.sh command in NEXT, such as " --run <run>" for a run that is not the caller's.
ps_rows() {  # <run arg>
  local UNACKED AS A ST LEFT DONES GONE
  UNACKED=$("$MAIL" read --inbox "$INBOX" --unacked | jq -sc .)
  AS='{}'
  for A in $(jq -r '.[] | select(.status == "working") | .agent // empty' "$LEDGER" | sort -u); do
    ST=$(herdr agent get "$A" 2>/dev/null | jq -r '.result.agent | if . == null then "missing" elif .stalled_secs then "stalled" else .agent_status end' || echo missing)
    AS=$(jq -c --arg a "$A" --arg s "$ST" '.[$a] = $s' <<<"$AS")
  done
  # Attempts that were to be released and still have a worktree: a release that failed or never ran.
  # A kept attempt counts once a release of it failed.
  LEFT=$(jq -r '.[] | select(.released == null and .worktree != null
      and ((.status == "settled" and .decision == "release") or .status == "stopped"
           or (.status == "settled" and (.decision == "retain" or .decision == "reuse") and .release_error != null)))
      | [.dispatch, .worktree] | @tsv' "$LEDGER" \
    | while IFS=$'\t' read -r DD WT; do [ ! -d "$WT" ] || echo "$DD"; done | jq -R . | jq -sc .)
  # Kept attempts that hold nothing: no later attempt took them over, their worktree is gone, and
  # their agent does not run on their pane.
  GONE=$(jq -r "$REPLACED_JQ"' . as $l | .[] | select(.status == "settled" and (.decision == "retain" or .decision == "reuse")
      and .released == null and .worktree != null) | select(. as $e | $l | replaced_by($e) == null)
      | [.dispatch, .worktree, (.agent // ""), (.pane_id // "")] | @tsv' "$LEDGER" \
    | while IFS=$'\t' read -r DD WT A P; do
        [ ! -d "$WT" ] || continue
        [ -n "$A" ] && herdr agent get "$A" 2>/dev/null | jq -e --arg p "$P" '.result.agent.pane_id as $q | $q != null and ($p == "" or $q == $p)' >/dev/null && continue
        echo "$DD"
      done | jq -R . | jq -sc .)
  DONES=$("$MAIL" read --inbox "$INBOX" --types worker_done | jq -sc 'map({id, from, ts})')
  jq -c --argjson un "$UNACKED" --argjson as "$AS" --arg mail "$SELF" --argjson left "$LEFT" --argjson gone "$GONE" --argjson dones "$DONES" --arg ra "$1" "$REPLACED_JQ"'
    . as $l
    | (map(select(.status == "settled" and .outcome == "succeeded") | .dispatch)) as $ok
    | (map(select(.status == "settled" and .outcome != "succeeded" or .status == "abandoned" or .status == "stopped" or .status == "failed_start") | .dispatch)) as $bad
    | .[]
    | . as $e
    | ($un | map(select(.from == $e.agent))) as $mine
    | (if $e.status == "settled" and ($e.decision == "retain" or $e.decision == "reuse") and $e.released == null
       then $l | replaced_by($e) else null end) as $rep
    | (if $e.status == "pending" then "-"
       elif $e.status != "working" then $e.status
       else ($as[$e.agent] // "missing") as $st
         | if $st == "missing" then "exited"
           elif $st == "blocked" then "stuck"
           elif $st == "stalled" then "stalled"
           else "live"
           end
       end) as $live
    | (($e.after // []) | map(select(. as $a | $bad | any(. == $a)))) as $failed_deps
    | (($e.after // []) | map(select(. as $a | $ok | any(. == $a) | not))) as $unmet
    | (if $e.status == "pending" then (if ($failed_deps | length) > 0 then "dep failed" elif ($unmet | length) > 0 then "waiting deps" else "ready" end)
       elif ($left | index($e.dispatch)) != null then "unreleased"
       elif ($mine | any(.type == "escalation")) then "escalation"
       elif ($mine | any(.type == "worker_done")) then "worker_done"
       elif ($mine | any(.type == "question")) then "question"
       elif $live == "stuck" then "stuck"
       elif $live == "stalled" then "stalled"
       elif $live == "exited" and $e.status == "working" then "exited"
       elif $e.status == "failed_start" then "failed_start"
       elif ($gone | index($e.dispatch)) != null then "worktree gone"
       elif ($mine | any(.type == "status")) then "status"
       else "none" end) as $att
    | (($mine | map(select(.type == $att)) | first | .id) // "") as $mid
    | ($e.settled_by // ($dones | map(select(.from == $e.agent and .ts <= ($e.settled // ""))) | last | .id)) as $done_id
    | (if $e.status == "stopped" then "\($mail) stop\($ra) --dispatch \($e.dispatch)"
       elif $e.status == "settled" and ($e.decision == "retain" or $e.decision == "reuse") then "\($mail) release\($ra) --dispatch \($e.dispatch)"
       elif $done_id != null then "\($mail) ack\($ra) --id \($done_id) --decision release"
       else "herdr worktree remove --workspace \($e.workspace_id)" end) as $finish
    | (if $att == "unreleased" then
         (if ($e.placement // "") == "tab" then "close its tab by hand (herdr agent get \($e.agent)), then git worktree remove \($e.worktree)"
          elif $e.release_error != null then "release failed (\($e.release_error)): look at what is dirty in \($e.worktree), then herdr worktree remove --workspace \($e.workspace_id) --force; then \($finish)"
          else $finish end)
       elif $att == "escalation" then "tell the user; \($mail) read\($ra) --id \($mid)"
       elif $att == "worker_done" then "\($mail) ack\($ra) --id \($mid) --decision reuse|retain|release"
       elif $att == "question" then "\($mail) reply\($ra) --id \($mid) --body ..."
       elif $att == "stuck" then "herdr agent get \($e.agent) | jq -r .result.agent.blocker"
       elif $att == "stalled" then "herdr agent read \($e.agent) --source recent-unwrapped --lines 120"
       elif $att == "exited" and ($e.session.value // "") != ""
         then "inspect; \($mail) resume\($ra) --dispatch \($e.dispatch), retry or abandon"
       elif $att == "exited" then "inspect; \($mail) retry\($ra) --dispatch \($e.dispatch) or abandon"
       elif $att == "failed_start" then "\($mail) show\($ra) --dispatch \($e.dispatch); \($mail) retry\($ra) --dispatch \($e.dispatch)"
       elif $att == "worktree gone" then "\($mail) release\($ra) --dispatch \($e.dispatch)"
       elif $att == "status" then "\($mail) read\($ra) --id \($mid); \($mail) ack\($ra) --id \($mid)"
       elif $att == "dep failed" then "\($mail) retry\($ra) --dispatch \($failed_deps | first)"
       elif $att == "waiting deps" then "-"
       elif $att == "ready" then "launching on next ack"
       elif $rep != null then "replaced by \($rep)"
       elif ($e.decision == "retain" or $e.decision == "reuse") and $e.released == null then
         "kept (\($e.decision)): \($e.decision_reason // "no reason recorded"); release when \($e.release_when // "not recorded")"
       else "\($mail) wait\($ra)" end) as $next
    | {dispatch: $e.dispatch, agent: ($e.agent // "-"), attempt: $e.attempt, status: $e.status, outcome: ($e.outcome // "-"),
       liveness: $live, attention: $att, next: $next, unacked: ($mine | length), after: ($e.after // []),
       decision: $e.decision, reason: $e.decision_reason, until: $e.release_when, transcript: $e.transcript,
       released: $e.released, release_error: $e.release_error, replaced_by: $rep}' "$LEDGER"
}

# Nested runs: a run whose supervisor pane is the pane of an attempt in another run is a child of
# that attempt. The time an attempt stops holding its pane is its release, or the close of an
# attempt that keeps no agent; one that runs or is kept (retain, reuse) holds it still (null).
HELD_JQ='def held_until: if .released != null then .released
    elif .status == "working" or .status == "pending" then null
    elif .status == "settled" and (.decision == "retain" or .decision == "reuse") then null
    else .settled // .started end;
  def candidates($pane; $t; $run): .runs as $runs | .attempts | map(select(.run != $run and .pane == $pane
    and .started <= $t and (.end == null or .end >= $t)) | . + {supervisor: $runs[.run].supervisor});'

# Every run with a ledger: its supervisor pane, when that pane last bound it (the mtime of the
# supervisor file), its recorded parent, and every attempt that held a pane.
runs_index() {
  local d sup meta= parents=()
  for d in "$RUNS"/*/; do
    d=${d%/}; [ -f "$d/ledger.json" ] || continue
    sup=; [ ! -f "$d/supervisor" ] || read -r sup <"$d/supervisor" || true
    meta+="${d##*/}"$'\t'"$sup"$'\t'"$([ -z "$sup" ] || date -u -r "$d/supervisor" +%Y-%m-%dT%H:%M:%SZ)"$'\n'
    [ ! -f "$d/parent" ] || parents+=("$d/parent")
  done
  [ -n "$meta" ] || { echo '{"runs":{},"attempts":[]}'; return 0; }
  jq -nc --arg meta "$meta" "$HELD_JQ"'
    ($meta | split("\n") | map(select(length > 0) | split("\t")
      | {key: .[0], value: {supervisor: .[1], bound: .[2], parent: null}}) | from_entries) as $m
    | reduce inputs as $x ({runs: $m, attempts: []};
        (input_filename | split("/")) as $p
        | if $p[-1] == "parent" then .runs[$p[-2]].parent = $x
          else .attempts += [$x[] | select(.pane_id != null and .started != null)
            | {run: $p[-2], dispatch, agent, pane: .pane_id, started, end: held_until}] end)' \
    "$RUNS"/*/ledger.json "${parents[@]+"${parents[@]}"}"
}

# The one attempt among <candidates> that is the parent of a run supervised from <pane>, or nothing.
# Herdr's supervision link of the pane, when it has one, must name the supervisor of that attempt's run.
pick_parent() {  # <pane> <candidates json>
  local s
  [ "$(jq length <<<"$2")" -gt 0 ] || return 0
  s=$(herdr agent get "$1" 2>/dev/null | jq -r '.result.agent.supervisor_pane_id // empty' 2>/dev/null || true)
  jq -c --arg s "$s" 'map(select($s == "" or .supervisor == $s))
    | if length == 1 then .[0] | {run, dispatch} else empty end' <<<"$2"
}

# The parent of each run: the one recorded at bind, else, for a run bound before parents were
# recorded, the one attempt that held the run's supervisor pane when that pane bound it.
run_parents() {  # <index json>
  local ix=$1 out r pane cands p line
  out=$(jq -c '.runs | with_entries(select(.value.parent.dispatch != null)
    | .value = (.value.parent | {run, dispatch, how: "recorded"}))' <<<"$ix")
  while IFS= read -r line; do
    r=$(jq -r .run <<<"$line"); pane=$(jq -r .pane <<<"$line"); cands=$(jq -c .cands <<<"$line")
    p=$(pick_parent "$pane" "$cands")
    [ -z "$p" ] || out=$(jq -c --arg r "$r" --argjson p "$p" '.[$r] = $p + {how: "inferred"}' <<<"$out")
  done < <(jq -c "$HELD_JQ"' . as $ix | .runs | to_entries[]
    | select(.value.parent == null and .value.supervisor != "")
    | .key as $r | .value as $v
    | {run: $r, pane: $v.supervisor, cands: ($ix | candidates($v.supervisor; $v.bound; $r))}
    | select(.cands | length > 0)' <<<"$ix")
  echo "$out"
}

# Record the parent of the bound run, from the pane that now supervises it. A run bound from a
# pane that is no attempt's gets a record with no dispatch, so its parent is never inferred.
record_parent() {
  local ix p t; t=$(now)
  ix=$(runs_index)
  p=$(pick_parent "$HERDR_PANE_ID" "$(jq -c --arg p "$HERDR_PANE_ID" --arg t "$t" --arg r "$RUN" "$HELD_JQ"' candidates($p; $t; $r)' <<<"$ix")")
  [ -n "$p" ] || p='{}'
  jq -c --arg pane "$HERDR_PANE_ID" --arg t "$t" '. + {pane: $pane, bound: $t}' <<<"$p" >"$RUN_DIR/parent.new"
  mv "$RUN_DIR/parent.new" "$RUN_DIR/parent"
  jq -c 'select(.dispatch != null) | {run, dispatch}' "$RUN_DIR/parent"
}

# ps --tree: the bound run and, under each attempt, the runs that attempt supervises, at every depth.
# A row that is done (settled, stopped or abandoned, with nothing owed, and nothing kept or kept but
# replaced) and has no shown row below it is hidden unless <all> is 1. Prints the tree as JSON.
ps_tree() {  # <all 0|1>
  local ix parents runs r nodes= root=$RUN
  ix=$(runs_index); parents=$(run_parents "$ix")
  runs=$(jq -r --arg root "$root" --argjson p "$parents" '
    def kids($r): $p | to_entries | map(select(.value.run == $r) | .key);
    def down($r; $seen): $r, (kids($r)[] | select(. as $k | $seen | any(.[]; . == $k) | not) as $k | down($k; $seen + [$k]));
    down($root; [$root])' <<<"{}")
  for r in $runs; do
    RUN=$r; RUN_DIR="$RUNS/$r"; LEDGER="$RUN_DIR/ledger.json"; INBOX="$RUN_DIR/inbox.jsonl"
    nodes+=$(ps_rows "$([ "$r" = "$root" ] || echo " --run $r")" | jq -sc --arg r "$r" --arg gone "$(gone_supervisor)" \
      --arg sup "$(supervisor)" '{run: $r, supervisor: $sup, supervisor_gone: ($gone != ""), rows: .}')$'\n'
  done
  RUN=$root; RUN_DIR="$RUNS/$root"; LEDGER="$RUN_DIR/ledger.json"; INBOX="$RUN_DIR/inbox.jsonl"
  jq -sc 'map({key: .run, value: .}) | from_entries' <<<"$nodes" \
  | jq -c --arg root "$root" --argjson p "$parents" --argjson all "$1" --arg mail "$SELF" '. as $R
    | def done: .attention == "none" and (.status == "settled" or .status == "stopped" or .status == "abandoned")
        and ((.decision == "retain" or .decision == "reuse") and .released == null and .replaced_by == null | not);
      def node($r; $seen):
        ($R[$r].rows | map(. as $row | . + {runs: [$p | to_entries[]
            | select(.value.run == $r and .value.dispatch == $row.dispatch and (.key as $k | $seen | any(.[]; . == $k) | not))
            | node(.key; $seen + [.key])]})) as $all_rows
        | ($all_rows | map(select($all == 1 or (done | not) or any(.runs[]; .rows | length > 0)))) as $shown
        | {run: $r, supervisor: $R[$r].supervisor, supervisor_gone: $R[$r].supervisor_gone,
           parent: ($p[$r] // null), hidden: (($all_rows | length) - ($shown | length)), rows: $shown}
        + (if $R[$r].supervisor_gone then {next: "from the pane that supervises it now: \($mail) run --name \($r)"} else {} end);
      node($root; [$root])'
}

# shellcheck source-path=SCRIPTDIR
# shellcheck source=pause.sh
source "$HERE/pause.sh"
case "$CMD" in
  wait|read|ps|show|list) ;;
  *) mkdir -p "$STATE"; exec 8>"$STATE/control.lock"; flock -x 8; export FLEET_CONTROL_LOCK_PATH="$STATE/control.lock" ;;
esac
fleet_pause_admission "$@"

case "$CMD" in
  pause|pause-status|unpause|pause-release)
    bind_run
    fleet_pause_command "$@"
    ;;
  run)
    NAME= WAKE= LABEL= LABEL_SET= LINK= TAKE=0
    while [ $# -gt 0 ]; do case "$1" in --name) NAME=$2; shift 2 ;; --wake) WAKE=$2; shift 2 ;; --label) LABEL=$2 LABEL_SET=1; shift 2 ;; --link) LINK=$2; shift 2 ;; --take-over) TAKE=1; shift ;; *) die "run: unknown option $1" ;; esac; done
    check_link "$LINK"
    herdr agent deliveries >/dev/null 2>&1 || die "this Herdr server has no delivery queue: fleet needs the Herdr build with orchestration support"
    case ",$WAKE," in *[!a-z_,]*) die "run: --wake takes a comma-separated list of mail types" ;; esac
    RUN=${NAME:-run-$(date +%Y%m%d-%H%M%S)}
    RUN=$(printf '%s' "$RUN" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_.-]+/-/g')
    RUN_DIR="$RUNS/$RUN"; LEDGER="$RUN_DIR/ledger.json"; INBOX="$RUN_DIR/inbox.jsonl"
    OLD=$(supervisor); MOVED=
    if [ -n "${HERDR_PANE_ID:-}" ] && [ "$OLD" != "$HERDR_PANE_ID" ]; then
      [ -z "$OLD" ] || [ -n "$(gone_supervisor)" ] || [ "$TAKE" = 1 ] \
        || die "run '$RUN' is supervised by pane $OLD, which is still open. Pass --take-over to move it to this pane"
      MOVED=1
    fi
    mkdir -p "$RUN_DIR/workers" "$STATE"
    [ -f "$RUN_DIR/ledger.json" ] || echo '[]' >"$RUN_DIR/ledger.json"
    touch "$RUN_DIR/inbox.jsonl"
    echo "$RUN" >"$POINTER"
    [ -z "$WAKE" ] || echo "$WAKE" >"$RUN_DIR/wake-types"
    if [ -n "${HERDR_PANE_ID:-}" ]; then
      echo "$HERDR_PANE_ID" >"$RUN_DIR/supervisor"
      # A run named for an issue labels its supervisor with the key, unless --label says otherwise.
      [ -n "$LABEL_SET" ] || LABEL=$(ticket_of "$RUN")
      [ -z "$LABEL$LABEL_SET" ] || label_pane "$HERDR_PANE_ID" "$LABEL"
      link_pane "$HERDR_PANE_ID" "$LINK"
    fi
    LINKED=0; [ -z "$MOVED" ] || LINKED=$(link_workers)
    [ -z "$MOVED" ] || [ -z "$OLD" ] || echo "fleet.sh: run '$RUN' moved from pane $OLD to this pane, with $LINKED live workers. Run ps: mail sent meanwhile woke no one" >&2
    # A run bound from a dispatched worker's pane is a child of that worker's attempt, for ps --tree.
    PARENT=
    if [ -n "${HERDR_PANE_ID:-}" ] && { [ -n "$MOVED" ] || [ ! -f "$RUN_DIR/parent" ]; }; then PARENT=$(record_parent); fi
    jq -cn --arg run "$RUN" --arg dir "$RUN_DIR" --arg old "${MOVED:+$OLD}" --argjson n "$LINKED" --arg parent "$PARENT" \
      '{run: $run, dir: $dir, inbox: ($dir + "/inbox.jsonl"), ledger: ($dir + "/ledger.json")}
       + (if $old == "" then {} else {moved_from: $old, linked: $n} end)
       + (if $parent == "" then {} else {parent: ($parent | fromjson)} end)'
    ;;

  start)
    bind_run
    REPO= BRANCH= NAME= KIND= TASK= BASE= LABEL= LABEL_SET= LINK=; AFTER=(); AARGS=()
    while [ $# -gt 0 ]; do
      case "$1" in
        --repo) REPO=$2; shift 2 ;; --branch) BRANCH=$2; shift 2 ;; --name) NAME=$2; shift 2 ;;
        --kind) KIND=$2; shift 2 ;; --task) TASK=$2; shift 2 ;; --base) BASE=$2; shift 2 ;;
        --after) AFTER+=("$2"); shift 2 ;; --label) LABEL=$2 LABEL_SET=1; shift 2 ;;
        --link) LINK=$2; shift 2 ;;
        --) shift; AARGS=("$@"); break ;;
        *) die "start: unknown option $1" ;;
      esac
    done
    [ -n "$REPO" ] && [ -n "$BRANCH" ] && [ -n "$NAME" ] && [ -r "$TASK" ] || die "start needs --repo, --branch, --name and a readable --task"
    TASK=$(realpath "$TASK")
    check_link "$LINK"
    # The issue key of a name goes to the label, and the name keeps the description. A name whose
    # rest is not a usable agent name stays whole.
    if [ -z "$LABEL_SET" ] && [ -n "$(ticket_of "$NAME")" ]; then
      LABEL=$(ticket_of "$NAME"); REST=$(printf '%s' "$NAME" | sed -E 's/^[A-Za-z]{2,6}-[0-9]{1,5}-?//')
      case "$REST" in
        [A-Za-z]*) echo "fleet.sh: note: $LABEL of $NAME is the label and the rest is the agent name; use the agent name in the receipt for later commands, or pass --label \"\" to keep a name whole" >&2; NAME=$REST ;;
      esac
    fi
    for A in "${AFTER[@]+"${AFTER[@]}"}"; do [ -n "$(entry "$A")" ] || die "--after $A: no such dispatch in run $RUN"; done
    D=$(new_id)
    AARGS_JSON=$(printf '%s\n' "${AARGS[@]+"${AARGS[@]}"}" | jq -R . | jq -sc 'map(select(length > 0))')
    SPEC=$(jq -cn --arg repo "$REPO" --arg branch "$BRANCH" --arg name "$NAME" --arg kind "$KIND" --arg base "$BASE" \
      --arg task "$TASK" --argjson aargs "$AARGS_JSON" --arg label "$LABEL" --arg link "$LINK" \
      '{repo: $repo, branch: $branch, name: $name, kind: $kind, base: $base, task: $task, agent_args: $aargs, label: $label, link: $link}')
    AFTER_JSON=$(printf '%s\n' "${AFTER[@]+"${AFTER[@]}"}" | jq -R . | jq -sc 'map(select(length > 0))')
    ledger_mod --arg d "$D" --arg ts "$(now)" --argjson spec "$SPEC" --argjson after "$AFTER_JSON" \
      '. + [{dispatch: $d, attempt: 1, supersedes: null, after: $after, status: "pending", outcome: null, decision: null,
             created: $ts, started: null, settled: null, agent: null, launch: $spec}]'
    if deps_met "$D"; then launch_dispatch "$D"
    else jq -cn --arg d "$D" --argjson after "$AFTER_JSON" '{dispatch: $d, status: "pending", after: $after}'; fi
    ;;

  wait)
    bind_run
    TYPES=status,question,escalation,worker_done TIMEOUT=0
    while [ $# -gt 0 ]; do case "$1" in --types) TYPES=$2; shift 2 ;; --timeout) TIMEOUT=$2; shift 2 ;; *) die "wait: unknown option $1" ;; esac; done
    "$MAIL" wait --inbox "$INBOX" --types "$TYPES" --timeout "$TIMEOUT"
    ;;

  read)
    bind_run
    ID=; while [ $# -gt 0 ]; do case "$1" in --id) ID=$2; shift 2 ;; *) die "read: unknown option $1" ;; esac; done
    [ -n "$ID" ] || die "read needs --id"
    "$MAIL" read --inbox "$INBOX" --id "$ID"
    ;;

  ack)
    bind_run
    ID= DECISION= REASON= UNTIL=
    while [ $# -gt 0 ]; do case "$1" in --id) ID=$2; shift 2 ;; --decision) DECISION=$2; shift 2 ;; --reason) REASON=$2; shift 2 ;; --until) UNTIL=$2; shift 2 ;; *) die "ack: unknown option $1" ;; esac; done
    [ -n "$ID" ] || die "ack needs --id"
    MSG=$("$MAIL" read --inbox "$INBOX" --id "$ID"); [ -n "$MSG" ] || die "no message $ID in run $RUN"
    TYPE=$(jq -r .type <<<"$MSG"); FROM=$(jq -r .from <<<"$MSG"); D= OUTCOME=
    [ "$TYPE" = worker_done ] || [ -z "$REASON$UNTIL" ] || die "--reason and --until go with the decision on a worker_done"
    if [ "$TYPE" = worker_done ]; then
      case "$DECISION" in reuse|retain|release) ;; *) die "acking a worker_done requires --decision reuse|retain|release" ;; esac
      # A kept worker is a cost until it is released, so the ledger says why and until when.
      if [ "$DECISION" != release ]; then
        [ -n "$REASON" ] && [ -n "$UNTIL" ] || die "--decision $DECISION needs --reason <why> and --until <release condition>"
      else
        [ -z "$UNTIL" ] || die "--until is the release condition of a retain or reuse"
      fi
      OUTCOME=$(jq -r '.outcome // "unspecified"' <<<"$MSG")
      # A re-run after a crash finds the entry this message already settled and finishes its steps.
      # An entry that an older version settled has no settled_by: it is the agent's settled release
      # that is not released and was settled after the message was sent.
      SETTLED=$(jq -r --arg id "$ID" --arg a "$FROM" --arg t "$(jq -r .ts <<<"$MSG")" '
        ([.[] | select(.settled_by == $id)] | last | .dispatch)
        // ([.[] | select(.agent == $a and .status == "settled" and .settled_by == null and .decision == "release"
                          and .released == null and (.settled // "") >= $t)] | last | .dispatch) // empty' "$LEDGER")
      if [ -n "$SETTLED" ]; then
        D=$SETTLED; E=$(entry "$D")
        PREV=$(jq -r .decision <<<"$E"); OUTCOME=$(jq -r '.outcome // "unspecified"' <<<"$E")
        [ "$PREV" = "$DECISION" ] || die "$ID already settled $D with --decision $PREV; re-run with that decision"
        REASON=$(jq -r '.decision_reason // ""' <<<"$E"); UNTIL=$(jq -r '.release_when // ""' <<<"$E")
        [ "$DECISION" != release ] || release_worker "$FROM" "$D"
      else
        D=$(jq -r --arg a "$FROM" '[.[] | select(.agent == $a and .status == "working")] | last | .dispatch // empty' "$LEDGER")
      fi
      if [ -n "$D" ] && [ -z "$SETTLED" ]; then
        record_session "$D"
        ledger_mod --arg d "$D" --arg o "$OUTCOME" --arg dec "$DECISION" --arg ts "$(now)" --arg why "$REASON" --arg until "$UNTIL" --arg id "$ID" \
          'map(if .dispatch == $d then .status = "settled" | .outcome = $o | .decision = $dec | .settled = $ts | .settled_by = $id
               | .decision_reason = (if $why == "" then null else $why end)
               | .release_when = (if $until == "" then null else $until end) else . end)'
        [ "$DECISION" != release ] || release_worker "$FROM" "$D"
      elif [ -z "$D" ]; then
        echo "fleet.sh: warning: no working dispatch for agent $FROM; acked without settlement" >&2
      fi
    fi
    "$MAIL" ack --inbox "$INBOX" --id "$ID" >/dev/null
    [ "$TYPE" != worker_done ] || [ -z "$D" ] || launch_ready
    jq -cn --arg id "$ID" --arg t "$TYPE" --arg d "$D" --arg o "$OUTCOME" --arg dec "$DECISION" \
      --arg why "$REASON" --arg until "$UNTIL" \
      '{acked: $id, type: $t} + (if $d == "" then {} else {dispatch: $d, outcome: $o, decision: $dec}
         + (if $why == "" then {} else {reason: $why} end) + (if $until == "" then {} else {until: $until} end) end)'
    ;;

  reply)
    bind_run
    ID= BODY= BODY_FILE=
    while [ $# -gt 0 ]; do case "$1" in --id) ID=$2; shift 2 ;; --body) BODY=$2; shift 2 ;; --body-file) BODY_FILE=$2; shift 2 ;; *) die "reply: unknown option $1" ;; esac; done
    [ -n "$ID" ] && { [ -n "$BODY" ] || [ -n "$BODY_FILE" ]; } || die "reply needs --id and --body or --body-file"
    Q=$("$MAIL" read --inbox "$INBOX" --id "$ID"); [ -n "$Q" ] || die "no message $ID in run $RUN"
    AGENT=$(jq -r .from <<<"$Q"); SUBJ=$(jq -r .subject <<<"$Q")
    WI="$RUN_DIR/workers/$AGENT/inbox.jsonl"
    "$MAIL" send --inbox "$WI" --from supervisor --type reply --reply-to "$ID" --subject "re: $SUBJ" \
      ${BODY_FILE:+--body-file "$BODY_FILE"} ${BODY:+--body "$BODY"}
    "$MAIL" ack --inbox "$INBOX" --id "$ID" >/dev/null
    nudge "$AGENT" "Your supervisor answered your question. Read it: $MAIL read --inbox $WI --unacked"
    ;;

  send)
    bind_run
    AGENT= SUBJ= BODY= BODY_FILE=
    while [ $# -gt 0 ]; do case "$1" in --agent) AGENT=$2; shift 2 ;; --subject) SUBJ=$2; shift 2 ;; --body) BODY=$2; shift 2 ;; --body-file) BODY_FILE=$2; shift 2 ;; *) die "send: unknown option $1" ;; esac; done
    [ -n "$AGENT" ] && [ -n "$SUBJ" ] && { [ -n "$BODY" ] || [ -n "$BODY_FILE" ]; } || die "send needs --agent, --subject and --body or --body-file"
    [ -n "$(jq -r --arg a "$AGENT" '.[] | select(.agent == $a) | .dispatch' "$LEDGER")" ] \
      || die "send: no agent named $AGENT in run $RUN (agents: $(jq -r '[.[].agent // empty] | unique | join(", ")' "$LEDGER"))"
    WI="$RUN_DIR/workers/$AGENT/inbox.jsonl"
    "$MAIL" send --inbox "$WI" --from supervisor --type followup --subject "$SUBJ" ${BODY_FILE:+--body-file "$BODY_FILE"} ${BODY:+--body "$BODY"}
    # A worker that gets new work is watched again.
    supervise "$AGENT" "$REPORTS"
    nudge "$AGENT" "Your supervisor sent a follow-up. Read it: $MAIL read --inbox $WI --unacked"
    ;;

  ps)
    bind_run
    JSON=0 TREE=0 ALL=0
    while [ $# -gt 0 ]; do case "$1" in --json) JSON=1; shift ;; --tree) TREE=1; shift ;; --all) ALL=1; shift ;; *) die "ps: unknown option $1" ;; esac; done
    [ "$TREE" = 1 ] || [ "$ALL" = 0 ] || die "ps: --all goes with --tree"
    if [ "$TREE" = 1 ]; then
      T=$(ps_tree "$ALL")
      if [ "$JSON" = 1 ]; then jq . <<<"$T"; exit 0; fi
      # One line per run and per attempt, indented by depth. A run line says where it is supervised from.
      { printf 'TREE\tDISPATCH\tSTATUS\tOUTCOME\tLIVE\tATTENTION\tNEXT\n'
        jq -r 'def pad($d): [range($d)] | map("  ") | add // "";
          def lines($d): [pad($d) + "run " + .run, "", "", "", "", "",
              ([if .parent != null and $d == 0 then "under \(.parent.run) \(.parent.dispatch)" else empty end,
                if .supervisor_gone then "supervisor pane \(.supervisor) is gone; \(.next)"
                elif (.supervisor // "") == "" then "no supervisor pane" else "supervised from \(.supervisor)" end,
                if .hidden > 0 then "\(.hidden) done hidden (--all shows them)" else empty end] | join("; "))],
            (.rows[] | [pad($d + 1) + .agent, .dispatch, .status, .outcome, .liveness, .attention, .next],
                       (.runs[] | lines($d + 2)));
          lines(0) | @tsv' <<<"$T"; } | column -t -s $'\t'
      exit 0
    fi
    GONE=$(gone_supervisor)
    [ -z "$GONE" ] || echo "fleet.sh: warning: the supervisor pane $GONE of run '$RUN' is gone, so wakes reach no one. From the pane that supervises it now, run: $SELF run --name $RUN" >&2
    ROWS=$(ps_rows "")
    if [ "$JSON" = 1 ]; then jq -s . <<<"$ROWS"; else
      { printf 'DISPATCH\tAGENT\tSTATUS\tOUTCOME\tLIVE\tATTENTION\tNEXT\n'
        jq -r '[.dispatch, .agent, .status, .outcome, .liveness, .attention, .next] | @tsv' <<<"$ROWS"; } | column -t -s $'\t'
    fi
    ;;

  retry)
    bind_run
    D= FORCE=0; while [ $# -gt 0 ]; do case "$1" in --dispatch) D=$2; shift 2 ;; --force) FORCE=1; shift ;; *) die "retry: unknown option $1" ;; esac; done
    OLD=$(entry "$D"); [ -n "$OLD" ] || die "no dispatch $D in run $RUN"
    check_attempts "$D" "$FORCE"
    N=$(new_id)
    NEW=$(jq -c --arg n "$N" --arg ts "$(now)" '{dispatch: $n, attempt: (.attempt + 1), supersedes: .dispatch, after: .after,
      status: "pending", outcome: null, decision: null, created: $ts, started: null, settled: null, agent: null, launch: .launch}' <<<"$OLD")
    # The attempt that is replaced sends no more reports.
    OLD_AGENT=$(jq -r '.agent // empty' <<<"$OLD"); [ -z "$OLD_AGENT" ] || supervise "$OLD_AGENT"
    record_session "$D"
    ledger_mod --arg d "$D" --argjson n "$NEW" 'map(if .dispatch == $d and .status != "settled" then .status = "abandoned" else . end) + [$n]'
    if deps_met "$N"; then launch_dispatch "$N"; else jq -cn --arg d "$N" '{dispatch: $d, status: "pending"}'; fi
    ;;

  resume)
    bind_run
    D= FORCE=0 DRY=0
    while [ $# -gt 0 ]; do case "$1" in --dispatch) D=$2; shift 2 ;; --force) FORCE=1; shift ;; --dry-run) DRY=1; shift ;; *) die "resume: unknown option $1" ;; esac; done
    OLD=$(entry "$D"); [ -n "$OLD" ] || die "no dispatch $D in run $RUN"
    RETRY="start the task again from its prompt instead: $SELF retry --dispatch $D"
    AGENT=$(jq -r '.agent // empty' <<<"$OLD"); [ -n "$AGENT" ] || die "resume: $D never started an agent; $RETRY"
    SESSION=$(jq -c '.session // empty | select(.value != null and .value != "")' <<<"$OLD")
    [ -n "$SESSION" ] || die "resume: $D has no recorded session; $RETRY"
    KIND=$(jq -r .agent <<<"$SESSION")
    WT=$(jq -r '.worktree // empty' <<<"$OLD")
    [ -n "$WT" ] && [ -d "$WT" ] || die "resume: the worktree of $D is gone (${WT:-none recorded}); $RETRY"
    NEXT=$(jq -r --arg d "$D" '[.[] | select(.supersedes == $d and (.status == "pending" or .status == "working")) | .dispatch] | first // empty' "$LEDGER")
    [ -z "$NEXT" ] || die "resume: $D already has a newer attempt $NEXT; resume or stop that one"
    ! herdr agent get "$AGENT" >/dev/null 2>&1 \
      || die "resume: agent $AGENT is still running; send it a follow-up with $SELF send, or resume once it has exited"
    check_attempts "$D" "$FORCE"
    rc=0; ERR=$("$HERE/herdr-fleet.sh" --check-session "$SESSION" 2>&1) || rc=$?
    ERR=${ERR#herdr-fleet: }
    case "$rc" in 0) ;; 1) die "resume: $ERR; $RETRY" ;; *) die "resume: $ERR" ;; esac
    N=$(new_id)
    NOTICE="$RUN_DIR/workers/$AGENT/resume-$N.md"
    TASKREF=$(jq -r '.resume.task // .prompt_file // .launch.task // ""' <<<"$OLD")
    RES=$(jq -cn --arg from "$D" --arg agent "$AGENT" --arg kind "$KIND" --argjson session "$SESSION" --arg wt "$WT" \
      --arg pane "$(jq -r '.pane_id // ""' <<<"$OLD")" --arg notice "$NOTICE" --arg task "$TASKREF" \
      --argjson a "$(jq -c '.launch.agent_args // []' <<<"$OLD")" \
      '{from: $from, agent: $agent, kind: $kind, session: $session, worktree: $wt, pane: $pane, notice: $notice, task: $task, agent_args: $a}')
    if [ "$DRY" = 1 ]; then
      jq -c --arg n "$(task_attempts "$D" | grep -c .)" '. + {attempts: ($n | tonumber)}' <<<"$RES"; exit 0
    fi
    mkdir -p "$(dirname "$NOTICE")"
    cat >"$NOTICE" <<EOF
# Resumed after an interruption

Your run of this task was interrupted. This is the same conversation, resumed in the same worktree: $WT

Your last tool call may have run in full, in part or not at all. Before you continue:
1. Run \`git status\` and \`git log --oneline -5\` in the worktree, and compare them with what you were doing.
2. Check whether your last command or edit finished. Finish or repair it if it did not.
3. Read your inbox for messages you have not acked (see below).

Then carry on with the task${TASKREF:+. Its full text is in $TASKREF}. Your reporting rules and inbox are the same as before.
When you are not sure a report went out, send it again with the same --key; it is not delivered twice.
EOF
    NEW=$(jq -c --arg n "$N" --arg ts "$(now)" --arg kind "$KIND" --argjson res "$RES" '{dispatch: $n, attempt: (.attempt + 1),
      supersedes: .dispatch, after: .after, status: "pending", outcome: null, decision: null, created: $ts, started: null,
      settled: null, agent: null, launch: (.launch + {kind: $kind}), resume: $res}' <<<"$OLD")
    ledger_mod --arg d "$D" --argjson n "$NEW" 'map(if .dispatch == $d and .status != "settled" then .status = "abandoned" else . end) + [$n]'
    launch_dispatch "$N"
    ;;

  release)
    bind_run
    D=; while [ $# -gt 0 ]; do case "$1" in --dispatch) D=$2; shift 2 ;; *) die "release: unknown option $1" ;; esac; done
    E=$(entry "$D"); [ -n "$E" ] || die "no dispatch $D in run $RUN"
    jq -e '.status == "settled" and (.decision == "retain" or .decision == "reuse")' <<<"$E" >/dev/null \
      || die "release: $D is not kept (status $(jq -r .status <<<"$E"), decision $(jq -r '.decision // "none"' <<<"$E")); ack its worker_done again to finish a release decision"
    if [ "$(jq -r '.released // empty' <<<"$E")" = "" ]; then
      REP=$(jq -r --argjson e "$E" "$REPLACED_JQ"' replaced_by($e) // empty' "$LEDGER")
      [ -z "$REP" ] || die "release: $D was replaced by $REP, which holds its agent and worktree now; release that attempt"
      AGENT=$(jq -r '.agent // empty' <<<"$E"); WT=$(jq -r '.worktree // empty' <<<"$E")
      # Release finds the workspace by the agent's name, so the name must still be this attempt's.
      if [ -n "$WT" ] && [ -d "$WT" ]; then
        P=$(herdr agent get "$AGENT" 2>/dev/null | jq -r '.result.agent.pane_id // empty' || true)
        [ -z "$P" ] || [ "$P" = "$(jq -r '.pane_id // empty' <<<"$E")" ] \
          || die "release: agent $AGENT now runs on pane $P, not on the pane of $D; remove $WT by hand"
      fi
      release_worker "$AGENT" "$D"
    fi
    entry "$D" | jq -c '{dispatch, decision, released} + (if .release_error then {release_error} else {} end)'
    ;;

  stop|abandon)
    bind_run
    D=; while [ $# -gt 0 ]; do case "$1" in --dispatch) D=$2; shift 2 ;; *) die "$CMD: unknown option $1" ;; esac; done
    E=$(entry "$D"); [ -n "$E" ] || die "no dispatch $D in run $RUN"
    AGENT=$(jq -r '.agent // empty' <<<"$E")
    # Stop removes the worker with its worktree workspace. A worker that an older version placed
    # as a tab has none of its own, so it cannot be stopped from here.
    if [ "$CMD" = stop ] && [ "$(jq -r '.placement // "workspace"' <<<"$E")" = tab ]; then
      die "stop: $AGENT was placed as a tab by an older version. Close its tab by hand, then run: $SELF abandon --dispatch $D"
    fi
    NEWST=stopped; [ "$CMD" = stop ] || NEWST=abandoned
    record_session "$D"
    # A re-run of stop keeps the first stop time and finishes the release.
    ledger_mod --arg d "$D" --arg s "$NEWST" --arg ts "$(now)" \
      'map(if .dispatch == $d then .settled = (if .status == $s then .settled // $ts else $ts end) | .status = $s else . end)'
    # An attempt that is closed sends no more reports.
    [ -z "$AGENT" ] || supervise "$AGENT"
    [ "$CMD" != stop ] || [ -z "$AGENT" ] || release_worker "$AGENT" "$D"
    jq -cn --arg d "$D" --arg s "$NEWST" '{dispatch: $d, status: $s}'
    ;;

  show)
    bind_run
    D=; while [ $# -gt 0 ]; do case "$1" in --dispatch) D=$2; shift 2 ;; *) die "show: unknown option $1" ;; esac; done
    E=$(entry "$D"); [ -n "$E" ] || die "no dispatch $D in run $RUN"; jq . <<<"$E"
    ;;
  list) bind_run; jq . "$LEDGER" ;;

  notify)
    LINE= TITLE= TO=
    while [ $# -gt 0 ]; do case "$1" in --line) LINE=$2; shift 2 ;; --title) TITLE=$2; shift 2 ;; --to) TO=$2; shift 2 ;; *) die "notify: unknown option $1" ;; esac; done
    [ -n "$LINE" ] || die "notify needs --line"
    [ -n "$TO" ] || { bind_run; TO=$(supervisor); }
    LINE=$(printf '%s' "$LINE" | tr '[:cntrl:]' ' ' | cut -c1-400)
    deliver "$TO" "$LINE" "${TITLE:-fleet: wake held}" "$LINE" || exit 3
    ;;

  adopt)
    # Runs started before Herdr kept the links: link each live worker, and label it from its name.
    N=0
    for LEDGER in "$RUNS"/*/ledger.json; do
      [ -f "$LEDGER" ] || continue
      RUN_DIR=$(dirname "$LEDGER"); RUN=$(basename "$RUN_DIR")
      N=$((N + $(link_workers)))
    done
    jq -cn --argjson n "$N" '{adopted: $n}'
    ;;

  report)
    bind_run
    FROM= TYPE= SUBJ= BODY= BODY_FILE= OUTCOME= KEY=
    while [ $# -gt 0 ]; do
      case "$1" in
        --from) FROM=$2; shift 2 ;; --type) TYPE=$2; shift 2 ;; --subject) SUBJ=$2; shift 2 ;; --key) KEY=$2; shift 2 ;;
        --body) BODY=$2; shift 2 ;; --body-file) BODY_FILE=$2; shift 2 ;; --outcome) OUTCOME=$2; shift 2 ;;
        *) die "report: unknown option $1" ;;
      esac
    done
    case "$TYPE" in status|question|escalation|worker_done) ;; *) die "report: --type must be status|question|escalation|worker_done" ;; esac
    # The name goes into a line typed into the supervisor's prompt.
    case "$FROM" in ""|*[!A-Za-z0-9_.-]*) die "report: --from must be an agent name ([A-Za-z0-9_.-])" ;; esac
    # A key repeats within the agent's current attempt, so a resumed or retried attempt can report again.
    SINCE=$(jq -r --arg a "$FROM" '[.[] | select(.agent == $a)] | last | .created // empty' "$LEDGER")
    SENT=$("$MAIL" send --inbox "$INBOX" --from "$FROM" --type "$TYPE" --subject "$SUBJ" \
      ${BODY_FILE:+--body-file "$BODY_FILE"} ${BODY:+--body "$BODY"} ${OUTCOME:+--outcome "$OUTCOME"} \
      ${KEY:+--key "$KEY"} ${SINCE:+--since "$SINCE"})
    echo "$SENT"
    ID=$(jq -r .sent <<<"$SENT")
    if [ "$(jq -r '.duplicate // false' <<<"$SENT")" = true ]; then
      # An acked repeat is handled. An un-acked one may have lost its wake, so it wakes again for the first message.
      [ "$(jq -r '.acked // false' <<<"$SENT")" = false ] || exit 0
      FIRST=$("$MAIL" read --inbox "$INBOX" --id "$ID")
      TYPE=$(jq -r .type <<<"$FIRST"); SUBJ=$(jq -r .subject <<<"$FIRST")
    fi
    # A worker that is done stays linked for the tree and sends no more reports.
    if [ "$TYPE" = worker_done ] && ! fleet_pause_active "$FROM"; then supervise "$FROM"; fi
    case ",$(cat "$RUN_DIR/wake-types" 2>/dev/null || echo question,escalation,worker_done)," in *",$TYPE,"*)
      SHORT=$(printf '%s' "$SUBJ" | tr '|' ' ' | tr '[:cntrl:]' ' ' | cut -c1-160 | iconv -c -f UTF-8 -t UTF-8 2>/dev/null || true)
      notify_supervisor "MAIL|$ID|$TYPE|$FROM|$SHORT - handle it: $SELF read --run $RUN --id $ID" \
        "fleet: $TYPE from $FROM" "$SHORT" ;;
    esac
    ;;
  *) die "unknown command $CMD" ;;
esac
