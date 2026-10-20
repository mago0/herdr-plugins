# SRE sidebar design

Date: 2026-10-19. Status: approved design, not built.

## Goal

Replace the supervisor tree in the `herdr-tree` sidebar pane with a view built around how SRE work runs:

- One workspace is one effort: a ticket or a Linear project, worked by a parent agent, often from the `~/Projects/flosports` folder and across many repos.
- An elaborate subtask goes to a worker agent in its own workspace and worktree. The worker reports its result back to the parent agent.
- Efforts must stay visible, including the shells beside an agent and the workers of each effort.

Supervisor chains are one level deep in practice. The tree's deep nesting, tab rows and groups do not add value, and the tree hides panes that have no agent.

## Decisions

| Topic | Decision |
|---|---|
| Unit | One entry per workspace (effort). No tab rows. |
| Panes | Every pane of an effort shows: shells first, then each agent followed by its workers. |
| Workers | Nest under the agent that dispatched them. Not shown again as an effort. |
| Order | Herdr's workspace order. No numbers. |
| Attention | No summary bar. The toast and the state dots carry it. |
| Fold | By hand, remembered across restarts. A new effort starts open. |
| Effort tag | From the parent agent's own `label` and `link` tokens. An issue shows `SRE-923 ↗`, a Linear project shows `proj ↗`. |
| Agent text | The session title, with the agent kind as fallback. Workers too. |
| Shell text | The pane label when set, else the cwd. |
| Worker location | `repo@branch` with a branch glyph for a linked worktree. A main checkout gets no glyph and no warning. |
| Steps | Herdr's next/previous workspace keys step over agent and worker rows. |

## Mockup

Live data from 2026-10-19, plain theme. Effort tags assume the label rule in "Follow-ups".

```
 ▾ Main
     flosports@main
     ● Herdr left pane view layout mockup
     ○ Infra review conventions update
     ├ ○ review-continue-watching-79     #79
     │    continue-watching@effect-local-ex…
     │   $ reviewr
     └ ○ Review infra base services    #1102
          infra-base-services@jordangarri…
         $ reviewr

 ▾ GKE Migration                      proj ↗
     flosports@main
     $ Status
     ○ HANDOFF.md resume
     └ ○ SRE-194 job scheduler      SRE-194
          job-scheduler@sre-194-watchdog-n…
         $ shepherd

 ▸ Core PHP8.3 Upgrade  ✓ ●       SRE-923 ↗
```

Glyphs: `●` state dot (color by state), `$` shell, `` linked worktree, `▾`/`▸` fold.

## Model

The pane builds these nodes from Herdr's state:

- **Effort**: one for each workspace, in Herdr's order. It has the workspace name, its `repo@branch` and ahead/behind, and the `label` and `link` of its first top-level agent. A `linear.app/<org>/project/...` link is a project; another link is an issue.
- **Shell**: a pane with no agent. It has the pane label, else the cwd: relative to the repo root inside a repo, from `~` outside.
- **Agent**: a pane with an agent. It has the session title (else the agent kind), state, and the stall, pending and blocker marks the tree reads now.
- **Worker**: an agent whose supervisor link points at an agent in another workspace. It hangs under that agent with the shells of its own workspace. It has `repo@branch`, the worktree flag, ahead/behind against `origin/HEAD`, `label`, `link`, and `reported`.

Rules:

- Within an effort: shells first, then each agent followed by its workers.
- A worker with its own workers nests one more level in the same way.
- A supervisor link between two agents of one workspace does not nest. Both stay agent rows.
- A workspace that holds only workers is not an effort when all its agents hang under another effort. A workspace with any top-level agent or only shells is an effort.
- `reported`: the dispatch ledger has an acked `worker_done` for the worker. It clears when the worker goes back to `working`.
- A hidden agent is not drawn.

Sources: the Herdr snapshot the tree reads now (workspaces, panes, agents, supervision links, tokens), `git` for branch and ahead/behind, and the dispatch ledger (new).

## Rendering

| Row | Line 1 | Line 2 |
|---|---|---|
| Effort | fold mark, bold name, right edge: `KEY ↗`, `proj ↗` or nothing | dim `repo@branch -n +n` |
| Shell | dim `$` glyph and label or cwd | none |
| Agent | state dot, session title | none |
| Worker | branch line from the agent's dot, state dot, session title, right edge: label, else `#PR` from a GitHub link | worktree glyph if linked, `repo@branch -n +n`, `reported` |
| Worker shell | `$` glyph at the worker's text column | none |

- The branch line (`├`, `└`, `│`) starts under the parent agent's dot.
- Dots, colors, stall, pending, blocker marks and glyph config come from the current theme.
- A blank line separates efforts.
- A folded effort is one line: fold mark, name, one dot for each agent and worker, the right-edge tag. Shells add no dot. A folded effort with only shells shows `$`.
- Narrow pane: cut the title first, then the branch from the right. Never cut the right-edge tag or the ahead/behind counts.
- A right-edge tag with a link is underlined, as now.
- Removed: tab rows, the "No agent" and "Hidden" groups, the attention filter.

## Interaction

Mouse:

- Click: an effort row focuses its workspace. Any other row focuses its pane.
- Ctrl+click: open the row's link (`↗` of an effort, tag of a worker).
- Right-click: Herdr's menu for the row: workspace menu on an effort, pane menu on the others. `Label...` stays on agent and worker rows, and the blocker answers stay on a blocked agent.
- Drag an effort row onto another to reorder workspaces. Pane rows do not drag.
- Click the fold mark to fold or open an effort.

Keys (sidebar focused): `j`/`k` move, `enter` focuses, `space` folds, `o` opens the link, `shift+up`/`shift+down` move an effort.

Steps (F20/F19 from Herdr's next/previous workspace): go to the next or previous agent or worker row. Skip shells, effort rows and the rows of a folded effort.

## Testing

- `internal/model`: build tests from snapshot fixtures: shells and an agent in one workspace, a worker in another workspace, a worker in a main checkout, a same-workspace supervisor link, a worker with its own worker, a shell-only workspace, project and issue links, `reported` from a ledger fixture.
- `internal/view`: plain-theme render tests at 44 and 30 columns: order, branch line under the agent's dot, folded dots, cut order, right-edge tag kept.
- Program: steps (skip shells, effort rows, folded rows), click targets, fold mark, drag to reorder.
- Live: `tree --once` against the running session, compared with the mockup.

## Rollout

`go build -o bin/tree ./cmd/tree`. The fork client restarts the sidebar pane when the binary changes. No config change. The tree stays in git history.

## Follow-ups

1. `herdr-tree` skill: when work starts on an issue or project, set `link` (issue or project URL) with `label` (issue key, none for a project) on the agent's own pane. Update its wording from "tree" to the new view.
2. Dispatch: no change expected. Confirm the ledger location and that the pane can read the `worker_done` ack without a lock before the model work starts.
