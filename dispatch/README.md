# dispatch

Send a unit of work to another coding agent in its own git worktree, and supervise it from the session that sent it.

The plugin has two parts that version together:

- **A skill** (`skill/`) with the scripts a supervisor agent runs: `dispatch.sh` (runs, ledger, mailbox, fleet view) and its helpers.
- **An event hook** (`hook/`), a small Go binary that Herdr starts on pane events.

## How it works

- A worker reports with `dispatch.sh report`. The message goes to an ack-based mailbox file, which is the source of truth.
- For `question`, `escalation` and `worker_done` (and `status`, when the run is bound with `--wake`), the supervisor is woken by one line typed into its pane with `herdr agent prompt`: `MAIL|<id>|<type>|<from>|<subject>`. No watcher process is needed.
- The hook tells the supervisor when a worker stops at a dialog (`DISPATCH|blocked|...`) or its pane goes away with no `worker_done` (`DISPATCH|exited|...`).
- A wake is never typed into a pane the user has focused, because it would be appended to a half-typed draft. The user gets a toast, and the wake is delivered on the next focus move or supervisor state change (`DISPATCH|pending|...`).
- `dispatch.sh notify` and `dispatch.sh track` give any other process the same delivery, so a poll loop in a plain pane can wake the supervisor or, with `--to`, any other agent.
- A closed tab or workspace emits no pane event, so on those the hook checks every indexed pane against `herdr pane list`.
- `dispatch.sh ps` is the reconcile path. Herdr does not replay hook events after a restart.

`docs/overview.html` is a one-screen overview. `docs/architecture.html` has the step-by-step sequence diagram. `skill/SKILL.md` is the reference for agents.

## Requirements

- Herdr 0.9.1 or later
- `bash`, `jq`, `flock`, `git`; `inotifywait` for event-driven `wait` (falls back to polling)
- Go 1.22 or later to build the hook on install

## Install

```sh
herdr plugin install mago0/herdr-plugins/dispatch
root="$(herdr plugin list --plugin dispatch --json | jq -r '.result.plugins[0].plugin_root')"
ln -s "$root/skill" ~/.claude/skills/dispatch   # or your agent's skills directory
```

The skill and the hook share the pane index format and the wake lines, so keep the skill symlinked to the installed plugin and do not copy it.

## State

Everything is under `${XDG_STATE_HOME:-~/.local/state}/agent-dispatch/`:

- `runs/<run>/ledger.json`: dispatch attempts
- `runs/<run>/inbox.jsonl`: worker to supervisor mail
- `runs/<run>/workers/<agent>/inbox.jsonl`: supervisor to worker mail
- `panes.json`: pane id to run, role and supervisor. The hook reads only this file.

## Test

```sh
test/smoke.sh
```

Runs every `dispatch.sh` command that needs no live agent against a throwaway state directory.

## Limits

- A worker that hangs while `working` is not detected.
- A worker that blocks before the launcher returns is reported by `dispatch.sh start` (`status: blocked`), not by the hook.
- A held `DISPATCH|exited` wake for a `track --notify` target is not delivered later. Only held wakes for a supervisor are.
- Wake delivery was tested with Claude Code. Other agent kinds queue typed input in their own way.
