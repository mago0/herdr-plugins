---
name: herdr-tree
description: Label this agent's row in the Herdr sidebar tree with the ticket it works on. Use when work starts on a ticket or issue in this session ("let's get started on SRE-923", "pick up this ticket", "work on issue 42"), when the session moves to a different ticket, and when the user says "label this agent", "set the label", or "clear the label". Requires HERDR_ENV=1.
---

# Herdr tree label

The Herdr sidebar tree shows one row for each agent. A row can carry a short label at its right edge, such as a ticket key. The label is the `label` token of the agent's pane. Set it on your own pane so the user can see which ticket each agent has.

Check `test "${HERDR_ENV:-}" = 1` first. If it fails, do nothing: there is no tree to label.

## Set the label

Do this as soon as the ticket is known, before other work on it:

```bash
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --token label=SRE-923
```

- Use the ticket key alone, in the form the tracker uses (`SRE-923`, `#42`). The tree shows at most 12 characters.
- A session has one label. A new ticket replaces the old label with the same command.
- With no ticket, set a label only when the user asks for one.

## Clear the label

When the user asks, or when the session stops work on the ticket and takes up work that has none:

```bash
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --clear-token label
```

## Rules

- Label only your own pane, `$HERDR_PANE_ID`. A label on another agent's pane is the user's to set, from the row's right-click menu.
- Keep `--source tree`. The menu clears and replaces the label under that source.
- Do not report the label to the user as a step. It is bookkeeping.
