#!/usr/bin/env bash
# Sourced by fleet.sh. Pause records are control data, not worker settlement.
pause_valid() {
  jq -e '.version == 1 and (.id | type == "string") and (.root | type == "string") and
    (.state as $s | ["pausing","paused","failed","resuming","resumed","released"] | index($s) != null) and
    (.runs | type == "array") and (.panes | type == "array") and (.agents | type == "array") and
    (.snapshots | type == "array") and (.run | type == "string" and length > 0) and
    (. as $p | (.runs | index($p.run)) != null and (.panes | index($p.root)) != null) and
    (.runs | all(type == "string")) and (.panes | all(type == "string")) and (.agents | all(type == "string")) and
    (. as $p | ([.snapshots[].run] | sort) == ($p.runs | unique | sort)) and
    (.snapshots | all((.ledger | type == "array") and (.supervisor | type == "string")))' "$1" >/dev/null 2>&1
}
# All lifecycle mutations share one lock with ordinary fleet mutations.
fleet_pause_active() { # [target]
  local file target=${1:-} run=${RUN:-}
  for file in "$RUNS"/*/pause.json; do
    [ -f "$file" ] || continue
    pause_valid "$file" || die "invalid pause record: $file (fail closed)"
    if jq -e --arg r "$run" --arg p "${HERDR_PANE_ID:-}" --arg t "$target" '
      .state != "resumed" and .state != "released" and
      ((.runs | index($r)) != null or (.panes | index($p)) != null or
       ($t != "" and ((.panes | index($t)) != null or (.agents | index($t)) != null)))' "$file" >/dev/null; then
      return 0
    fi
  done
  return 1
}

fleet_pause_admission() {
  case "$CMD" in read|wait|ps|show|list|report|pause|pause-status|unpause|pause-release|notify) return ;; esac
  # --name determines the run before `run` binds it.
  local arg next=0 saved=$RUN
  if [ "$CMD" = run ]; then
    for arg in "$@"; do
      if [ "$next" = 1 ]; then RUN=$arg; break; fi
      [ "$arg" != --name ] || next=1
    done
  fi
  [ -n "$RUN" ] || RUN=$(cat "$POINTER" 2>/dev/null || true)
  fleet_pause_active && die "subtree paused or pausing; use unpause with explicit authorization"
  local target='' dispatch='' message='' flag=''
  for arg in "$@"; do
    if [ -n "$flag" ]; then
      case "$flag" in --agent) target=$arg ;; --name) [ "$CMD" != start ] || target=$arg ;; --dispatch) dispatch=$arg ;; --id) message=$arg ;; esac
      flag=; continue
    fi
    case "$arg" in --agent|--name|--dispatch|--id) flag=$arg ;; esac
  done
  if [ -n "$message" ] && [ -f "$RUNS/$RUN/inbox.jsonl" ]; then
    target=$("$MAIL" read --inbox "$RUNS/$RUN/inbox.jsonl" --id "$message" | jq -r '.from // empty')
  fi
  if [ -n "$target" ] && fleet_pause_active "$target"; then die "target subtree paused or pausing; explicit unpause required"; fi
  if [ -n "$dispatch" ] && [ -f "$RUNS/$RUN/ledger.json" ]; then
    while IFS= read -r target; do
      [ -z "$target" ] || ! fleet_pause_active "$target" || die "dispatch subtree paused or pausing; explicit unpause required"
    done < <(jq -r --arg d "$dispatch" '.[] | select(.dispatch == $d) | .agent, .pane_id, .launch.name // empty' "$RUNS/$RUN/ledger.json")
  fi
  if [ "$CMD" = adopt ]; then
    for arg in "$RUNS"/*/pause.json; do
      [ -f "$arg" ] || continue
      jq -e '.state == "resumed" or .state == "released"' "$arg" >/dev/null || die "subtree paused; adopt would change topology"
    done
  fi
  RUN=$saved
}

pause_write() { # <json>
  local tmp
  tmp=$(mktemp "$RUN_DIR/pause.json.XXXXXX")
  printf '%s\n' "$1" > "$tmp"
  chmod 600 "$tmp"
  # sync before rename makes coordinator exit/restart recovery independent of memory.
  sync -f "$tmp"
  mv "$tmp" "$RUN_DIR/pause.json"
  sync -f "$RUN_DIR"
}

