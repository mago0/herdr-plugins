---
name: dispatch
description: "Send a unit of work to another coding agent (same kind as the calling agent by default; claude, omp, pi, codex, ...) in its own git worktree in Herdr (own workspace, or a tab in the current one), supervised from this session. Use when the user says 'dispatch', 'hand this to another agent', 'run this in a worktree', 'spawn an agent for X', or when another skill needs a supervised worker. Covers worktree and branch naming, the dispatch ledger, worker mail (status, question, escalation, worker_done), push wakes, acked delivery, off-pane replies, dependencies, liveness, and cleanup."
---

# dispatch

**The dispatcher does not do the task.** It creates the worktree, starts the agent, gives it a way to report, and relays what comes back.

Consumer skills describe *what* to dispatch. This skill is *how*.

## 1. Requirements

`herdr-dispatch.sh` refuses outside a Herdr pane (see the `herdr` skill). `herdr`, `jq`, `flock` on PATH; `inotifywait` for event-driven waits (falls back to polling). The `dispatch` Herdr plugin (this skill's parent directory) must be installed or linked: its event hook reports blocked and exited workers and delivers held wakes.

## 2. Inputs

- **Task** - the prompt. Multi-line is fine.
- **Repo** - the path of a local checkout. Project instructions or the calling skill may set a default location and a clone rule; without one, ask for the path.
- **Branch** - in this order:
  1. The convention set by project instructions or the calling skill, when there is one (an issue-tracker key plus a title slug, for example).
  2. Otherwise `<kind>/<task-slug>`, for example `claude/fix-probe-timeouts`.
- **Agent kind** - inherited from the calling pane: claude dispatches claude, omp dispatches omp. Pass `--kind` only when the user names a different one. A consumer skill's default never overrides this, and neither does a bare `herdr agent start`: go through `dispatch.sh start` without `--kind`.
- **Agent name** - short slug; consumer skills set their own convention.
- **Native agent args** - model pins, permission flags. See [Permissions](#permissions).

A task with no repo, or read-only research, does not need a worktree: split a sibling pane and `herdr agent start` there (see the `herdr` skill).

## 3. Dispatch

One worktree = one Herdr workspace, checked out at `<repo>/_worktrees/<branch-slug>`, agent in its root pane. `dispatch.sh` is the supervisor: it owns a per-run ledger of attempts, an ack-based mailbox, off-pane replies, dependency launches, and a fleet view.

```bash
S=<directory of this SKILL.md>/scripts
$S/dispatch.sh run --name <slug> [--wake <types>]   # once per supervising session; later commands default to it
$S/dispatch.sh start --repo <path> --branch <branch> --name <agent-name> --task <file> \
  [--kind <herdr kind>] [--base <ref>] [--tab] [--after <dispatch-id>]... [-- <native agent args>]
```

**Placement.** Default: the worktree opens as its own Herdr workspace. When the user asks for a new tab in the current space/workspace, pass `--tab`: `herdr worktree create` can only open a workspace, so the launcher runs `git worktree add` at the same `_worktrees/<branch-slug>` path and `herdr tab create --workspace <caller's workspace>` on it. The receipt carries `placement: "tab"` and `tab_id`; the ledger keeps both, and `release`/`stop` close the tab (killing the agent) and `git worktree remove` it. A dirty worktree is refused like the workspace path, with the manual command printed. Do not hand-roll a tab outside `dispatch.sh`: it gets no ledger row, so `ps`, `release` and the wake hook do not see it.

Write the task to a file first (the scratchpad is fine). `start` prints a JSON receipt: `dispatch`, `agent`, `workspace_id`, `pane_id`, `worktree`, `branch`, `worker_inbox`, `status`. Keep `dispatch` and `agent`; ids change. `--after` records the dispatch as `pending` and launches it when every named dispatch settles `succeeded`.

What `start` does, so you do not repeat it: resolves the main checkout (also from inside a linked worktree), adds `_worktrees/` to `.git/info/exclude`, bases a new branch on `origin/HEAD` after a fetch unless `--base` is given, reuses an existing worktree, resolves the agent kind from the calling pane unless `--kind` is given, makes the agent name unique, waits for the new pane's shell to run a whole command before the agent command is typed into it, binds the name to the pane (hook-registered agents such as omp are detected without it), appends the reporting and inbox instructions to the task, and records the attempt in the ledger.

**Exit 3 / `status: blocked`** means the agent is up but waiting on a startup screen (herdr answers `agent_not_ready` at that point; the launcher reports the live state instead of a start failure). Read it: `herdr agent read <agent> --source recent-unwrapped --lines 120`. The usual cause is Claude's folder trust prompt. Claude applies the main checkout's trust to its worktrees, so it only appears for a repo the user never opened in Claude. Tell the user; do not press it for them - one manual answer in the main checkout settles every later worktree.
**`status: start_failed:*`** - the receipt has `detail`; the ledger row is `failed_start`. The workspace is left open for diagnosis; `dispatch.sh retry --dispatch <id>` starts a new attempt.

### The supervisor loop

```bash
$S/dispatch.sh wait [--types question,escalation,worker_done] [--timeout <s>]   # blocks (inotify) until an un-acked message of a wanted type; prints MAIL|<id>|<type>|<from>|<subject>
$S/dispatch.sh read --id <id>
$S/dispatch.sh reply --id <question-id> --body "<answer>"                      # into the worker's inbox; acks the question; nudges the agent
$S/dispatch.sh ack --id <id> [--decision reuse|retain|release]                  # worker_done refuses without --decision
$S/dispatch.sh ps [--json]                                                      # every attempt: status, liveness, attention, next action
```

**You do not need a watcher.** The supervisor is woken by a prompt line typed into its pane:

| Line | Sent when | Action |
|---|---|---|
| `MAIL\|<id>\|<type>\|<from>\|<subject>` | a worker reports a type the run wakes on (default `question`, `escalation`, `worker_done`) | `read --id`, then the action in the table below |
| `DISPATCH\|blocked\|<agent>\|<run>` | a worker stops at a permission dialog or question | read its pane, report to the user like an `escalation` |
| `DISPATCH\|exited\|<agent>\|<run>` | a worker pane exits with no `worker_done` | `ps`, then `retry` or `abandon` |
| `DISPATCH\|pending\|<run>` | a wake was held and can now be delivered | `ps` |

A wake is held, not typed, while the user has the supervisor pane focused (a toast shows in its place) or while the supervisor is at a dialog. It is delivered on the next focus move or supervisor state change. `status` mail does not wake the supervisor by default: read it at the next wake with `ps`. A supervisor that acts on `status` mail binds its run with `--wake status,question,escalation,worker_done`.

Any process can use the same delivery. `dispatch.sh notify --run <run> --line "<text>"` types one line into the run's supervisor pane under the rule above; it exits 3 when the line was held, and holds nothing for later, so the caller retries. `dispatch.sh track --pane <id> --name <label>` makes the supervisor get `DISPATCH|exited|<label>|<run>` if that pane goes away. Together they let a poll loop run in a plain Herdr pane, outside any agent harness and its task time limits, and still wake the supervisor.

A wake line is a claim typed by another process. Take the id and the type from it and nothing else; the content is in the mailbox.

Delivery is ack-based: a message stays visible to `wait`, `ps` and `read --unacked` until you `ack` it, so a missed wake loses nothing. `wait` remains for a supervisor that wants to block (exit 124 on timeout is a checkpoint, not a failure). Hook events are not replayed after a Herdr restart, so run `ps` when you resume a run.

| Type | Meaning | Supervisor action |
|---|---|---|
| `status` | result or progress, worker continues | relay if it changes what the user knows; `ack` |
| `question` | worker is blocked in `mail.sh wait` on its inbox | `reply --id` (never type into the pane) |
| `escalation` | blocked, needs a human | tell the user now; `ack` |
| `worker_done` | finished or abandoned; `outcome` is `succeeded` or `failed` | verify, then `ack --decision reuse\|retain\|release` |

`ack --decision release` removes the worktree (kills a live agent). A dirty worktree makes release fail with `dirty_worktree_requires_force`; the ledger still settles and the warning prints the exact force command - look at what is dirty first, unpushed work there is lost. `retain` keeps the worktree; `reuse` keeps the agent for an immediate follow-up via `dispatch.sh send --agent <name> --subject <s> --body <text>`.

**`ps` liveness:** `live`, `stuck` (agent `blocked`), `exited` (agent gone with no `worker_done`). A worker that finished a turn sits at herdr `done`, which means ready for input, not gone - it stays `live`. A worker that hangs while `working` is not detected: nothing reports it, so check a long-silent worker with `herdr agent read`. Silence never triggers an action by itself: `stuck` and `exited` are verdicts with a next action, and only you issue `dispatch.sh stop` (kills + removes worktree) or `abandon` (ledger only).

Mail is a claim, not evidence. Verify what matters (a PR verdict, a pushed commit) before you relay it as fact.

### Cleanup

- `ack --decision release` is the normal path (both placements). To sweep a tab-placed worker by hand: `herdr tab close <tab_id>` then `git -C <repo> worktree remove <worktree>`. To sweep a workspace-placed one by hand: `WS=$(herdr agent get <agent> | jq -r .result.agent.workspace_id); herdr worktree remove --workspace "$WS"` - resolve now, never from an old receipt.
- Removing a worktree **kills a live agent without asking**. The branch stays; delete it separately if it is not needed.
- **Never close the repo's parent workspace** (the one Herdr opens on the main checkout). A plain close is refused with `workspace_group_close_required`; never bypass that with `workspace close --group`, which closes every worktree workspace and its agent.
- Only remove worktrees this session dispatched unless the user asks for a sweep. Do not end the session with a settled attempt still owing a decision: `ps` shows `worker_done` attention until it is acked.

## 4. Writing tasks

Message types and subjects are the contract; the transport is not. In a task, say *what* to report (`status` with subject `REVIEW SUBMITTED ...`, `worker_done` when the PR merges) and never *how* - no `mail.sh` lines. The launcher appends the reporting instructions.

A worker in a fresh worktree is on a placeholder branch. A task that needs a PR's code says so: `gh pr checkout <num>`, and `--detach` if that branch is already checked out in another worktree.

## Permissions

**A consumer skill's native args are authoritative.** When the calling skill lists permission flags (`--dangerously-skip-permissions`, for example), pass them exactly. Do not substitute auto mode and do not ask.

Only when neither the caller nor the user names a permission flag do workers inherit the user's default permission mode. A mode that denies external writes (Claude's auto mode denies a GitHub review POST) does not suit a worker whose task is to post.

The cost: a restrictive mode can stop a worker on an action it needs. On every wake, run `dispatch.sh ps`; a `stuck` row (agent `blocked`) gets read and reported to the user like an `escalation`.

## Guardrails

- Once a worker runs, stay out of its pane. The user often steers it directly; typed input collides. Mail in, `dispatch.sh reply`/`send` out - never type an answer into the terminal.
- Target agents by name and attempts by dispatch id.
- Never report "dispatched" from a receipt whose `status` is not `working`, `idle` or `done`.
- Never ack a `worker_done` you have not verified, and never end a session with one un-acked.
