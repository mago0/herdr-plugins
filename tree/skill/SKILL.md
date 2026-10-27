---
name: herdr-tree
description: Label this agent's row in the Herdr sidebar with the ticket or project it works on, and link the row to it. Use when work starts on a ticket, issue or project in this session ("let's get started on SRE-923", "pick up this ticket", "work on the GKE migration project"), when the session moves to different work, and when the user says "label this agent", "set the label", or "clear the label". Requires HERDR_ENV=1.
---

# Herdr sidebar label and link

The Herdr sidebar shows each workspace as an effort. The right edge of the effort row comes from its first agent's pane: the `label` token (an issue key) and the `link` token (the web address of the issue or project). An issue shows `SRE-923 ↗`, a Linear project shows `proj ↗`. A click with ctrl opens the link.

Check `test "${HERDR_ENV:-}" = 1` first. If it fails, do nothing.

## Set them

Do this as soon as the work is known, before other work on it.

For an issue, set the key and the issue's address:

```bash
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --token label=SRE-923 --token link=https://linear.app/flosports/issue/SRE-923
```

For a project, set only the project's address and clear any old label. A project has no key, and the workspace name already names it:

```bash
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --clear-token label --token link=https://linear.app/flosports/project/gke-aws-eks-migration-f425468449cd
```

- Take the address from the tracker, not from memory. For Linear, an issue is `https://linear.app/<org>/issue/<KEY>`. A project's address has a slug; read it from the API (`projects(filter: {name: {contains: "..."}}) { nodes { name url } }`).
- Use the key alone for the label, in the form the tracker uses (`SRE-923`, `#42`). The sidebar shows at most 12 characters.
- A session has one label and one link. New work replaces both with the same command.
- With no ticket or project, set them only when the user asks.

## Clear them

When the user asks, or when the session takes up work with no ticket or project:

```bash
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --clear-token label --clear-token link
```

## Rules

- Set them only on your own pane, `$HERDR_PANE_ID`, and put the pane id first, before the options. A label on another agent's pane is the user's to set, from the row's right-click menu.
- The source names only the writer. A pane has one `label` and one `link`, and the last write wins, also over the row's menu.
- Do not report this to the user as a step. It is bookkeeping.
