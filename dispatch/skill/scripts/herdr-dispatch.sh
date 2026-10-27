#!/usr/bin/env bash
# Start a coding agent in a git worktree that Herdr opens as its own workspace.
# Prints one JSON receipt on stdout; diagnostics go to stderr.
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: herdr-dispatch.sh --repo <path> --branch <name> --name <agent-name> --prompt-file <file>
                         [--base <ref>] [--kind <herdr agent kind>]
                         [--run-dir <dir> | --inbox <file>]
                         [-- <native agent args>...]
       herdr-dispatch.sh --resume <worktree> [--pane <id>] --name <agent-name> --prompt-file <file> --kind <kind>
                         --run-dir <dir> -- <native agent args that resume the session>...
  --resume   start the agent again in the existing worktree under the same name, in --pane when that
             pane is still open on the worktree with no agent. The prompt file says it was interrupted.
  --run-dir  supervised mode: run inbox at <dir>/inbox.jsonl and a per-worker inbox for replies.
             Used by dispatch.sh.
  --inbox    legacy single-mailbox mode (no worker inbox, no supervisor wake).
EOF
  exit 2
}

REPO= BRANCH= BASE= NAME= KIND= PROMPT_FILE= INBOX= RUN_DIR= RESUME_WT= REUSE_PANE=
AGENT_ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --repo) REPO=$2; shift 2 ;;
    --branch) BRANCH=$2; shift 2 ;;
    --base) BASE=$2; shift 2 ;;
    --name) NAME=$2; shift 2 ;;
    --kind) KIND=$2; shift 2 ;;
    --prompt-file) PROMPT_FILE=$2; shift 2 ;;
    --inbox) INBOX=$2; shift 2 ;;
    --run-dir) RUN_DIR=$2; shift 2 ;;
    --resume) RESUME_WT=$2; shift 2 ;;
    --pane) REUSE_PANE=$2; shift 2 ;;
    --) shift; AGENT_ARGS+=("$@"); break ;;
    *) usage ;;
  esac
done
{ [ -n "$REPO" ] && [ -n "$BRANCH" ] || [ -n "$RESUME_WT" ]; } && [ -n "$NAME" ] && [ -r "$PROMPT_FILE" ] || usage

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
COMMON=$(git -C "${RESUME_WT:-$REPO}" rev-parse --path-format=absolute --git-common-dir) || die "${RESUME_WT:-$REPO} is not a git repo"
ROOT=$(dirname "$COMMON")
if [ -n "$RESUME_WT" ]; then
  WT=$(cd "$RESUME_WT" && pwd); BRANCH=$(git -C "$WT" branch --show-current)
else
  SLUG=$(printf '%s' "$BRANCH" | tr '/' '-')
  WT="$ROOT/_worktrees/$SLUG"
fi

# Local-only ignore; the repo's tracked .gitignore stays untouched.
mkdir -p "$COMMON/info"
grep -qxF '_worktrees/' "$COMMON/info/exclude" 2>/dev/null || echo '_worktrees/' >>"$COMMON/info/exclude"

# Slug to 28 chars so the `-N` uniqueness suffix below still fits herdr's 32-char name limit.
# A resumed agent keeps its name as it was.
[ -n "$RESUME_WT" ] || NAME=$(printf '%s' "$NAME" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]+/-/g; s/^[^a-z]+//; s/-+$//' | cut -c1-28)
[ -n "$NAME" ] || die "agent name is empty after slugging"
LIVE=$(herdr agent list | jq -r '.result.agents[]?.name // empty')
BASE_NAME=$NAME; N=2
# A resumed agent keeps its name, which is the key of its inbox.
if [ -n "$RESUME_WT" ] && printf '%s\n' "$LIVE" | grep -qxF "$NAME"; then die "agent $NAME is still running"; fi
while printf '%s\n' "$LIVE" | grep -qxF "$NAME"; do NAME="$BASE_NAME-$N"; N=$((N + 1)); done

WT_EXISTS=; git -C "$ROOT" worktree list --porcelain | grep -qxF "worktree $WT" && WT_EXISTS=1
[ -z "$RESUME_WT" ] || [ -n "$WT_EXISTS" ] || die "$WT is not a worktree of $ROOT"
NEW_BRANCH=
if [ -z "$WT_EXISTS" ] && ! git -C "$ROOT" show-ref --verify --quiet "refs/heads/$BRANCH"; then
  NEW_BRANCH=1
  if [ -z "$BASE" ]; then
    git -C "$ROOT" fetch --quiet origin 2>/dev/null || true
    BASE=$(git -C "$ROOT" symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null || echo HEAD)
  fi
fi