pause_owned_rows() { # snapshots JSON -> current, unreleased ownership only
  jq -c "$REPLACED_JQ"' [.[] as $s | $s.ledger as $l | $l[] as $e
    | select(($e.pane_id // "") != "" and $e.released == null
        and (["working","pending","failed_start","settled"] | index($e.status)) != null
        and ($l | replaced_by($e)) == null)
    | {run:$s.run,dispatch:$e.dispatch,pane_id:$e.pane_id,supervisor_pane_id:$s.supervisor,
       agent:$e.agent,session:$e.session}]' <<<"$1"
}

pause_refresh_expected() {
  local owned; owned=$(pause_owned_rows "$(jq -c .snapshots <<<"$1")")
  jq -c --argjson owned "$owned" '.expected=($owned | map(del(.run,.dispatch)) | unique_by(.pane_id)) |
    .panes=((.panes + [.root, .expected[].pane_id, .expected[].supervisor_pane_id]) | unique) |
    .agents=((.agents + [.expected[].agent // empty]) | unique)' <<<"$1"
}

pause_verify_coverage() {
  jq -c "$REPLACED_JQ"'
    def session_key: {agent,kind,value};
    . as $p | [$p.server.members[].pane_id] as $covered |
    ([$p.root, $p.expected[].pane_id, $p.expected[].supervisor_pane_id] | unique) as $required |
    .verification={
      missing_members:[$required[] as $id | select(($covered | index($id)) == null) | $id],
      unresolved_attempts:[$p.snapshots[] as $s | $s.ledger as $l | $l[] as $e |
        select(($e.pane_id // "") == "" and $e.released == null and
          ($e.status == "working" or $e.status == "failed_start" or ($e.status == "pending" and $e.started != null)) and
          ($l | replaced_by($e)) == null) | {run:$s.run,dispatch:$e.dispatch,agent:$e.agent,workspace_id:$e.workspace_id,worktree:$e.worktree}],
      missing_links:[$p.expected[] as $e | select(any($p.server.links[]?;
        .pane_id == $e.pane_id and .supervisor_pane_id == $e.supervisor_pane_id) | not) | $e | {pane_id,supervisor_pane_id}],
      identity_mismatches:[$p.expected[] as $e | $p.server.members[] | select(.pane_id == $e.pane_id) | . as $m |
        select(($e.agent != null and $e.agent != $m.identity.name) or
          ($e.session != null and ($e.session | session_key) != ($m.identity.session | session_key))) |
        select(any($p.server.reconciliations[]?; .pane_id == $m.pane_id and .current == $m.identity and
          (.authorization // "" | length) > 0 and (.evidence // "" | length) > 0) | not) | $e.pane_id]
    }' <<<"$1"
}

pause_server_request() {
  local tmp expected_file="$RUN_DIR/pause.expected.json"
  tmp=$(mktemp "$expected_file.XXXXXX")
  jq '.expected' <<<"$1" > "$tmp"; chmod 600 "$tmp"; sync -f "$tmp"
  mv "$tmp" "$expected_file"; sync -f "$RUN_DIR"
  herdr subtree pause --target "$(jq -r .root <<<"$1")" --id "$(jq -r .id <<<"$1")" --expected-file "$expected_file"
}

pause_snapshot() {
  local ix parents runs snapshots r dir owned
  ix=$(runs_index); parents=$(run_parents "$ix")
  snapshots='[]'
  for dir in "$RUNS"/*; do
    [ -f "$dir/ledger.json" ] || continue
    r=${dir##*/}
    snapshots=$(jq -cn --argjson old "$snapshots" --arg run "$r" --arg supervisor "$(cat "$dir/supervisor" 2>/dev/null || true)" \
      --arg wake "$(cat "$dir/wake-types" 2>/dev/null || echo question,escalation,worker_done)" \
      --slurpfile ledger "$dir/ledger.json" '$old + [{run:$run,supervisor:$supervisor,wake_types:$wake,ledger:$ledger[0]}]')
  done
  owned=$(pause_owned_rows "$snapshots")
  parents=$(jq -c --argjson o "$owned" 'with_entries(.value as $p |
    select(any($o[]; .run == $p.run and .dispatch == $p.dispatch)))' <<<"$parents")
  runs=$(jq -nc --arg root "$RUN" --argjson p "$parents" '
    def below($r; $seen): if ($seen | index($r)) != null then [] else
      [$r] + ([$p | to_entries[] | select(.value.run == $r) | .key | below(.; $seen + [$r])[]]) end;
    below($root; []) | unique')
  snapshots=$(jq -c --argjson runs "$runs" 'map(select(.run as $r | $runs | index($r) != null))' <<<"$snapshots")
  local record
  record=$(jq -cn --arg run "$RUN" --arg root "$(supervisor)" --arg id "fleet-${RUN}-$(date +%s)-$RANDOM" \
    --arg ts "$(now)" --argjson runs "$runs" --argjson snapshots "$snapshots" '
    {version:1,id:$id,run:$run,root:$root,state:"pausing",created:$ts,runs:$runs,snapshots:$snapshots,
     panes:[$root],agents:[],server:null}')
  pause_refresh_expected "$record"
}

fleet_pause_command() {
  local approval='' file="$RUN_DIR/pause.json" record result auth jobs r expected current
  while [ $# -gt 0 ]; do
    case "$1" in --authorization) approval=${2:?missing authorization file}; shift 2 ;; *) die "$CMD: unknown option $1" ;; esac
  done
  [ ! -f "$file" ] || pause_valid "$file" || die "invalid pause record: $file (fail closed)"
  if [ "$CMD" = pause ]; then
    if [ -f "$file" ] && jq -e '.state != "resumed" and .state != "released"' "$file" >/dev/null; then record=$(cat "$file")
    else record=$(pause_snapshot); pause_write "$record"; fi
    record=$(pause_refresh_expected "$record"); pause_write "$record"
    # Durable local intent already gates all descendants if Herdr is unreachable.
    if ! result=$(pause_server_request "$record"); then
      die "pause incomplete: server fence unconfirmed; intent saved at $file; rerun pause"
    fi
  else
    [ -f "$file" ] || die "no pause record for $RUN"
    record=$(cat "$file")
    result=$(herdr subtree status --id "$(jq -r .id <<<"$record")") || die "pause status unconfirmed; local fence retained"
  fi
  jq -e --arg id "$(jq -r .id <<<"$record")" '.result.pause.id == $id and (.result.pause.members | type == "array" and length > 0)' <<<"$result" >/dev/null || die "invalid server pause receipt; local fence retained"
  record=$(jq -c --argjson s "$(jq -c .result.pause <<<"$result")" '
    .server=$s | .panes=((.panes + [$s.members[].pane_id]) | unique) |
    .agents=((.agents + [$s.members[].identity.name // empty]) | unique)' <<<"$record")
  # Server topology is authoritative even when an older run has no parent ledger metadata.
  local dir sup extra before
  before=$(jq ' .runs | length' <<<"$record")
  for dir in "$RUNS"/*; do
    [ -f "$dir/ledger.json" ] && [ -f "$dir/supervisor" ] || continue
    sup=$(cat "$dir/supervisor"); r=${dir##*/}
    if jq -e --arg p "$sup" --arg r "$r" '(.panes | index($p)) != null and (.runs | index($r)) == null' <<<"$record" >/dev/null; then
      extra=$(jq -cn --arg r "$r" --arg sup "$sup" --arg wake "$(cat "$dir/wake-types" 2>/dev/null || echo question,escalation,worker_done)" \
        --slurpfile l "$dir/ledger.json" '{run:$r,supervisor:$sup,wake_types:$wake,ledger:$l[0]}')
      record=$(jq -c --arg r "$r" --argjson e "$extra" '.runs += [$r] | .snapshots += [$e]' <<<"$record")
    fi
  done
  record=$(pause_refresh_expected "$record")
  if [ "$CMD" = pause ] && [ "$(jq '.runs | length' <<<"$record")" -gt "$before" ]; then
    # New owned scopes are fenced to a fixed point; status alone never widens the server fence.
    pause_write "$record"
    fleet_pause_command "$@"
    return
  fi
  record=$(pause_verify_coverage "$record")
  if [ "$CMD" = unpause ]; then
    jq -e '[.verification[] | length] | add == 0' <<<"$record" >/dev/null || { pause_write "$record"; die "pause coverage incomplete: owned identities or links are missing; rerun pause and reconcile before resume"; }
  fi
  if [ "$CMD" = unpause ] || [ "$CMD" = pause-release ]; then
    [ -n "$approval" ] && [ -r "$approval" ] || die "unpause requires --authorization <reconciliation.json> from explicit user authorization"
    jq -e '(.authorization | type == "string" and length > 0) and .ownership_checked == true and .pr_state_checked == true and (.jobs | type == "array")' "$approval" >/dev/null \
      || die "authorization requires authorization text, ownership_checked, pr_state_checked and jobs"
    local operation=resume final_state=resumed
    if [ "$CMD" = pause-release ]; then
      operation=release; final_state=released
      jq -e '.jobs | length == 0' "$approval" >/dev/null || die "pause-release cannot restore jobs; use an empty jobs array"
    fi
    jq -e --arg id "$(jq -r .id <<<"$record")" --arg operation "$operation" \
      '.pause_id == $id and .operation == $operation' "$approval" >/dev/null \
      || die "authorization does not match this pause ID and operation"
    # Ledger drift is not silently accepted as the same ownership.
    while IFS= read -r r; do
      expected=$(jq -Sc --arg r "$r" '.snapshots[] | select(.run == $r) | .ledger' <<<"$record")
      current=$(jq -Sc . "$RUNS/$r/ledger.json")
      if [ "$expected" != "$current" ]; then
        local approved
        approved=$(jq -Sc --arg r "$r" '.ownership[$r] // empty' "$approval")
        [ "$approved" = "$current" ] || die "ownership changed in $r; reconcile and authorize its exact current ledger before resume"
      fi
    done < <(jq -r '.runs[]' <<<"$record")
    local job command repo number state watcher
    while IFS= read -r job; do
      command=$(jq -c --arg j "$job" '.server.jobs[] | select(.name == $j) | .command' <<<"$record")
      [ -n "$command" ] || die "job not in saved pause: $job"
      watcher=$(jq -c 'to_entries | map(select(.value | endswith("/pr-watch.sh") or . == "pr-watch.sh")) | first // empty' <<<"$command")
      if [ -n "$watcher" ]; then
        repo=$(jq -r --argjson i "$(jq -r .key <<<"$watcher")" '.[$i+1] // empty' <<<"$command")
        number=$(jq -r --argjson i "$(jq -r .key <<<"$watcher")" '.[$i+2] // empty' <<<"$command")
        [[ "$repo" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ && "$number" =~ ^[0-9]+$ ]] || die "cannot reconcile PR watch arguments: $job"
        state=$(gh pr view "$number" --repo "$repo" --json state --jq .state) || die "cannot verify PR state for $job; fence retained"
        [ "$state" = OPEN ] || die "PR for $job is not OPEN ($state); omit the stale watch from jobs"
      fi
    done < <(jq -r '.jobs[]' "$approval")
    auth=$(jq -r .authorization "$approval"); jobs=$(jq -r '.jobs | join(",")' "$approval")
    record=$(jq -c --slurpfile a "$approval" '.authorization=$a[0] | .state="resuming"' <<<"$record")
    pause_write "$record"
    result=$(herdr subtree "$operation" --id "$(jq -r .id <<<"$record")" --authorization "$auth" --restore-jobs "$jobs") \
      || die "resume incomplete; local fence retained; inspect pause-status"
    jq -e --arg state "$final_state" '.result.pause.state == $state' <<<"$result" >/dev/null || die "server did not confirm $operation; local fence retained"
    record=$(jq -c --arg state "$final_state" --argjson s "$(jq -c .result.pause <<<"$result")" '.server=$s | .state=$state' <<<"$record")
  elif [ "$(jq -r .state <<<"$record")" != resuming ]; then
    record=$(jq -c '.state=(if .state == "resumed" or .state == "released" then .state elif ([.verification[] | length] | add) > 0 or .server.state == "failed" then "failed" elif .server.state == "paused" then "paused" else "pausing" end)' <<<"$record")
  fi
  pause_write "$record"
  jq '{id,state,root,runs,coverage:.server,verification,pending:((.server.pending // []) + .verification.missing_members + [.verification.missing_links[].pane_id] + .verification.identity_mismatches + [.verification.unresolved_attempts[] | "attempt:" + .dispatch] | unique)}' <<<"$record"
}
