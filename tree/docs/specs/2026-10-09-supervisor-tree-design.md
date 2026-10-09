# tree: a supervisor tree of Herdr agents

Status: design, not implemented. Date: 2026-10-09.

## Problem

Herdr's sidebar groups workspaces by repository: a worktree workspace is drawn under the workspace of its main checkout. Supervised agent work does not follow that shape. One supervising agent dispatches workers into several repositories, a worker can dispatch workers of its own, and one workspace can hold more than one supervising session in separate tabs. In the sidebar those workers are spread across repository groups with nothing that ties them to the agent that started them. With 20 or more workspaces it is easy to lose a worker or to assign it to the wrong parent.

A Herdr plugin cannot change this. Verified on Herdr 0.9.3:

- Plugin panes have five placements (`overlay`, `popup`, `split`, `tab`, `zoomed`). There is no sidebar slot.
- `workspace.move` changes the order of the workspace list, but the sidebar still draws a worktree workspace under its repository. No configuration option turns this off.
- Workspace metadata tokens do render in sidebar rows, so the sidebar can carry a label. It cannot carry structure.

## Goal

A pane that shows every agent as a tree by supervisor, with the state of each, and jumps to the selected agent.

Success is: from one view, the user can see which agent started each worker, see any worker that needs attention, and move to it in two keystrokes.

## Non-goals (v1)

- A supervisor label in the native sidebar.
- Manual assignment of a parent.
- A link between runs started by other plugins (scheduled automations, for example) and a supervisor.
- Rows for plain terminal panes. The tree is a view of agents.
- Any write to dispatch state or any change to Herdr layout, apart from the focus call on jump.

## Data model

### Node

One pane that hosts an agent, from `agent.list`. Its state is that agent's `agent_status`: `blocked`, `working`, `done`, `idle`, `unknown`.

A pane with no agent is not a node and has no row.

### Panes that do not count

Two kinds of agent pane are left out before the tree is built. They have no row and no dot.

1. **Loops that dispatch tracks.** An entry in the dispatch pane index (`<state>/agent-dispatch/panes.json`) with `role: "tracked"`. These are poll loops that report as agents so that a supervisor hears when they stop. No setup is needed.
2. **Panes that register themselves.** A pane whose metadata has the token `tree=hide`. Any script can set it on its own pane:

   ```sh
   herdr pane report-metadata "$HERDR_PANE_ID" --source <id> --token tree=hide
   ```

   Tokens are read from `pane.list`; `agent.list` does not return them.

The tree shows the number of panes left out as a last line, `Hidden (n)`, so a pane that set the token by mistake does not go away without a trace. The line unfolds to list them.

### Parent

`<state>` is `$XDG_STATE_HOME`, default `~/.local/state`. The dispatch plugin keeps, for each run, a ledger at `<state>/agent-dispatch/runs/<run>/ledger.json` and the supervisor's pane id in `<run>/supervisor`.

For each ledger entry, the worker pane is the entry's `pane_id`, and its parent is the run's supervisor pane.

Rules:

- The supervisor must be a live agent pane. There is no fallback for it: a worker is never shown under an agent that did not start it.
- A worker pane id that is no longer an agent pane falls back to the first agent pane of its tab (`tab_id`). An entry with no `tab_id`, a worker placed as a workspace, then falls back to the first agent pane of its workspace. This keeps a worker in place when its agent is started again in a new pane.
- An entry is ignored when the worker or the supervisor cannot be resolved to a live agent pane, or when both resolve to the same pane.
- When several entries resolve to the same pane, the entry with the latest `created` time decides.
- The `released` field is not used. A worker that is still open is still shown under its parent.
- A parent link that would form a cycle is dropped, and the node becomes a root.

A pane with no ledger entry has no parent. There is no "unowned" group: an agent nobody dispatched is a supervisor that has no workers yet.

Depth is not limited. A worker that binds its own run and dispatches is a parent like any other. Verified with a three-level dispatch.

### Rows for tabs and workspaces

The same rule applies at the tab level and at the workspace level.

A **head** of a tab is an agent pane in that tab whose parent is not in the same tab. A head of a workspace is a tab row whose parent is not in the same workspace.

- **One head.** The outer row stands for it. A tab with one agent is one row with the tab's label. A workspace with one such tab is one row with the workspace's label. No extra level.
- **Several heads.** The outer row is a container with the outer label, and each head is a row under it. If one of the heads has a parent outside (it was dispatched), the container takes that head's place under the supervisor.
- **No heads.** A workspace with no agent pane goes to the `No agent` group.

Results in practice:

