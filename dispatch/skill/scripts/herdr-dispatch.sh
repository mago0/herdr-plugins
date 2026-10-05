#!/usr/bin/env bash
# Start a coding agent in a git worktree that Herdr opens as its own workspace, or with --tab
# as a new tab in the caller's workspace.
# Prints one JSON receipt on stdout; diagnostics go to stderr.
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: herdr-dispatch.sh --repo <path> --branch <name> --name <agent-name> --prompt-file <file>
                         [--base <ref>] [--kind <herdr agent kind>] [--heartbeat <minutes>]
                         [--tab] [--run-dir <dir> | --inbox <file>]
                         [-- <native agent args>...]
  --tab      open the worker as a new tab in the caller's Herdr workspace instead of its own
             workspace. The worktree path is the same; release closes the tab and runs
             `git worktree remove`.
  --run-dir  supervised mode: run inbox at <dir>/inbox.jsonl, a per-worker inbox for replies,
             and a heartbeat obligation (default 10 min). Used by dispatch.sh.
  --inbox    legacy single-mailbox mode (no worker inbox, no heartbeats).
EOF
  exit 2
}

REPO= BRANCH= BASE= NAME= KIND= PROMPT_FILE= INBOX= RUN_DIR= HEARTBEAT=10 TAB_MODE=
AGENT_ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --tab) TAB_MODE=1; shift ;;
    --repo) REPO=$2; shift 2 ;;
    --branch) BRANCH=$2; shift 2 ;;
    --base) BASE=$2; shift 2 ;;
    --name) NAME=$2; shift 2 ;;
    --kind) KIND=$2; shift 2 ;;
    --prompt-file) PROMPT_FILE=$2; shift 2 ;;
    --inbox) INBOX=$2; shift 2 ;;
    --run-dir) RUN_DIR=$2; shift 2 ;;
    --heartbeat) HEARTBEAT=$2; shift 2 ;;
    --) shift; AGENT_ARGS+=("$@"); break ;;
    *) usage ;;
  esac
done
[ -n "$REPO" ] && [ -n "$BRANCH" ] && [ -n "$NAME" ] && [ -r "$PROMPT_FILE" ] || usage

die() { echo "herdr-dispatch: $*" >&2; exit 1; }
[ "${HERDR_ENV:-}" = "1" ] || die "not inside a Herdr pane (HERDR_ENV != 1)"
for bin in herdr jq git; do command -v "$bin" >/dev/null || die "$bin not on PATH"; done
HERDR_STATUS=$(herdr status 2>/dev/null) || die "herdr server not reachable (herdr status)"
case "$HERDR_STATUS" in *"compatible: no"*)
  echo "herdr-dispatch: warning: herdr client/server protocol mismatch, see herdr status" >&2 ;;
esac

# A dispatch inherits the caller's own agent kind unless --kind names one: claude dispatches
# claude, omp dispatches omp. A caller herdr does not recognize as an agent must pass --kind.
if [ -z "$KIND" ]; then
  CALLER_PANE=${HERDR_PANE_ID:-}
  [ -n "$CALLER_PANE" ] || CALLER_PANE=$(herdr pane current 2>/dev/null | jq -r '.result.pane.pane_id // empty' 2>/dev/null || true)
  [ -z "$CALLER_PANE" ] || KIND=$(herdr agent get "$CALLER_PANE" 2>/dev/null | jq -r '.result.agent.agent // empty' 2>/dev/null || true)
  [ -n "$KIND" ] || die "cannot tell the calling agent's kind; pass --kind"
fi

# The main checkout owns _worktrees/, also when --repo points inside a linked worktree.
COMMON=$(git -C "$REPO" rev-parse --path-format=absolute --git-common-dir) || die "$REPO is not a git repo"
ROOT=$(dirname "$COMMON")
SLUG=$(printf '%s' "$BRANCH" | tr '/' '-')
WT="$ROOT/_worktrees/$SLUG"

