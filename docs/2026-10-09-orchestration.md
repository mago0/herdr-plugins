# Dispatch and tree on the Herdr orchestration primitives

Date: 2026-10-09. Branch `orchestration` in this repository and in `mago0/herdr`.

## Goal

The `dispatch` skill and the watch loops around it carried workarounds for five gaps in Herdr. The
fork now closes those gaps in the server. This change removes the workarounds from the plugins
and makes the tree read supervision from Herdr.

## What the server now gives

| Primitive | Command | Replaces |
|---|---|---|
| Delivery queue: a prompt typed only when the pane is safe | `herdr agent deliver` | `dispatch.sh notify` hold logic, the hook on `pane.focused`, `DISPATCH\|pending` |
| Jobs: a command on an interval, an event or a file change, with no pane | `herdr job add` | poll loops in plain panes, `herdr-loop.sh`, the `tree=hide` token on loops |
| Supervision link with reports | `herdr pane supervise` | `panes.json`, the Go event hook, `dispatch.sh track`, `DISPATCH\|blocked`, `DISPATCH\|exited` |
| Event history with a cursor | `herdr events read` | `dispatch.sh ps` as the only reconcile path |
| Stall detection and blocker text on the agent record | `agent.get` fields | reading a worker's terminal |
| Pane tokens kept across a restart | none | labels lost on restart |

## dispatch

- `dispatch.sh` types nothing itself. A wake line goes to `herdr agent deliver`, and a toast shows
  when Herdr holds it.
- `dispatch.sh start` makes the supervisor pane the supervisor of the worker pane, with reports
  for `blocked`, `exited` and `stalled`. A worker that reports `worker_done`, or an attempt that is
  stopped or abandoned, keeps its link and loses its reports. `send` turns the reports on again.
- The event hook, its Go module, `panes.json`, `track` and the `--tab` placement are gone.
- `--label` on `run` and `start` sets the row label. A name that starts with an issue key is split
  into label and name, so an agent name carries no issue key.
- `ps` has the liveness `stalled`.
- `adopt` links and labels the live workers of runs that the old version started.
- The mailbox, the ledger, retries and dependencies stay in files. They work, and moving them into
  the server is not planned.

Wake lines a supervisor can get:

```
MAIL|<id>|<type>|<from>|<subject> - handle it: dispatch.sh read --run <run> --id <id>
HERDR|blocked|<agent>|<pane>|<what the agent shows>
HERDR|stalled|<agent>|<pane>|<seconds without a screen change>
HERDR|exited|<agent>|<pane>
```

## tree

- The supervisor of a row is the agent's `supervisor_pane_id`. The dispatch state reader and the
  run-name ticket are gone; the label token is the only source of the text at the right edge.
- A tab or pane row shows the repository of its own checkout, from the agent's working directory.
- New on a row: a stalled state dot, a count of held prompts, and for a blocked row the blocker
  text on the last line of the pane.
- The menu of a blocked Claude Code row offers the numbered answers of its dialog.

## Answering a blocked agent from the row menu

Question of the spike: can the client offer an answer with keys that are right for the dialog?

Findings, from Claude Code 2.1.288 in an isolated session:

- A numbered dialog (permission prompt, `AskUserQuestion`) takes the digit of a choice as its
  answer. `herdr agent send-keys <agent> 2` picked the second choice.
- The folder trust prompt has no numbers and its default is "No, exit". Enter there closed the
  agent. A fixed key for "approve" is therefore not safe.

Design that follows: the tree reads the choices from the blocker text and offers them only when
they are numbered from 1 with no gap and the agent kind is one whose dialogs were checked
(`claude`). The client presses only one digit from 1 to 9 and refuses any other key a plugin
names. Choices that open a text field ("Type something", "Chat about this") are left out. A dialog
that fails those rules gets no answer items, and the user answers it in its pane.

## Migration on one machine

1. Build the fork from `orchestration` and make it the `herdr` on PATH for the server and the
   client. A running fork server can take the new build with `herdr server live-handoff
   --import-exe <new binary>`, which keeps the panes.
2. Point the `dispatch` and `herdr-tree` plugin links at this branch.
3. Run `dispatch.sh adopt` once.
4. Replace each watch loop with a job, then close its pane. Example, for a loop that polled a
   file and typed a line for each new entry:

   ```sh
   herdr job add soak-fix --path proposals.json --to <supervisor> -- \
     jq -r '[.[] | select(.status == "warranted") | .id] | if length > 0 then "SOAKFIX|" + join(",") else empty end' proposals.json
   ```

## Not done

- Mail and runs in the server.
- Answers for agent kinds other than Claude Code.
- A stall rule for an agent that animates while it waits.