# The workspace open on the main checkout, which Herdr makes the parent of the worktree's group.
parent_ws() {
  herdr workspace list 2>/dev/null | jq -r --arg root "$ROOT" \
    'first(.result.workspaces[]? | select(.worktree.checkout_path == $root and .worktree.is_linked_worktree == false) | .workspace_id) // empty' 2>/dev/null || true
}
PARENT_BEFORE=$(parent_ws)

# A resumed agent goes back into its own pane when that pane is still open on the worktree, idle.
PANE= WS=
if [ -n "$RESUME_WT" ] && [ -n "$REUSE_PANE" ] && ! herdr agent get "$REUSE_PANE" >/dev/null 2>&1; then
  WS=$(herdr pane get "$REUSE_PANE" 2>/dev/null | jq -r '.result.pane.workspace_id // empty' 2>/dev/null || true)
  if [ -n "$WS" ] && [ "$(herdr workspace list 2>/dev/null | jq -r --arg w "$WS" \
      'first(.result.workspaces[]? | select(.workspace_id == $w) | .worktree.checkout_path) // empty')" = "$WT" ]; then
    PANE=$REUSE_PANE
  else
    WS=
  fi
fi

if [ -n "$PANE" ]; then
  :
elif [ -n "$WT_EXISTS" ]; then
  OUT=$(herdr worktree open --cwd "$ROOT" --path "$WT" --label "$NAME" --no-focus)
else
  ARGS=(--cwd "$ROOT" --branch "$BRANCH" --path "$WT" --label "$NAME" --no-focus)
  [ -z "$NEW_BRANCH" ] || ARGS+=(--base "$BASE")
  OUT=$(herdr worktree create "${ARGS[@]}")
fi
if [ -z "$PANE" ]; then
  [ "$(echo "$OUT" | jq -r 'has("error")')" = "false" ] || die "worktree: $(echo "$OUT" | jq -r .error.message)"
  PANE=$(echo "$OUT" | jq -r .result.root_pane.pane_id)
  WS=$(echo "$OUT" | jq -r .result.workspace.workspace_id)
fi

# Herdr also opens the main checkout as a parent workspace when none is open, and marks it
# opened_for_worktrees. The token stands in for that mark on a server that predates it.
if [ -z "$PARENT_BEFORE" ]; then
  PARENT=$(parent_ws)
  if [ -n "$PARENT" ] && [ "$(herdr workspace get "$PARENT" 2>/dev/null | jq -r '.result.workspace.opened_for_worktrees // false')" != true ]; then
    herdr workspace report-metadata "$PARENT" --source dispatch --token dispatch_parent=1 >/dev/null 2>&1 || true
  fi
fi

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

    $DISPATCH report --run $(basename "$RUN_DIR") --from $NAME --type <type> --key <key> --subject "<one line>" [--body-file <file>] [--outcome succeeded|failed]

Types:
- \`status\` - progress or a result; you keep working.
- \`question\` - you need an answer. See "Your inbox" below for how the reply arrives.
- \`escalation\` - blocked, a human is needed.
- \`worker_done\` - task complete or abandoned. Send exactly once, with \`--key done\`, \`--outcome succeeded\` or \`--outcome failed\` (never encode failure only in prose), and a three-sentence summary in --body-file. After sending it, stop and idle; do not start new work. A \`worker_done\` that ends the work of a follow-up takes \`--key done-<follow-up id>\`.

\`--key\` names one report: a short slug of what it says, such as \`tests-pass\` or \`pr-opened\`. A report with a key you already used is not sent again and prints the first message id. So when you are not sure a report went out, send it again with the same key; give each new report a new key.

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

LINE="Your task is in the file $TASK_FILE - read it now and carry it out in full."
[ -z "$RESUME_WT" ] || LINE="You were interrupted and this session is resumed. Read the file $TASK_FILE now, before anything else, and follow it."
for _ in 1 2 3; do
  START=$(herdr agent start "$NAME" --kind "$KIND" --pane "$PANE" --timeout 60000 -- "${AGENT_ARGS[@]}" "$LINE" 2>&1 || true)
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
  --arg status "$STATUS" --arg task "$TASK_FILE" --arg detail "$START" \
  '{backend:"herdr", agent:$agent, kind:$kind, workspace_id:$ws, pane_id:$pane, worktree:$wt,
    branch:$branch, repo_root:$root, inbox:$inbox, worker_inbox:$winbox, run_dir:$rundir,
    task_file:$task, status:$status}
   + (if ($status | startswith("start_failed")) then {detail:$detail} else {} end)'
case "$STATUS" in working|idle|done) exit 0 ;; *) exit 3 ;; esac