# Local-only ignore; the repo's tracked .gitignore stays untouched.
mkdir -p "$COMMON/info"
grep -qxF '_worktrees/' "$COMMON/info/exclude" 2>/dev/null || echo '_worktrees/' >>"$COMMON/info/exclude"

# Slug to 28 chars so the `-N` uniqueness suffix below still fits herdr's 32-char name limit.
NAME=$(printf '%s' "$NAME" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]+/-/g; s/^[^a-z]+//; s/-+$//' | cut -c1-28)
[ -n "$NAME" ] || die "agent name is empty after slugging"
LIVE=$(herdr agent list | jq -r '.result.agents[]?.name // empty')
BASE_NAME=$NAME; N=2
while printf '%s\n' "$LIVE" | grep -qxF "$NAME"; do NAME="$BASE_NAME-$N"; N=$((N + 1)); done

WT_EXISTS=; git -C "$ROOT" worktree list --porcelain | grep -qxF "worktree $WT" && WT_EXISTS=1
NEW_BRANCH=
if [ -z "$WT_EXISTS" ] && ! git -C "$ROOT" show-ref --verify --quiet "refs/heads/$BRANCH"; then
  NEW_BRANCH=1
  if [ -z "$BASE" ]; then
    git -C "$ROOT" fetch --quiet origin 2>/dev/null || true
    BASE=$(git -C "$ROOT" symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null || echo HEAD)
  fi
fi

TAB=
if [ -n "$TAB_MODE" ]; then
  # `herdr worktree create/open` always opens a new workspace, so tab mode makes the worktree
  # with git and opens a tab on it in the caller's workspace.
  CALLER_WS=${HERDR_WORKSPACE_ID:-}
  [ -n "$CALLER_WS" ] || CALLER_WS=$(herdr pane current 2>/dev/null | jq -r '.result.pane.workspace_id // empty' 2>/dev/null || true)
  [ -n "$CALLER_WS" ] || die "--tab: cannot resolve the caller's workspace"
  if [ -z "$WT_EXISTS" ]; then
    if [ -n "$NEW_BRANCH" ]; then git -C "$ROOT" worktree add --quiet -b "$BRANCH" "$WT" "$BASE" >&2
    else git -C "$ROOT" worktree add --quiet "$WT" "$BRANCH" >&2; fi || die "git worktree add $WT failed"
  fi
  OUT=$(herdr tab create --workspace "$CALLER_WS" --cwd "$WT" --label "$NAME" --no-focus)
  [ "$(echo "$OUT" | jq -r 'has("error")')" = "false" ] || die "tab: $(echo "$OUT" | jq -r .error.message)"
  PANE=$(echo "$OUT" | jq -r .result.root_pane.pane_id)
  WS=$(echo "$OUT" | jq -r .result.root_pane.workspace_id)
  TAB=$(echo "$OUT" | jq -r .result.tab.tab_id)
else
  if [ -n "$WT_EXISTS" ]; then
    OUT=$(herdr worktree open --cwd "$ROOT" --path "$WT" --label "$NAME" --no-focus)
  else
    ARGS=(--cwd "$ROOT" --branch "$BRANCH" --path "$WT" --label "$NAME" --no-focus)
    [ -z "$NEW_BRANCH" ] || ARGS+=(--base "$BASE")
    OUT=$(herdr worktree create "${ARGS[@]}")
  fi
  [ "$(echo "$OUT" | jq -r 'has("error")')" = "false" ] || die "worktree: $(echo "$OUT" | jq -r .error.message)"
  PANE=$(echo "$OUT" | jq -r .result.root_pane.pane_id)
  WS=$(echo "$OUT" | jq -r .result.workspace.workspace_id)
fi

# Herdr also opens the main checkout as a parent workspace. This script never closes it:
# a plain close is refused with `workspace_group_close_required`, and `--group` would take
# every worktree workspace and its agent with it.

