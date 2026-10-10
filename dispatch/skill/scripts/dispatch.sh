#!/usr/bin/env bash
# Supervisor front-end for Herdr dispatch: a per-run ledger of dispatch attempts, an ack-based
# mailbox, off-pane replies, dependency launches, and a fleet view with liveness + next action.
#
#   dispatch.sh run     [--name <slug>] [--wake <types>] [--label <text>] [--link <url>]   bind (create or resume) a run; later commands default to it
#                       --wake: mail types that wake the supervisor (default question,escalation,worker_done)
#                       --label: label of the supervisor's row in the sidebar tree, such as an issue key
#   dispatch.sh start   --repo <path> --branch <name> --name <agent> --task <file>
#                       [--label <text>] [--link <url>] [--kind <herdr kind>] [--base <ref>] [--after <dispatch>]... [-- <agent args>]
#                       --label: label of the worker's row. A name that starts with an issue key
#                       (sre-142-fix-probes) gives the label SRE-142 and the name fix-probes.
#                       --link: web address the sidebar tree opens for the row, such as that of the issue
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
#                       worker side: mail the supervisor, and wake it for the run's wake types
#   dispatch.sh notify  --line <text> [--to <agent|pane>] [--title <toast title>]
#                       type one line into the supervisor (or --to) from any process; exit 3 = no such agent
#   dispatch.sh adopt                                            link the live workers of every run to their
#                       supervisor in Herdr and label them; for runs started before Herdr kept the links
#
# Run selection: --run <id> anywhere, else $DISPATCH_RUN, else the run last bound from this pane.
# Ledger: <state>/agent-dispatch/runs/<run>/ledger.json. Mailbox: <run>/inbox.jsonl (worker -> supervisor),
# <run>/workers/<agent>/inbox.jsonl (supervisor -> worker).
# Herdr holds the rest: who supervises which pane, the wake lines that wait for a safe moment, and
# the reports of a blocked, stalled or gone worker.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
MAIL="$HERE/mail.sh"
STATE="${XDG_STATE_HOME:-$HOME/.local/state}/agent-dispatch"
RUNS="$STATE/runs"
# What Herdr reports to a supervisor about a worker that has not finished.
REPORTS=blocked,exited,stalled
POINTER="$STATE/current-run${HERDR_PANE_ID:+.${HERDR_PANE_ID//:/_}}"

die() { echo "dispatch.sh: $*" >&2; exit 1; }
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
new_id() { echo "d_$(date +%Y%m%d%H%M%S)_$RANDOM"; }
for bin in jq herdr flock; do command -v "$bin" >/dev/null || die "$bin not on PATH"; done

CMD=${1:-}; shift || true
[ -n "$CMD" ] || { sed -n '2,/^set -euo pipefail/{/^set -euo pipefail/!p}' "$0" >&2; exit 2; }

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

# The issue key at the start of a name, upper case, or nothing: sre-142-fix-probes -> SRE-142.
ticket_of() { printf '%s' "$1" | sed -nE 's/^([A-Za-z]{2,6})-([0-9]{1,5})(-.*)?$/\1-\2/p' | tr '[:lower:]' '[:upper:]'; }

# Set or clear the label of a pane's row in the sidebar tree.
label_pane() {  # <pane> <text>
  [ -n "$1" ] || return 0
  if [ -n "$2" ]; then herdr pane report-metadata "$1" --source tree --token "label=$2" >/dev/null 2>&1 || true
  else herdr pane report-metadata "$1" --source tree --clear-token label >/dev/null 2>&1 || true; fi
}

# Set the web address that the sidebar tree opens for a pane's row. No address sets nothing.
link_pane() {  # <pane> <url>
  [ -n "$1" ] && [ -n "$2" ] || return 0
  herdr pane report-metadata "$1" --source tree --token "link=$2" >/dev/null 2>&1 || true
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
  out=$(herdr agent deliver "$1" "$2" --source dispatch 2>&1) || {
    DELIVER_ERROR=$(jq -r '.error.code // empty' <<<"$out" 2>/dev/null || true)
    [ -n "$DELIVER_ERROR" ] || DELIVER_ERROR="herdr did not answer"
    return 1
  }
  case "$(jq -r '.result.delivery.held_by // empty' <<<"$out")" in
    focused|unsent_input) herdr notification show "$3" --body "$4" --sound request >/dev/null 2>&1 || true ;;
  esac
}
supervisor() { cat "$RUN_DIR/supervisor" 2>/dev/null || true; }
notify_supervisor() {  # <prompt line> <toast title> <toast body>
  deliver "$(supervisor)" "$@" || echo "dispatch.sh: note: the supervisor was not woken ($DELIVER_ERROR); the message is in the run inbox" >&2
}

