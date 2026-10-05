#!/usr/bin/env bash
# Supervisor front-end for Herdr dispatch: a per-run ledger of dispatch attempts, an ack-based
# mailbox, off-pane replies, dependency launches, and a fleet view with liveness + next action.
#
#   dispatch.sh run     [--name <slug>]                         bind (create or resume) a run; later commands default to it
#   dispatch.sh start   --repo <path> --branch <name> --name <agent> --task <file>
#                       [--kind <herdr kind>] [--base <ref>] [--tab] [--after <dispatch>]... [-- <agent args>]
#                       --tab: new tab in the caller's workspace instead of a workspace of its own
#   dispatch.sh wait    [--types t,t] [--timeout <s>]           block until an un-acked message of a wanted type; prints MAIL| lines
#   dispatch.sh read    --id <msg>                               full message JSON
#   dispatch.sh ack     --id <msg> [--decision reuse|retain|release]   settle; worker_done requires --decision
#   dispatch.sh reply   --id <question> (--body <text> | --body-file <f>)   answer into the worker's inbox
#   dispatch.sh send    --agent <name> --subject <s> (--body <text> | --body-file <f>)   follow-up into the worker's inbox
#   dispatch.sh ps      [--json]                                 fleet: status, liveness, attention, next action
#   dispatch.sh retry   --dispatch <d>                           new attempt of the same task; old attempt abandoned
#   dispatch.sh stop    --dispatch <d>                           kill the worker (removes its worktree) and mark stopped
#   dispatch.sh abandon --dispatch <d>                           mark abandoned; worker left running for inspection
#   dispatch.sh show    --dispatch <d> | list
#   dispatch.sh report  --from <agent> --type <t> --subject <s> [--body <text> | --body-file <f>] [--outcome <o>]
#                       worker side: mail the supervisor, and wake it for question, escalation and worker_done
#
# Run selection: --run <id> anywhere, else $DISPATCH_RUN, else the run last bound from this pane.
# Ledger: <state>/agent-dispatch/runs/<run>/ledger.json. Mailbox: <run>/inbox.jsonl (worker -> supervisor),
# <run>/workers/<agent>/inbox.jsonl (supervisor -> worker).
# Pane index: <state>/agent-dispatch/panes.json, pane id -> {run, role, agent, supervisor, pending}.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
MAIL="$HERE/mail.sh"
STATE="${XDG_STATE_HOME:-$HOME/.local/state}/agent-dispatch"
RUNS="$STATE/runs"
INDEX="$STATE/panes.json"
POINTER="$STATE/current-run${HERDR_PANE_ID:+.${HERDR_PANE_ID//:/_}}"

die() { echo "dispatch.sh: $*" >&2; exit 1; }
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
new_id() { echo "d_$(date +%Y%m%d%H%M%S)_$RANDOM"; }
for bin in jq herdr flock; do command -v "$bin" >/dev/null || die "$bin not on PATH"; done

CMD=${1:-}; shift || true
[ -n "$CMD" ] || { sed -n '2,20p' "$0" >&2; exit 2; }

# --run may appear anywhere; strip it before per-command parsing.
RUN=${DISPATCH_RUN:-}
ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in --run) RUN=$2; shift 2 ;; *) ARGS+=("$1"); shift ;; esac
done
set -- "${ARGS[@]+"${ARGS[@]}"}"