- A workspace with one agent is one row.
- A worker placed as a tab in its supervisor's workspace is a row under the supervisor, marked as a tab.
- A workspace with two supervising sessions in two tabs is a container with two tab rows, each with its own workers.
- A tab with two agents is a container with two pane rows.

### Order

Groups, in this order:

1. Roots with children.
2. Roots with no children.
3. `No agent (n)`: workspaces with no agent pane. One folded line, closed by default. In practice these are the main-checkout workspaces that Herdr keeps as repository parents. A workspace leaves this group when an agent starts in it.
4. `Hidden (n)`: panes that do not count. One folded line, closed by default.

Inside a group, and among children, rows are in Herdr's order: workspace number, then tab number, then pane order.

### Labels

- A row that stands for a workspace: the workspace label. For a tab: the tab label.
- A pane row: the agent's name. With no name, the agent kind when it is specific (a loop's label, for example). With a generic kind (`claude`, `codex` and similar), the terminal title, cut to fit.
- **Right column.** On a root, the ticket key. On any other row, the repository: the main checkout named by the ledger entry's `worktree` path, else the repository of the row's workspace.
- **Ticket key.** Display only; it never decides a parent. A key is the first match of `[A-Za-z]{2,6}-[0-9]{1,5}` in a run name. A root shows a key only when every run it supervises yields the same key. A supervisor that runs work for several tickets shows none.

## Display

```
 ▾ ● ops
   ├─ ● review monitor ⇥                     ops
   │  ├─ ● review-86                         iam
   │  └─ ● review-234                        billing
   └─ ● planning ⇥                           ops
      ├─ ● api redesign notes ›              ops
      │  └─ ● add-probe                      api
      └─ ● scratch questions ›               ops
 ▾ ● platform migration
   ├─ ● add-vpc-routes                       infra
   ├─ ● migrate-scheduler                    scheduler
   │  ├─ ● build-images ⇥                    scheduler
   │  └─ ● add-nodepools ⇥                   infra
   └─ ● add-workload-identity                infra
 ▾ ● php upgrade                             ABC-923
   └─ ● load-test                            loadtest
   ● refactor-notes
 ▸ No agent (8)
 ▸ Hidden (7)
```

- Columns: guide lines, state dot, label, kind glyph, right column.
- **Guide lines.** A row below a root is joined to its parent by box-drawing lines, in the style of Herdr's sidebar: `├` or `└` for the branch, `│` where a line runs past the row to a later sibling. The branch sits under the parent's dot. Lines are dim.
- **Fold mark.** A root with children has `▾` (open) or `▸` (folded) before its dot. Below a root an open parent has no mark, since its children are in view. A folded one has `▸` in place of the `─` of its branch, so dots stay in one column.
- The dot is the only state indicator: `●` in the state color, and `·` for `unknown`, as the sidebar draws it. No state text and no time-in-state. A container's dot is the state Herdr reports for that tab or workspace.
- **Kind glyph.** A row that is a tab inside its parent's workspace, or a tab under a container, has a tab glyph after its label. A pane row has a pane glyph. A row that stands for a workspace has none. Both glyphs are configuration values. The defaults are Nerd Font icons (`nf-md-tab`, `nf-fa-terminal`); the README gives `⇥` and `›` for a plain font.
- A folded parent shows one dot for each descendant after its right column, in tree order. An open parent shows no descendant dots: its rows are visible.
- Root labels are bold. The right column, the glyphs and the guide lines are dim.
- A label that does not fit is cut with `…`. The right column is kept.
- The list scrolls when it is longer than the pane.

**Colors.** The socket API does not expose the theme or the state colors, so the plugin carries its own palette: Catppuccin Mocha values by default. The `unknown` color and glyph were read from a real sidebar; the other four are checked by eye against the sidebar before release. Each color and both kind glyphs can be set in the plugin's `config.toml`.

## Interaction

| Key | Action |
|---|---|
| `↑` `↓`, `k` `j` | Move the selection. |
| `enter` | Focus the selected agent's pane and close the tree. On a container, focus its tab or workspace. |
| `space` | Fold or unfold the selected parent. |
| `a` | Toggle the attention filter: show only rows that are `blocked` or `done`, with their ancestors. |
| `q`, `esc` | Close. |

Fold state is stored by pane, tab or workspace id in the plugin's state directory (`HERDR_PLUGIN_STATE_DIR`) and read at start. Ids that are gone are removed on write.

The tree opens with the selection on the agent it was opened from, or on that pane's tab row when the pane has no agent.

## Plugin shape