PROMPT=$(cat "$PROMPT_FILE")
MAIL="$(cd "$(dirname "$0")" && pwd)/mail.sh"
DISPATCH="$(dirname "$MAIL")/dispatch.sh"
WORKER_INBOX=
if [ -n "$RUN_DIR" ]; then
  INBOX="$RUN_DIR/inbox.jsonl"
  WORKER_INBOX="$RUN_DIR/workers/$NAME/inbox.jsonl"
  mkdir -p "$(dirname "$WORKER_INBOX")"; touch "$INBOX" "$WORKER_INBOX"
  PROMPT+=$(cat <<EOF


## Reporting to your supervisor

You are dispatched agent \`$NAME\`. Your supervisor reads a mailbox, not your terminal. Send mail with:

    $DISPATCH report --run $(basename "$RUN_DIR") --from $NAME --type <type> --subject "<one line>" [--body-file <file>] [--outcome succeeded|failed]

Types:
- \`status\` - progress or a result; you keep working.
- \`heartbeat\` - liveness only. Send one every $HEARTBEAT minutes while you are working. It proves you are alive, not done.
- \`question\` - you need an answer. See "Your inbox" below for how the reply arrives.
- \`escalation\` - blocked, a human is needed.
- \`worker_done\` - task complete or abandoned. Send exactly once, with \`--outcome succeeded\` or \`--outcome failed\` (never encode failure only in prose), and a three-sentence summary in --body-file. After sending it, stop and idle; do not start new work.

Put long bodies in a file and pass --body-file. Do not use any other callback channel.

## Your inbox

Your supervisor answers questions and sends follow-ups to $WORKER_INBOX - never by typing into this terminal.
- After sending a \`question\`, wait for the answer: \`$MAIL wait --inbox $WORKER_INBOX --types reply\` then \`$MAIL read --inbox $WORKER_INBOX --unacked\`. Do not open a local prompt or wait for terminal input.
- Read your inbox for \`followup\` messages at natural checkpoints (before starting a new file, after a test run) and once more immediately before sending \`worker_done\`: \`$MAIL read --inbox $WORKER_INBOX --unacked\`.
- Ack each inbox message once you have handled it: \`$MAIL ack --inbox $WORKER_INBOX --id <id>\`.
EOF
)
elif [ -n "$INBOX" ]; then
  mkdir -p "$(dirname "$INBOX")"; touch "$INBOX"
  PROMPT+=$(cat <<EOF


## Reporting to your supervisor

You are dispatched agent \`$NAME\`. Your supervisor reads a mailbox, not your terminal. Send mail with:

    $MAIL send --inbox $INBOX --from $NAME --type <type> --subject "<one line>" [--body-file <file>]

Types: \`status\` (progress or a result, you keep working), \`question\` (you need an answer; the reply is typed into this terminal, wait for it), \`escalation\` (blocked, a human is needed), \`worker_done\` (task complete or abandoned; add \`--outcome succeeded|failed\`). Put long bodies in a file and pass --body-file. Do not use any other callback channel.
EOF
)
fi

# Herdr types the agent command into the pane's shell and rejects multi-line arguments,
# so the task travels as a file and the argument only points at it.
STATE="${XDG_STATE_HOME:-$HOME/.local/state}/agent-dispatch/prompts"
mkdir -p "$STATE"
TASK_FILE="$STATE/$NAME-$(date +%Y%m%dT%H%M%S).md"
printf '%s\n' "$PROMPT" >"$TASK_FILE"
agent_status() { herdr agent get "$NAME" 2>/dev/null | jq -r '.result.agent.agent_status // "missing"' 2>/dev/null || echo missing; }
# A pane whose shell is still starting drops the first characters of the command herdr types,
# which leaves the shell running `laude ...` or `mp ...` and no agent. Make the pane prove it
# can execute a whole command line first; a dropped character then costs only a marker retry.
# The shell computes the marker, so a match cannot be satisfied by the echoed input line.
warm_pane() {
  local i
  for i in 1 2 3 4 5; do
    herdr pane run "$PANE" 'echo $((6*7))-herdr-ready' >/dev/null 2>&1 || true
    herdr pane wait-output "$PANE" --match '42-herdr-ready' --timeout 5000 >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}
warm_pane || echo "herdr-dispatch: warning: pane $PANE never ran a marker command; starting anyway" >&2

for _ in 1 2 3; do
  START=$(herdr agent start "$NAME" --kind "$KIND" --pane "$PANE" --timeout 60000 -- "${AGENT_ARGS[@]}" \
    "Your task is in the file $TASK_FILE - read it now and carry it out in full." 2>&1 || true)
  CODE=$(echo "$START" | jq -r '.error.code // empty' 2>/dev/null || echo unparsable)
  [ -n "$CODE" ] || break
  if [ "$CODE" = agent_pane_busy ]; then sleep 3; continue; fi
  # A truncated command line leaves the shell reporting `command not found`. Clear it and
  # retype rather than recording a start failure the operator would retry by hand.
  if herdr pane read "$PANE" --source recent --lines 20 2>/dev/null | grep -q "command not found"; then
    herdr pane send-keys "$PANE" ctrl-c >/dev/null 2>&1 || true
    sleep 2
    continue
  fi
  break
done
# Agents that register through lifecycle hooks (omp) can be detected in the pane while
# `agent start` still times out waiting for its own detection path and never binds the name.
# The pane is the source of truth: if an agent is there, bind the name and carry on.
pane_agent() { herdr agent get "$PANE" 2>/dev/null | jq -r '.result.agent.agent_status // empty' 2>/dev/null || true; }
bind_name() {
  [ "$(herdr agent get "$NAME" 2>/dev/null | jq -r '.result.agent.pane_id // empty')" = "$PANE" ] && return 0
  herdr agent rename "$PANE" "$NAME" >/dev/null 2>&1
}
if [ -n "$CODE" ] && [ "$CODE" != "agent_not_ready" ]; then
  if [ -n "$(pane_agent)" ] && bind_name; then CODE=; fi
fi
# `agent_not_ready` means herdr started the agent but it has not reached idle, usually a
# startup approval or trust dialog. The name stays bound and readable, so report the live
# state below instead of a start failure.
if [ "$CODE" = agent_not_ready ]; then CODE=; fi
if [ -n "$CODE" ]; then
  STATUS="start_failed:$CODE"
else
  bind_name || true
  herdr agent wait "$NAME" --until working --until idle --until done --until blocked --timeout 60000 >/dev/null 2>&1 || true
  STATUS=$(agent_status)
fi

jq -n --arg agent "$NAME" --arg kind "$KIND" --arg ws "$WS" --arg pane "$PANE" --arg wt "$WT" \
  --arg branch "$BRANCH" --arg root "$ROOT" --arg inbox "$INBOX" --arg winbox "$WORKER_INBOX" --arg rundir "$RUN_DIR" \
  --arg status "$STATUS" --arg task "$TASK_FILE" --arg detail "$START" --argjson hb "$HEARTBEAT" --arg tab "$TAB" \
  '{backend:"herdr", agent:$agent, kind:$kind, placement:(if $tab == "" then "workspace" else "tab" end),
    workspace_id:$ws, tab_id:(if $tab == "" then null else $tab end), pane_id:$pane, worktree:$wt,
    branch:$branch, repo_root:$root, inbox:$inbox, worker_inbox:$winbox, run_dir:$rundir, heartbeat_min:$hb,
    task_file:$task, status:$status}
   + (if ($status | startswith("start_failed")) then {detail:$detail} else {} end)'
case "$STATUS" in working|idle|done) exit 0 ;; *) exit 3 ;; esac