bind_run() {
  [ -n "$RUN" ] || RUN=$(cat "$POINTER" 2>/dev/null || true)
  [ -n "$RUN" ] || die "no run bound: run 'dispatch.sh run' first, or pass --run <id>"
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

# Dependencies of a dispatch are met when every `after` is settled with outcome succeeded.
deps_met() {
  [ "$(jq -r --arg d "$1" '
    (map(select(.status == "settled" and .outcome == "succeeded") | .dispatch)) as $ok
    | .[] | select(.dispatch == $d) | ((.after // []) | all(. as $a | $ok | any(. == $a)))' "$LEDGER")" = true ]
}

# index_mod [jq args...] '<filter over the object>'  -- atomic read-modify-write of the pane index.
index_mod() {
  mkdir -p "$STATE"
  ( flock 9
    [ -s "$INDEX" ] || echo '{}' >"$INDEX"
    TMP=$(mktemp "$INDEX.XXXXXX"); jq "$@" "$INDEX" >"$TMP" && mv "$TMP" "$INDEX"
  ) 9>"$INDEX.lock"
}
index_worker() {  # <agent>: (re)index the pane that hosts a worker of this run
  local pane; pane=$(herdr agent get "$1" 2>/dev/null | jq -r '.result.agent.pane_id // empty' || true)
  [ -n "$pane" ] || return 0
  index_mod --arg p "$pane" --arg r "$RUN" --arg a "$1" --arg s "$(cat "$RUN_DIR/supervisor" 2>/dev/null || true)" \
    '.[$p] = {run: $r, role: "worker", agent: $a, supervisor: $s}'
}
unindex_worker() {  # <agent>
  index_mod --arg r "$RUN" --arg a "$1" 'with_entries(select((.value.role == "worker" and .value.run == $r and .value.agent == $a) | not))'
}
set_pending() {  # <supervisor pane> <true|false>
  index_mod --arg p "$1" --argjson v "$2" 'if .[$p] then .[$p].pending = $v else . end'
}

# Wake the supervisor with one prompt line. A focused pane may hold a half-typed draft and a
# blocked or unknown one may hold a dialog, so those get a toast or nothing, and a pending mark.
notify_supervisor() {  # <prompt line> <toast title> <toast body>
  local sup info
  sup=$(cat "$RUN_DIR/supervisor" 2>/dev/null || true)
  [ -n "$sup" ] || return 0
  info=$(herdr agent get "$sup" 2>/dev/null) || { set_pending "$sup" true; return 0; }
  if [ "$(jq -r '.result.agent.focused // false' <<<"$info")" = true ]; then
    herdr notification show "$2" --body "$3" --sound request >/dev/null 2>&1 || true
    set_pending "$sup" true; return 0
  fi
  case "$(jq -r '.result.agent.agent_status // "unknown"' <<<"$info")" in blocked|unknown) set_pending "$sup" true; return 0 ;; esac
  herdr agent prompt "$sup" "$1" >/dev/null 2>&1 || set_pending "$sup" true
}

nudge() {
  local err code
  err=$(herdr agent prompt "$1" "$2" 2>&1 >/dev/null) && return 0
  code=$(jq -r '.error.code // "unknown"' <<<"$err" 2>/dev/null || echo unknown)
  if [ "$code" = agent_blocked ]; then
    echo "dispatch.sh: $1 is parked at a dialog and will not read its inbox until it is answered; the message is in its inbox" >&2
  else
    echo "dispatch.sh: note: could not nudge $1 ($code); the message is in its inbox" >&2
  fi
}

launch_dispatch() {
  local D=$1 spec
  spec=$(entry "$D" | jq -c .launch)
  [ -n "$spec" ] && [ "$spec" != null ] || { echo "dispatch.sh: no launch spec for $D" >&2; return 1; }
  local args=(--repo "$(jq -r .repo <<<"$spec")" --branch "$(jq -r .branch <<<"$spec")" \
              --name "$(jq -r .name <<<"$spec")" \
              --prompt-file "$(jq -r .task <<<"$spec")" --run-dir "$RUN_DIR")
  # No kind in the spec means inherit the caller's: the launcher resolves it.
  local kind; kind=$(jq -r '.kind // ""' <<<"$spec"); [ -z "$kind" ] || args+=(--kind "$kind")
  local base; base=$(jq -r '.base // ""' <<<"$spec"); [ -z "$base" ] || args+=(--base "$base")
  [ "$(jq -r '.tab // false' <<<"$spec")" != true ] || args+=(--tab)
  local aargs=(); while IFS= read -r x; do aargs+=("$x"); done < <(jq -r '.agent_args[]?' <<<"$spec")
  [ ${#aargs[@]} -eq 0 ] || args+=(-- "${aargs[@]}")

  local receipt rc=0 t0; t0=$(now)
  receipt=$("$HERE/herdr-dispatch.sh" "${args[@]}") || rc=$?
  if [ -z "$receipt" ]; then
    ledger_mod --arg d "$D" --arg ts "$(now)" 'map(if .dispatch == $d then .status = "failed_start" | .started = $ts else . end)'
    echo "dispatch.sh: launcher failed for $D (see stderr above)" >&2; return 1
  fi
  local st ls; st=$(jq -r .status <<<"$receipt")
  case "$st" in working|idle|done|blocked|unknown) ls=working ;; *) ls=failed_start ;; esac
  ledger_mod --arg d "$D" --arg ts "$(now)" --arg ls "$ls" --argjson r "$receipt" \
    'map(if .dispatch == $d then . + {launch: (.launch + {kind: $r.kind}),
          status: $ls, started: $ts, agent: $r.agent,
          placement: ($r.placement // "workspace"), tab_id: $r.tab_id,
          workspace_id: $r.workspace_id, pane_id: $r.pane_id, worktree: $r.worktree, branch: $r.branch,
          prompt_file: $r.task_file, worker_inbox: $r.worker_inbox, agent_status: $r.status} else . end)'
  # A fast worker can report worker_done before the launcher returns; do not index it again.
  local ag; ag=$(jq -r .agent <<<"$receipt")
  if [ "$ls" = working ] && [ -z "$(jq -c --arg a "$ag" --arg t "$t0" 'select(.from == $a and .type == "worker_done" and .ts >= $t)' "$INBOX")" ]; then
    index_worker "$ag"
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

# Release = post-settlement cleanup: removes the worktree, which also kills a live agent.
release_worker() {
  local agent=$1 D=$2 ws out
  unindex_worker "$agent"
  if [ "$(entry "$D" | jq -r '.placement // "workspace"')" = tab ]; then release_tab_worker "$agent" "$D"; return 0; fi
  ws=$(herdr agent get "$agent" 2>/dev/null | jq -r '.result.agent.workspace_id // empty' || true)
  [ -n "$ws" ] || ws=$(entry "$D" | jq -r '.workspace_id // empty')
  [ -n "$ws" ] || { echo "dispatch.sh: warning: no workspace known for $agent; nothing removed" >&2; return 0; }
  if out=$(herdr worktree remove --workspace "$ws" 2>&1); then
    ledger_mod --arg d "$D" --arg ts "$(now)" 'map(if .dispatch == $d then .released = $ts else . end)'
  else
    echo "dispatch.sh: warning: release of $agent failed: $out" >&2
    echo "dispatch.sh: inspect the worktree, then: herdr worktree remove --workspace $ws --force" >&2
  fi
}

# Tab placement: herdr does not own the worktree, so check it is clean, close the tab (kills the
# agent), then remove the worktree with git. The branch stays, as with `herdr worktree remove`.
release_tab_worker() {
  local agent=$1 D=$2 E tab wt root out
  E=$(entry "$D")
  tab=$(herdr agent get "$agent" 2>/dev/null | jq -r '.result.agent.tab_id // empty' || true)
  [ -n "$tab" ] || tab=$(jq -r '.tab_id // empty' <<<"$E")
  wt=$(jq -r '.worktree // empty' <<<"$E")
  [ -n "$wt" ] || { echo "dispatch.sh: warning: no worktree known for $agent; nothing removed" >&2; return 0; }
  if [ -d "$wt" ] && [ -n "$(git -C "$wt" status --porcelain 2>/dev/null)" ]; then
    echo "dispatch.sh: warning: release of $agent refused: dirty_worktree_requires_force ($wt)" >&2
    echo "dispatch.sh: inspect the worktree, then: herdr tab close ${tab:-<tab>} && git -C $wt worktree remove --force $wt" >&2
    return 0
  fi
  [ -z "$tab" ] || herdr tab close "$tab" >/dev/null 2>&1 || echo "dispatch.sh: warning: could not close tab $tab" >&2
  if [ -d "$wt" ]; then
    root=$(dirname "$(git -C "$wt" rev-parse --path-format=absolute --git-common-dir)")
    if ! out=$(git -C "$root" worktree remove "$wt" 2>&1); then
      echo "dispatch.sh: warning: release of $agent failed: $out" >&2; return 0
    fi
  fi
  ledger_mod --arg d "$D" --arg ts "$(now)" 'map(if .dispatch == $d then .released = $ts else . end)'
}

case "$CMD" in
  run)
    NAME=
    while [ $# -gt 0 ]; do case "$1" in --name) NAME=$2; shift 2 ;; *) die "run: unknown option $1" ;; esac; done
    RUN=${NAME:-run-$(date +%Y%m%d-%H%M%S)}
    RUN=$(printf '%s' "$RUN" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_.-]+/-/g')
    RUN_DIR="$RUNS/$RUN"; mkdir -p "$RUN_DIR/workers" "$STATE"
    [ -f "$RUN_DIR/ledger.json" ] || echo '[]' >"$RUN_DIR/ledger.json"
    touch "$RUN_DIR/inbox.jsonl"
    echo "$RUN" >"$POINTER"
    if [ -n "${HERDR_PANE_ID:-}" ]; then
      echo "$HERDR_PANE_ID" >"$RUN_DIR/supervisor"
      index_mod --arg p "$HERDR_PANE_ID" --arg r "$RUN" '.[$p] = {run: $r, role: "supervisor", pending: false}'
    fi
    jq -cn --arg run "$RUN" --arg dir "$RUN_DIR" '{run: $run, dir: $dir, inbox: ($dir + "/inbox.jsonl"), ledger: ($dir + "/ledger.json")}'
    ;;

  start)
    bind_run
    REPO= BRANCH= NAME= KIND= TASK= BASE= TAB=false; AFTER=(); AARGS=()
    while [ $# -gt 0 ]; do
      case "$1" in
        --repo) REPO=$2; shift 2 ;; --branch) BRANCH=$2; shift 2 ;; --name) NAME=$2; shift 2 ;;
        --kind) KIND=$2; shift 2 ;; --task) TASK=$2; shift 2 ;; --base) BASE=$2; shift 2 ;;
        --after) AFTER+=("$2"); shift 2 ;; --tab) TAB=true; shift ;;
        --) shift; AARGS=("$@"); break ;;
        *) die "start: unknown option $1" ;;
      esac
    done
    [ -n "$REPO" ] && [ -n "$BRANCH" ] && [ -n "$NAME" ] && [ -r "$TASK" ] || die "start needs --repo, --branch, --name and a readable --task"
    TASK=$(realpath "$TASK")
    for A in "${AFTER[@]+"${AFTER[@]}"}"; do [ -n "$(entry "$A")" ] || die "--after $A: no such dispatch in run $RUN"; done
    D=$(new_id)
    AARGS_JSON=$(printf '%s\n' "${AARGS[@]+"${AARGS[@]}"}" | jq -R . | jq -sc 'map(select(length > 0))')
    SPEC=$(jq -cn --arg repo "$REPO" --arg branch "$BRANCH" --arg name "$NAME" --arg kind "$KIND" --arg base "$BASE" \
      --arg task "$TASK" --argjson aargs "$AARGS_JSON" --argjson tab "$TAB" \
      '{repo: $repo, branch: $branch, name: $name, kind: $kind, base: $base, task: $task, agent_args: $aargs, tab: $tab}')
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
    ID= DECISION=
    while [ $# -gt 0 ]; do case "$1" in --id) ID=$2; shift 2 ;; --decision) DECISION=$2; shift 2 ;; *) die "ack: unknown option $1" ;; esac; done
    [ -n "$ID" ] || die "ack needs --id"
    MSG=$("$MAIL" read --inbox "$INBOX" --id "$ID"); [ -n "$MSG" ] || die "no message $ID in run $RUN"
    TYPE=$(jq -r .type <<<"$MSG"); FROM=$(jq -r .from <<<"$MSG"); D= OUTCOME=
    if [ "$TYPE" = worker_done ]; then
      case "$DECISION" in reuse|retain|release) ;; *) die "acking a worker_done requires --decision reuse|retain|release" ;; esac
      OUTCOME=$(jq -r '.outcome // "unspecified"' <<<"$MSG")
      D=$(jq -r --arg a "$FROM" '[.[] | select(.agent == $a and .status == "working")] | last | .dispatch // empty' "$LEDGER")
      if [ -n "$D" ]; then
        ledger_mod --arg d "$D" --arg o "$OUTCOME" --arg dec "$DECISION" --arg ts "$(now)" \
          'map(if .dispatch == $d then .status = "settled" | .outcome = $o | .decision = $dec | .settled = $ts else . end)'
        [ "$DECISION" != release ] || release_worker "$FROM" "$D"
      else
        echo "dispatch.sh: warning: no working dispatch for agent $FROM; acked without settlement" >&2
      fi
    fi
    "$MAIL" ack --inbox "$INBOX" --id "$ID" >/dev/null
    [ "$TYPE" != worker_done ] || [ -z "$D" ] || launch_ready
    jq -cn --arg id "$ID" --arg t "$TYPE" --arg d "$D" --arg o "$OUTCOME" --arg dec "$DECISION" \
      '{acked: $id, type: $t} + (if $d == "" then {} else {dispatch: $d, outcome: $o, decision: $dec} end)'
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
    WI="$RUN_DIR/workers/$AGENT/inbox.jsonl"
    "$MAIL" send --inbox "$WI" --from supervisor --type followup --subject "$SUBJ" ${BODY_FILE:+--body-file "$BODY_FILE"} ${BODY:+--body "$BODY"}
    index_worker "$AGENT"
    nudge "$AGENT" "Your supervisor sent a follow-up. Read it: $MAIL read --inbox $WI --unacked"
    ;;

  ps)
    bind_run
    JSON=0; while [ $# -gt 0 ]; do case "$1" in --json) JSON=1; shift ;; *) die "ps: unknown option $1" ;; esac; done
    [ -z "${HERDR_PANE_ID:-}" ] || set_pending "$HERDR_PANE_ID" false
    UNACKED=$("$MAIL" read --inbox "$INBOX" --unacked | jq -sc .)
    AS='{}'
    for A in $(jq -r '.[] | select(.status == "working") | .agent // empty' "$LEDGER" | sort -u); do
      ST=$(herdr agent get "$A" 2>/dev/null | jq -r '.result.agent.agent_status // "missing"' || echo missing)
      AS=$(jq -c --arg a "$A" --arg s "$ST" '.[$a] = $s' <<<"$AS")
    done
    ROWS=$(jq -c --argjson un "$UNACKED" --argjson as "$AS" --arg mail "$0" '
      (map(select(.status == "settled" and .outcome == "succeeded") | .dispatch)) as $ok
      | (map(select(.status == "settled" and .outcome != "succeeded" or .status == "abandoned" or .status == "stopped" or .status == "failed_start") | .dispatch)) as $bad
      | .[]
      | . as $e
      | ($un | map(select(.from == $e.agent))) as $mine
      | (if $e.status == "pending" then "-"
         elif $e.status != "working" then $e.status
         else ($as[$e.agent] // "missing") as $st
           | if $st == "missing" then "exited"
             elif $st == "blocked" then "stuck"
             else "live"
             end
         end) as $live
      | (($e.after // []) | map(select(. as $a | $bad | any(. == $a)))) as $failed_deps
      | (($e.after // []) | map(select(. as $a | $ok | any(. == $a) | not))) as $unmet
      | (if $e.status == "pending" then (if ($failed_deps | length) > 0 then "dep failed" elif ($unmet | length) > 0 then "waiting deps" else "ready" end)
         elif ($mine | any(.type == "escalation")) then "escalation"
         elif ($mine | any(.type == "worker_done")) then "worker_done"
         elif ($mine | any(.type == "question")) then "question"
         elif $live == "stuck" then "stuck"
         elif $live == "exited" and $e.status == "working" then "exited"
         elif $e.status == "failed_start" then "failed_start"
         elif ($mine | any(.type == "status")) then "status"
         else "none" end) as $att
      | (($mine | map(select(.type == $att)) | first | .id) // "") as $mid
      | (if $att == "escalation" then "tell the user; \($mail) read --id \($mid)"
         elif $att == "worker_done" then "\($mail) ack --id \($mid) --decision reuse|retain|release"
         elif $att == "question" then "\($mail) reply --id \($mid) --body ..."
         elif $att == "stuck" then "herdr agent read \($e.agent) --source recent-unwrapped --lines 120"
         elif $att == "exited" then "inspect; \($mail) retry --dispatch \($e.dispatch) or abandon"
         elif $att == "failed_start" then "\($mail) show --dispatch \($e.dispatch); \($mail) retry --dispatch \($e.dispatch)"
         elif $att == "status" then "\($mail) read --id \($mid); \($mail) ack --id \($mid)"
         elif $att == "dep failed" then "\($mail) retry --dispatch \($failed_deps | first)"
         elif $att == "waiting deps" then "-"
         elif $att == "ready" then "launching on next ack"
         else "\($mail) wait" end) as $next
      | {dispatch: $e.dispatch, agent: ($e.agent // "-"), attempt: $e.attempt, status: $e.status, outcome: ($e.outcome // "-"),
         liveness: $live, attention: $att, next: $next, unacked: ($mine | length), after: ($e.after // [])}' "$LEDGER")
    if [ "$JSON" = 1 ]; then jq -s . <<<"$ROWS"; else
      { printf 'DISPATCH\tAGENT\tSTATUS\tOUTCOME\tLIVE\tATTENTION\tNEXT\n'
        jq -r '[.dispatch, .agent, .status, .outcome, .liveness, .attention, .next] | @tsv' <<<"$ROWS"; } | column -t -s $'\t'
    fi
    ;;

  retry)
    bind_run
    D=; while [ $# -gt 0 ]; do case "$1" in --dispatch) D=$2; shift 2 ;; *) die "retry: unknown option $1" ;; esac; done
    OLD=$(entry "$D"); [ -n "$OLD" ] || die "no dispatch $D in run $RUN"
    N=$(new_id)
    NEW=$(jq -c --arg n "$N" --arg ts "$(now)" '{dispatch: $n, attempt: (.attempt + 1), supersedes: .dispatch, after: .after,
      status: "pending", outcome: null, decision: null, created: $ts, started: null, settled: null, agent: null, launch: .launch}' <<<"$OLD")
    ledger_mod --arg d "$D" --argjson n "$NEW" 'map(if .dispatch == $d and .status != "settled" then .status = "abandoned" else . end) + [$n]'
    if deps_met "$N"; then launch_dispatch "$N"; else jq -cn --arg d "$N" '{dispatch: $d, status: "pending"}'; fi
    ;;

  stop|abandon)
    bind_run
    D=; while [ $# -gt 0 ]; do case "$1" in --dispatch) D=$2; shift 2 ;; *) die "$CMD: unknown option $1" ;; esac; done
    E=$(entry "$D"); [ -n "$E" ] || die "no dispatch $D in run $RUN"
    AGENT=$(jq -r '.agent // empty' <<<"$E")
    NEWST=stopped; [ "$CMD" = stop ] || NEWST=abandoned
    ledger_mod --arg d "$D" --arg s "$NEWST" --arg ts "$(now)" 'map(if .dispatch == $d then .status = $s | .settled = $ts else . end)'
    [ -z "$AGENT" ] || unindex_worker "$AGENT"
    [ "$CMD" != stop ] || [ -z "$AGENT" ] || release_worker "$AGENT" "$D"
    jq -cn --arg d "$D" --arg s "$NEWST" '{dispatch: $d, status: $s}'
    ;;

  show)
    bind_run
    D=; while [ $# -gt 0 ]; do case "$1" in --dispatch) D=$2; shift 2 ;; *) die "show: unknown option $1" ;; esac; done
    E=$(entry "$D"); [ -n "$E" ] || die "no dispatch $D in run $RUN"; jq . <<<"$E"
    ;;
  list) bind_run; jq . "$LEDGER" ;;

  report)
    bind_run
    FROM= TYPE= SUBJ= BODY= BODY_FILE= OUTCOME=
    while [ $# -gt 0 ]; do
      case "$1" in
        --from) FROM=$2; shift 2 ;; --type) TYPE=$2; shift 2 ;; --subject) SUBJ=$2; shift 2 ;;
        --body) BODY=$2; shift 2 ;; --body-file) BODY_FILE=$2; shift 2 ;; --outcome) OUTCOME=$2; shift 2 ;;
        *) die "report: unknown option $1" ;;
      esac
    done
    case "$TYPE" in status|question|escalation|worker_done) ;; *) die "report: --type must be status|question|escalation|worker_done" ;; esac
    SENT=$("$MAIL" send --inbox "$INBOX" --from "$FROM" --type "$TYPE" --subject "$SUBJ" \
      ${BODY_FILE:+--body-file "$BODY_FILE"} ${BODY:+--body "$BODY"} ${OUTCOME:+--outcome "$OUTCOME"})
    echo "$SENT"
    [ "$TYPE" != worker_done ] || unindex_worker "$FROM"
    case "$TYPE" in question|escalation|worker_done)
      ID=$(jq -r .sent <<<"$SENT"); SHORT=$(printf '%s' "$SUBJ" | tr '\n|' '  ' | cut -c1-160)
      notify_supervisor "MAIL|$ID|$TYPE|$FROM|$SHORT - handle it: $HERE/dispatch.sh read --run $RUN --id $ID" \
        "dispatch: $TYPE from $FROM" "$SHORT" ;;
    esac
    ;;
  *) die "unknown command $CMD" ;;
esac