# Tell a worker it has mail. The message is in its inbox either way.
nudge() {  # <agent> <prompt line>
  deliver "$1" "$2" "dispatch: mail for $1" "$2" && return 0
  echo "dispatch.sh: note: $1 was not nudged ($DELIVER_ERROR); the message is in its inbox" >&2
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
          workspace_id: $r.workspace_id, pane_id: $r.pane_id, worktree: $r.worktree, branch: $r.branch,
          prompt_file: $r.task_file, worker_inbox: $r.worker_inbox, agent_status: $r.status} else . end)'
  local ag pane; ag=$(jq -r .agent <<<"$receipt"); pane=$(jq -r '.pane_id // empty' <<<"$receipt")
  label_pane "$pane" "$(jq -r '.label // ""' <<<"$spec")"
  link_pane "$pane" "$(jq -r '.link // ""' <<<"$spec")"
  # A fast worker can report worker_done before the launcher returns; it then gets no reports.
  if [ "$ls" = working ] && [ -z "$(jq -c --arg a "$ag" --arg t "$t0" 'select(.from == $a and .type == "worker_done" and .ts >= $t)' "$INBOX")" ]; then
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

# Release = post-settlement cleanup: removes the worktree, which also kills a live agent.
release_worker() {
  local agent=$1 D=$2 ws out E
  E=$(entry "$D")
  # A worker that an older version placed as a tab shares its supervisor's workspace, so the
  # workspace must not be removed for it.
  if [ "$(jq -r '.placement // "workspace"' <<<"$E")" = tab ]; then
    echo "dispatch.sh: warning: $agent was placed as a tab by an older version; nothing removed" >&2
    echo "dispatch.sh: remove it by hand. Find its tab with: herdr agent get $agent" >&2
    echo "dispatch.sh: then: herdr tab close <tab>; git worktree remove $(printf '%q' "$(jq -r '.worktree // "<worktree>"' <<<"$E")")" >&2
    return 0
  fi
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

case "$CMD" in
  run)
    NAME= WAKE= LABEL= LABEL_SET= LINK=
    while [ $# -gt 0 ]; do case "$1" in --name) NAME=$2; shift 2 ;; --wake) WAKE=$2; shift 2 ;; --label) LABEL=$2 LABEL_SET=1; shift 2 ;; --link) LINK=$2; shift 2 ;; *) die "run: unknown option $1" ;; esac; done
    check_link "$LINK"
    herdr agent deliveries >/dev/null 2>&1 || die "this Herdr server has no delivery queue: dispatch needs the Herdr build with orchestration support"
    case ",$WAKE," in *[!a-z_,]*) die "run: --wake takes a comma-separated list of mail types" ;; esac
    RUN=${NAME:-run-$(date +%Y%m%d-%H%M%S)}
    RUN=$(printf '%s' "$RUN" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_.-]+/-/g')
    RUN_DIR="$RUNS/$RUN"; mkdir -p "$RUN_DIR/workers" "$STATE"
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
    jq -cn --arg run "$RUN" --arg dir "$RUN_DIR" '{run: $run, dir: $dir, inbox: ($dir + "/inbox.jsonl"), ledger: ($dir + "/ledger.json")}'
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
        [A-Za-z]*) echo "dispatch.sh: note: $LABEL of $NAME is the label and the rest is the agent name; use the agent name in the receipt for later commands, or pass --label \"\" to keep a name whole" >&2; NAME=$REST ;;
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
    JSON=0; while [ $# -gt 0 ]; do case "$1" in --json) JSON=1; shift ;; *) die "ps: unknown option $1" ;; esac; done
    UNACKED=$("$MAIL" read --inbox "$INBOX" --unacked | jq -sc .)
    AS='{}'
    for A in $(jq -r '.[] | select(.status == "working") | .agent // empty' "$LEDGER" | sort -u); do
      ST=$(herdr agent get "$A" 2>/dev/null | jq -r '.result.agent | if . == null then "missing" elif .stalled_secs then "stalled" else .agent_status end' || echo missing)
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
             elif $st == "stalled" then "stalled"
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
         elif $live == "stalled" then "stalled"
         elif $live == "exited" and $e.status == "working" then "exited"
         elif $e.status == "failed_start" then "failed_start"
         elif ($mine | any(.type == "status")) then "status"
         else "none" end) as $att
      | (($mine | map(select(.type == $att)) | first | .id) // "") as $mid
      | (if $att == "escalation" then "tell the user; \($mail) read --id \($mid)"
         elif $att == "worker_done" then "\($mail) ack --id \($mid) --decision reuse|retain|release"
         elif $att == "question" then "\($mail) reply --id \($mid) --body ..."
         elif $att == "stuck" then "herdr agent get \($e.agent) | jq -r .result.agent.blocker"
         elif $att == "stalled" then "herdr agent read \($e.agent) --source recent-unwrapped --lines 120"
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
    # The attempt that is replaced sends no more reports.
    OLD_AGENT=$(jq -r '.agent // empty' <<<"$OLD"); [ -z "$OLD_AGENT" ] || supervise "$OLD_AGENT"
    ledger_mod --arg d "$D" --argjson n "$NEW" 'map(if .dispatch == $d and .status != "settled" then .status = "abandoned" else . end) + [$n]'
    if deps_met "$N"; then launch_dispatch "$N"; else jq -cn --arg d "$N" '{dispatch: $d, status: "pending"}'; fi
    ;;

  stop|abandon)
    bind_run
    D=; while [ $# -gt 0 ]; do case "$1" in --dispatch) D=$2; shift 2 ;; *) die "$CMD: unknown option $1" ;; esac; done
    E=$(entry "$D"); [ -n "$E" ] || die "no dispatch $D in run $RUN"
    AGENT=$(jq -r '.agent // empty' <<<"$E")
    # Stop removes the worker with its worktree workspace. A worker that an older version placed
    # as a tab has none of its own, so it cannot be stopped from here.
    if [ "$CMD" = stop ] && [ "$(jq -r '.placement // "workspace"' <<<"$E")" = tab ]; then
      die "stop: $AGENT was placed as a tab by an older version. Close its tab by hand, then run: dispatch.sh abandon --dispatch $D"
    fi
    NEWST=stopped; [ "$CMD" = stop ] || NEWST=abandoned
    ledger_mod --arg d "$D" --arg s "$NEWST" --arg ts "$(now)" 'map(if .dispatch == $d then .status = $s | .settled = $ts else . end)'
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
    deliver "$TO" "$LINE" "${TITLE:-dispatch: wake held}" "$LINE" || exit 3
    ;;

  adopt)
    # Runs started before Herdr kept the links: link each live worker, and label it from its name.
    N=0
    for LEDGER in "$RUNS"/*/ledger.json; do
      [ -f "$LEDGER" ] || continue
      RUN_DIR=$(dirname "$LEDGER"); RUN=$(basename "$RUN_DIR"); SUP=$(supervisor)
      herdr pane get "$SUP" >/dev/null 2>&1 || continue
      while IFS=$'\t' read -r AGENT STATUS LEDGER_PANE STARTED; do
        PANE=$(herdr agent get "$AGENT" 2>/dev/null | jq -r '.result.agent.pane_id // empty' || true)
        # The name must still be on the pane the ledger has. A later run can own the name now.
        [ -n "$PANE" ] && [ "$PANE" = "$LEDGER_PANE" ] && [ "$PANE" != "$SUP" ] || continue
        # A worker that said it is done is not watched, also while that mail waits for its ack.
        # An earlier attempt can have had the same name, so only mail since this one started counts.
        DONE=$(jq -c --arg a "$AGENT" --arg t "$STARTED" 'select(.from == $a and .type == "worker_done" and .ts >= $t)' "$RUN_DIR/inbox.jsonl" 2>/dev/null | head -1 || true)
        if [ "$STATUS" = working ] && [ -z "$DONE" ]; then supervise "$PANE" "$REPORTS"; else supervise "$PANE"; fi
        KEY=$(ticket_of "$AGENT"); [ -n "$KEY" ] || KEY=$(ticket_of "$RUN")
        [ -z "$KEY" ] || [ -n "$(herdr pane get "$PANE" | jq -r '.result.pane.tokens.label // empty')" ] || label_pane "$PANE" "$KEY"
        N=$((N + 1))
      done < <(jq -r '.[] | select(.agent != null and (.status == "working" or .decision == "retain" or .decision == "reuse")) | [.agent, .status, (.pane_id // "-"), (.started // "")] | @tsv' "$LEDGER")
    done
    jq -cn --argjson n "$N" '{adopted: $n}'
    ;;

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
    # The name goes into a line typed into the supervisor's prompt.
    case "$FROM" in ""|*[!A-Za-z0-9_.-]*) die "report: --from must be an agent name ([A-Za-z0-9_.-])" ;; esac
    SENT=$("$MAIL" send --inbox "$INBOX" --from "$FROM" --type "$TYPE" --subject "$SUBJ" \
      ${BODY_FILE:+--body-file "$BODY_FILE"} ${BODY:+--body "$BODY"} ${OUTCOME:+--outcome "$OUTCOME"})
    echo "$SENT"
    # A worker that is done stays linked for the tree and sends no more reports.
    [ "$TYPE" != worker_done ] || supervise "$FROM"
    case ",$(cat "$RUN_DIR/wake-types" 2>/dev/null || echo question,escalation,worker_done)," in *",$TYPE,"*)
      ID=$(jq -r .sent <<<"$SENT"); SHORT=$(printf '%s' "$SUBJ" | tr '|' ' ' | tr '[:cntrl:]' ' ' | cut -c1-160 | iconv -c -f UTF-8 -t UTF-8 2>/dev/null || true)
      notify_supervisor "MAIL|$ID|$TYPE|$FROM|$SHORT - handle it: $HERE/dispatch.sh read --run $RUN --id $ID" \
        "dispatch: $TYPE from $FROM" "$SHORT" ;;
    esac
    ;;
  *) die "unknown command $CMD" ;;
esac
