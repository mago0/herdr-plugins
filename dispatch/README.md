# dispatch

Supervise coding agents in [Herdr](https://herdr.dev) without polling them.

A supervisor agent hands work to worker agents, each in its own git worktree. When a worker finishes, has a question or gets stuck, the supervisor is woken by one line typed into its pane. Nothing in the supervisor waits, so there is no watcher to time out and re-arm, and it stays free while workers run.

Herdr gives you `agent prompt` and `agent wait`. This adds what long-running, parallel work needs on top of them: a durable mailbox, push wakes that will not land in a half-typed prompt, and a report when a worker blocks or disappears.

![Overview: agents call dispatch.sh, mail goes to state files, Herdr types one wake line into the target pane](docs/overview.png)

## Why not just `herdr agent prompt --wait`?

For a short exchange with an agent beside you, use it. It is simpler. It stops fitting when the work is supervised, long-running or parallel:

- **No watcher to re-arm.** A wait runs inside the supervisor's harness, and harnesses kill long tasks. Here the sender wakes the supervisor, so nothing in it is waiting.
- **The supervisor stays free.** No blocking wait and no idle token cost. One supervisor can hold many workers and hear from them in any order.
- **Real messages, not screen scraping.** Workers send typed mail (`status`, `question`, `escalation`, `worker_done` with an outcome). A wait only says an agent stopped, and reading its terminal can lose output.
- **Durable and acked.** Mail stays until it is acknowledged, so it survives a restart, a context compaction or a missed wake. Herdr events are not replayed.
- **It does not corrupt a half-typed prompt.** `herdr agent prompt` appends to whatever is in the input box and submits it. A wake is held while the target pane is focused or at a dialog; you get a toast, and the line is delivered when you move away.
- **Stuck and dead workers are reported at once**, including a closed tab or workspace, which emits no pane event in Herdr.
- **A lifecycle around each worker.** A worktree per task, a ledger of attempts, retries, dependencies, and an explicit keep or release decision at the end.
- **External polls move out of the harness.** A loop that watches a chat thread or a pull request runs in a plain pane with no time limit and wakes an agent only when something changed.

If you have used Orca's orchestration, this is that model for Herdr: runs, typed and acked messages, and a fleet view, with wakes delivered by push.

## What it looks like

The supervisor agent binds a run and starts a worker. In practice the agent runs these from `skill/SKILL.md`; you ask it to "dispatch" something.

```sh
dispatch.sh run --name fix-probes
dispatch.sh start --repo ~/src/api --branch claude/fix-probe-timeouts --name probes --task task.md
```

The worker opens in its own worktree and pane, with instructions on how to report. The supervisor goes back to whatever it was doing. Later this line is typed into its pane:

```
MAIL|msg_1791236483_162548|worker_done|probes|probe timeouts fixed, PR opened - handle it: dispatch.sh read --run fix-probes --id msg_1791236483_162548
```

The line is only a wake. The supervisor reads the message from the mailbox, checks the work, and settles it:

```sh
dispatch.sh read --id msg_1791236483_162548
dispatch.sh ack  --id msg_1791236483_162548 --decision release    # or retain, or reuse
```

Other lines it can receive:

| Line | Meaning |
|---|---|
| `MAIL\|<id>\|<type>\|<from>\|<subject>` | a worker reported; `question` gets `dispatch.sh reply` |
| `DISPATCH\|blocked\|<agent>\|<run>` | a worker stopped at a permission dialog or question |
| `DISPATCH\|exited\|<agent>\|<run>` | a worker's pane went away before it reported `worker_done` |
| `DISPATCH\|pending\|<run>` | wakes were held while you were in the pane; run `dispatch.sh ps` |

`dispatch.sh ps` shows every attempt with its liveness and the next command to run.

## Install

```sh
herdr plugin install mago0/herdr-plugins/dispatch
root="$(herdr plugin list --plugin dispatch --json | jq -r '.result.plugins[0].plugin_root')"
ln -s "$root/skill" ~/.claude/skills/dispatch   # or your agent's skills directory
```

Keep the skill symlinked to the installed plugin and do not copy it: the scripts and the hook share the pane index format and the wake lines.

Requirements:

- Herdr 0.9.1 or later
- `bash`, `jq`, `flock`, `git`; `inotifywait` for an event-driven `dispatch.sh wait` (falls back to polling)
- Go 1.22 or later to build the hook on install

## Waking an agent from your own loop

Chat threads and pull requests cannot push to your machine, so something has to poll them. Run that loop in a plain Herdr pane, not as a task inside an agent session, and let it wake the agent only when there is news:

```sh
dispatch.sh track  --pane "$LOOP_PANE" --name pr-watch       # tell the supervisor if the loop's pane goes away
dispatch.sh notify --line "PR 123 has a new push"            # exit 3 = held; retry on the next tick
dispatch.sh notify --to worker-1 --line "..."                # any agent, not only the supervisor
```

`notify` uses the same held-not-typed rule and stores nothing, so a loop that reports state on every tick needs no extra bookkeeping.

## Limits

- A worker that hangs while `working` is not detected.
- A worker that blocks before the launcher returns is reported by `dispatch.sh start` (`status: blocked`), not by the hook.
- A held `DISPATCH|exited` wake for a `track --notify` target is not delivered later. Only held wakes for a supervisor are.
- Wake delivery was tested with Claude Code. Other agent kinds queue typed input in their own way.
- Hook events are not replayed after a Herdr restart. `dispatch.sh ps` is the reconcile path.

## How it is built

Two parts that version together:

- **A skill** (`skill/`): `SKILL.md`, the reference agents read, and the scripts they run. `dispatch.sh` holds runs, the ledger, the mailbox and the fleet view.
- **An event hook** (`hook/`): a small Go binary that Herdr starts once per pane event. It reads only the pane index, exits at once for panes that are not part of a run, and otherwise reports a blocked or vanished worker or delivers a held wake.

State is under `${XDG_STATE_HOME:-~/.local/state}/agent-dispatch/`:

- `runs/<run>/ledger.json`: dispatch attempts
- `runs/<run>/inbox.jsonl`: worker to supervisor mail
- `runs/<run>/workers/<agent>/inbox.jsonl`: supervisor to worker mail
- `panes.json`: pane id to run, role and wake target

Diagrams, as standalone HTML to open locally: `docs/overview.html` (one screen) and `docs/architecture.html` (step-by-step flows).

## Test

```sh
test/smoke.sh
```

Runs every `dispatch.sh` command that needs no live agent against a throwaway state directory.

## State read by other plugins

The [tree](../tree/) plugin reads the state below and writes none of it. Treat a change to these names as a breaking change.

| File | Fields |
|---|---|
| `runs/<run>/supervisor` | the supervisor pane id |
| `runs/<run>/ledger.json` | `pane_id`, `tab_id`, `workspace_id`, `worktree`, `created` |
| `panes.json` | `role` (the value `tracked`) |

It also reads the run directory's name for an issue key.
