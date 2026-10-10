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

Design that follows, with the changes the review asked for:

- The tree reads the choices from the screen lines of the agent (`blocker_lines`) and offers them
  only when every numbered line runs from 1 with no gap or repeat, there are at most 9, the
  choices are near each other and near the last line, the dialog's cursor is on exactly one of
  them, and the agent kind is one whose dialogs were checked (`claude`).
- Choices that open a text field ("Type something", "Chat about this") are left out.
- The request names the dialog (`dialog` on the agent record). The client sends `agent.answer`
  with that name, and the server reads the screen again and refuses when the agent shows anything
  else by then. The name covers every line of the screen and the server's count of the agent's
  waits, so the same question asked a second time is another dialog. The server accepts one digit from 1 to 9 and no other key.
- The answers are never the first menu item, because Enter in a menu that just opened picks the
  first item.
- A dialog that fails those rules gets no answer items, and the user answers it in its pane.

## Review, 2026-10-09

An a-team review of both branches found no blocker, 6 high and 16 medium findings. Fixed after it:

| Finding | Fix |
|---|---|
| Delivery text could hold keys (paste end, carriage return) | Every delivery is filtered once in the server; `dispatch.sh` refuses a `--from` that is not a plain name and strips control characters from the subject |
| An answer could reach another dialog | `agent.answer` with the dialog name, checked against the screen at that moment |
| A job never ended when a process it left held its output | The command runs in its own process group, which is stopped when the command exits |
| A job could trigger itself | A job cannot run on `pane.delivery_changed`, starts at most once a second, and at most 64 deliveries wait for one pane |
| The interleave test could not fail | A test that delivers twice with no wait between |
| `release` of an old tab-placed worker aimed at the supervisor's workspace | `release` refuses such an entry and prints the commands to run by hand |
| A delivery could be typed on top of unsent text, or while a working agent opens a dialog | Typed only when the agent waits for input and no typed text is unsent |
| An unreadable state file was overwritten | It is renamed aside; the file is private and synced |
| Answers were the first menu item | They are below the row's own items |
| The parser could map numbers to the wrong choices | Screen lines, one cursor mark, no gap or repeat |
| `events.read` gave no safe cursor for a cut page | The reply has `next` |
| A group close reported the panes of one workspace | Every pane of the group is reported |
| `agent start` wiped an existing link | It leaves a link that exists |
| `adopt` could take a name a later run owns | It matches the pane in the ledger, and does not watch a worker that said it is done |
| The label split could give an unusable name | A name whose rest does not start with a letter stays whole; `send` refuses an agent that is not in the run |

A second pass by the same reviewers found one high and 12 medium findings. Fixed after it:

| Finding | Fix |
|---|---|
| One typed character that was erased held a pane's deliveries without end | The server counts typed and erased characters; text nobody touched for 10 minutes no longer holds; keys at a dialog do not count; a delivery shows why it waits (`held_by`) |
| Shift+Enter, Ctrl+D, or a state report from a working agent cleared the mark while text was there | Only plain Enter, Ctrl+C, Ctrl+U and the start of the agent's work clear it |
| A client attached to one terminal bypassed both guards | Its keys feed the same mark, and the terminal counts as focused while it is attached |
| A dialog name covered only the last 16 lines | It covers the whole screen and the count of waits |
| Numbered lines that were not the dialog's choices could be offered | The choices must be one list near the end of the screen, and a list of ten or more is refused |
| A save inside the save interval made the tick run on every loop pass | The save waits for the tick |
| A supervisor that was slow to read could lose `exited` behind 64 older reports | A newer report replaces a waiting one of its kind, and `exited` replaces all for that pane |
| News a full queue refused was never delivered | News counts as told only when it is queued |
| A saved file skipped the checks of a request | Loaded deliveries are filtered and capped, loaded jobs are validated |
| Running job commands outlived the server | They are stopped at the end of the server and at a handoff |
| Output printed before a process that left the group held the pipe was lost | The reader shares what it has read |
| A file that was not text was overwritten | It is renamed aside like one that does not parse |
| `adopt` took the done mail of an earlier attempt, and stopped at a bad inbox line | Only mail since the attempt started counts, and a bad line is passed |
| `stop` of a tab-placed worker said it stopped | It refuses and names the manual steps |
| Tests that could not fail | Each fix has a test that fails when the fix is removed |

Known limits that stay: a delivery is typed at least once; the unsent-text mark is an estimate
from keys; events that come inside one second merge into one job run; the report text after
`HERDR|blocked` is screen text and a reader must treat it as data.

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