```
tree/
  herdr-plugin.toml
  go.mod
  cmd/tree/main.go
  internal/model/     tree build: pure, no I/O
  internal/source/    Herdr socket client, dispatch state reader
  internal/view/      Bubble Tea program, rendering
  docs/
  README.md
```

`herdr-plugin.toml`: id `herdr-tree`, `min_herdr_version = "0.9.1"`, a `[[build]]` step (`go build -o bin/tree ./cmd/tree`), and one `[[panes]]` entry `tree` with `placement = "popup"`. The user binds a key in their own config, for example:

```toml
[[keys.command]]
key = "prefix+t"
type = "shell"
command = "herdr plugin pane open --plugin herdr-tree --entrypoint tree --placement popup"
description = "open supervisor tree"
```

The same entrypoint works with `--placement split` or `tab` for a view that stays open.

**Units.**

- `model.Build(snapshot, dispatch) Tree`. Input is plain structs. Every rule in "Data model" is here and nowhere else.
- `source.Snapshot()` calls `workspace.list`, `tab.list`, `agent.list` and `pane.list` on the socket in `HERDR_SOCKET_PATH`. `source.Dispatch()` reads every run directory and the pane index. A run with a missing `supervisor` file or a ledger that does not parse is skipped; the others still load. A pane index that does not parse means no pane is treated as tracked.
- `view` holds selection, fold, filter and scroll state, and draws the tree.

**Refresh.** Both sources are read once a second. The dispatch files are small, so they are read in full each time with no cache. Herdr has `events.subscribe`; polling is used because the data are small and it has fewer failure modes. Events are a later change if polling proves slow.

**Dependencies.** Bubble Tea, Lip Gloss and a TOML parser for the plugin's config file. No dependency on the dispatch plugin's code. The contract with dispatch is the set of fields below, and the dispatch README gets a short section that names them as read by `tree`, so a change to them is a known breaking change.

| File | Fields read |
|---|---|
| `runs/<run>/supervisor` | the supervisor pane id |
| `runs/<run>/ledger.json` | `pane_id`, `tab_id`, `workspace_id`, `worktree`, `created` |
| `panes.json` | `role` |

The run directory name is read for the ticket key.

## Failure behavior

| Case | Behavior |
|---|---|
| Dispatch plugin not installed, or no runs | Every agent is a root. Nothing is hidden by rule 1. |
| Ledger entry for a pane, tab and workspace that are all gone | Ignored. |
| A ledger or `supervisor` file cannot be read | That run is skipped. |
| Socket not reachable | One line of error text in the pane. The next poll tries again. |
| Selected agent is gone at `enter` | The tree refreshes and stays open. |
| Started outside Herdr | Exit with a message. |

## Testing

- Unit tests on `model.Build` with fixture JSON:
  - three levels of dispatch;
  - a worker placed as a tab in its supervisor's workspace;
  - a workspace with two supervising tabs, each with workers;
  - a tab with two agents, with and without one of them dispatched;
  - an entry whose pane is gone but whose tab is live;
  - an entry with a dead supervisor, and one with a dead worker;
  - two entries for one pane;
  - a cycle;
  - a tracked pane, and a pane with `tree=hide`, each left out and counted;
  - a supervisor with one ticket and one with several;
  - a workspace with no agent, and one that holds only hidden panes.
- Tests on the rendered text, with color off, for an open tree, a folded parent, guide lines at three levels, wide characters and a pane narrower than the content.
- A smoke script that starts an isolated Herdr session with two workspaces and checks the plain-text output of `tree --once` over the real socket. A shell is not an agent, and a state reported for a pane with no agent process is cleared within a second, so the smoke test covers the socket path and the `No agent` group only. Rows for agents are covered by the unit tests.

`tree --once` prints the tree to stdout and exits. It exists for the smoke test and for use in scripts.

An isolated session for tests needs its own `XDG_CONFIG_HOME`. A named session (`--session`) or `HERDR_CONFIG_PATH` alone still loads the user's installed plugins. The socket path must stay under the `sun_path` length limit.

## Settled by test

- **Ids across a server restart.** Workspace, tab and pane ids were the same before and after a restart of an isolated Herdr 0.9.3 server, and the id of a workspace closed before the restart was not used again.

## Open items to settle during implementation

1. **State colors.** Compare the dot of one agent in each of `blocked`, `working`, `done` and `idle` against the sidebar and correct the default palette.
2. **Moved panes.** A supervisor pane moved to another workspace gets a new id, and the `supervisor` file keeps the old one. Until dispatch records the move, that supervisor's workers show as roots.
3. **Labels for agents with no name.** The terminal title changes while an agent works, and a generic title says nothing. v1 uses the title. Decide later whether a pane row with no name should use the pane's position in the tab.
