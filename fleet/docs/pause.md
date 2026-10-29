# Pause is not teardown

Pause stops autonomous work and wakeups while retaining agents, hierarchy, worktrees, mail and ownership. Resume is explicit. Release is a separate ownership/cleanup decision. `idle` and `done` do not prove pause.

```sh
fleet.sh pause --run PROJECT
fleet.sh pause-status --run PROJECT
```

Pause saves intent before contacting Herdr. Repeat it to recover an interrupted operation. The receipt distinguishes confirmed checkpoints, pending agents and failed topology/identity checks. Sent requests are not confirmations. A blocked agent or unsent user draft stays unconfirmed; do not answer dialogs or erase input to force delivery.

Fleet compares current, unreleased ledger ownership and expected edges with server coverage. A missing link cannot make an owned child disappear: fleet supplies those targets to the server fence without silently reparenting them. Missing members, identity drift, lost edges and attempts with unknown pane identities remain failed/unconfirmed; normal resume is rejected.

Herdr fences the subtree, retains supervision links, suppresses reports, suspends jobs and quarantines queued automation. Checkpoint requests proceed descendants first. Each agent must stop owned autonomous tasks and external wakes, write a durable checkpoint, then acknowledge the pause through the control command in its request. Do not report `worker_done` for a pause.

## Resume

Only after explicit user authorization, reconcile current identities, ownership, pull request (PR) state and saved job definitions. Record the decision:

```json
{"pause_id":"ID_FROM_PAUSE_RECEIPT","operation":"resume","authorization":"Matt requested resume of PROJECT","ownership_checked":true,"pr_state_checked":true,"jobs":["pr-watch-repo-123"]}
```

```sh
fleet.sh unpause --run PROJECT --authorization reconciliation.json
```

Approval is bound to that pause ID and operation, not future pauses or teardown. Only listed, still-applicable jobs resume. Recognized `pr-watch.sh OWNER/REPO NUMBER` watches are checked against GitHub and rejected unless the PR is still open. Other job applicability requires the recorded operator check. No blind delivery replay, duplicate job creation or clock-based resume. Independently paused descendant subtrees remain fenced. Work must be explicitly reissued after reconciliation; unpause does not invent or launch a new task.

For intentional teardown, use `fleet.sh pause-release --run PROJECT --authorization reconciliation.json` with that pause ID, `operation: "release"` and an empty `jobs` array, then use the existing worker cleanup commands. This can explicitly end an incomplete pause without claiming it was confirmed. It does not itself remove worktrees or settle workers. Old automation remains held until cleanup; a released pause cannot be resumed.

## Recovery and manual fallback

`runs/PROJECT/pause.json` is durable fleet control data. Keep it with the ledger and mail. On coordinator/server restart, inspect `pause-status`, then repeat `pause` to complete fencing. A failed server call leaves the fleet gate closed. Do not delete a record to bypass the gate. For an inspected replacement identity or lost expected link, use `herdr subtree reconcile --id ID --target PANE --authorization 'explicit user decision' --evidence CHECKPOINT`, descendants first. This records the decision and restores that pane's saved edge; it does not adopt new work. If a run's ledger changed intentionally, the authorization file must also contain `ownership: {"RUN": CURRENT_LEDGER_ARRAY}` matching its exact current contents. A boolean check alone cannot accept ownership drift.

An older server cannot provide a verified deterministic pause. Retain the local gate, report server coverage unconfirmed and arrange an authorized upgrade. A manual cooperative stop can reduce risk, but is not equivalent to server fencing. To suppress a link's reports without detaching its child:

```sh
herdr pane supervise CHILD --by PARENT --report ''
```

Never use `--clear`, release, stop, abandon, worktree removal or pane closure as pause mechanics. Record manual changes and saved definitions for reconciliation; keep mailbox contents intact.

## Boundary

See the implementation's server documentation for exact raw-command coverage. A server fence cannot undo bytes already sent to a terminal or stop an agent's current tool call, arbitrary external schedulers or application processes. Those require cooperative checkpoint confirmation. Native harness completion queues, such as Pi background notifications, are not intercepted by Herdr; a worker that cannot cancel or reconcile them must remain unconfirmed. Host-level process/terminal access is not a security boundary. Unconfirmed work stays visible; never infer complete coverage from user interface (UI) state.

Standalone installs need GNU `sync -f` for durable control writes; the Nix package supplies it.
