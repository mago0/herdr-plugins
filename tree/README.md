# tree

Agents in [Herdr](https://herdr.dev) as a tree by supervisor.

Herdr's sidebar groups workspaces by repository. Supervised work has another shape: one agent dispatches workers into several repositories, and a worker can dispatch its own. This pane shows each agent under the agent that started it, with its state, and jumps to the one you select.

```
 ▾ ● ops
   │ ops
   ├─ ● review monitor ⇥
   │  ├─ ● review-86
   │  │    iam
   │  └─ ● review-234
   │       billing
   └─ ● planning ⇥
      └─ ● add-probe
           api
 ▾ ● platform migration
   ├─ ● add-vpc-routes
   │    infra
   └─ ● migrate-scheduler
      │ scheduler
      └─ ● build-images ⇥
 ▸ No agent (8)
 ▸ Hidden (7)
```

The supervisor of each worker comes from the [dispatch](../dispatch/) plugin's run ledgers. Without dispatch, every agent is a root and the pane is a flat list with states.

## Install

```sh
herdr plugin install mago0/herdr-plugins/tree
```

Requirements: Herdr 0.9.1 or later, Go 1.24 or later to build on install.

Bind a key in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+t"
type = "shell"
command = "herdr plugin pane open --plugin herdr-tree --entrypoint tree --placement popup"
description = "open supervisor tree"
```

Use `--placement split` or `tab` for a view that stays open.

## Keys

| Key | Action |
|---|---|
| `↑` `↓`, `k` `j` | Move. |
| `enter` | Go to the selected agent and close. On a group, fold or unfold. |
| `space` | Fold or unfold. A folded row shows one dot for each agent below it. |
| `a` | Show only agents that are blocked or done, with the rows above them. |
| `q`, `esc` | Close. |

## As a sidebar section

The [mago0/herdr](https://github.com/mago0/herdr) fork can draw a sidebar section with a plugin
pane. It draws the spaces section with this plugin by default. To set it yourself:

```toml
[ui.sidebar.spaces]
plugin = "herdr-tree:tree"   # "" gives the native spaces list back

[ui.sidebar.agents]
plugin = ""                  # the agents section takes a plugin the same way
```

Herdr sets `HERDR_SIDEBAR_SECTION` for such a pane. The tree then changes in these ways:

- It shows only the rows. Herdr draws the section title.
- It stays open: `q` and `esc` do nothing, and a jump does not close it.
- The cursor follows the pane that Herdr has in focus.
- A click on a row jumps to it. A click on the part before the state dot folds a row that has
  children. The wheel scrolls.
- The last line says where a row works: `<repo>/<path in the repo>` for a linked Git worktree,
  else the path of the checkout. It is for the row under the pointer, or for the cursor row when
  the pointer is on no row or has not moved for five seconds.
- A right-click on a row opens Herdr's menu for what the row shows: the workspace, the tab or
  the pane. The pane names the row in its terminal title
  (`herdr-menu;<kind>;<id>;<count>;<agent pane>;<label>`) and the fork opens the menu at the
  pointer.
- The keys reach it only after the fork's `focus_sidebar` keybind.
- The fork's `next_workspace` and `previous_workspace` keybinds (`next_agent` and `previous_agent`
  in the agents section) move the focus one row down or up the tree. The fork sends the pane the
  keys `F20` and `F19` (`ESC [ 19 ; 2 ~` and `ESC [ 18 ; 2 ~`). A step starts from the row of the
  pane in focus, skips the group rows and the rows of a folded parent, and wraps at the ends.

## What a row is

A row is one pane that hosts an agent. Terminals have no row.

- A workspace with one agent is one row with the workspace's name.
- A worker that dispatch placed as a tab is a row under its supervisor, marked with the tab glyph.
- A workspace with supervising agents in several tabs is a row with one row for each tab under it.
- A tab with several agents is a row with one row for each pane under it, marked with the pane glyph.
- `No agent (n)` holds the workspaces with no agent. In practice these are the main checkouts that Herdr keeps as parents of worktree workspaces.

The repository is on a second line under the name, in italics and its own color. A row has that line only when its repository is not the one of the row it hangs from. Rows of one supervisor are in workspace order, then in tab bar order.

A row whose agent works in a linked Git worktree has the worktree glyph after its name. For a workspace row this comes from Herdr. For a tab or pane row it comes from the agent's working directory.

## Labels

A row can carry a short label at the right edge of its name line, such as a ticket key. It is the `label` token of the agent's pane, reported under the source `tree`, and the tree shows at most 12 cells of it.

```sh
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --token label=SRE-923
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --clear-token label
```

- In the fork's sidebar, the right-click menu of a row that is one agent has `Label...`. An empty text clears the label.
- The `skill/` folder tells an agent to label its own pane when work starts on a ticket. Link it into your agent's skills directory:

  ```sh
  root="$(herdr plugin list --plugin herdr-tree --json | jq -r '.result.plugins[0].plugin_root')"
  ln -s "$root/skill" ~/.claude/skills/herdr-tree
  ```

A root with no label shows the ticket key that every run it supervises names, when they all name the same one.

## Panes that do not count

Two kinds of agent pane are left out and counted in `Hidden (n)`:

- Loops that dispatch tracks (`dispatch.sh track`). Nothing to set up.
- A pane that sets the `tree=hide` token on itself:

  ```sh
  herdr pane report-metadata "$HERDR_PANE_ID" --source my-loop --token tree=hide
  ```

## Configuration

`$(herdr plugin config-dir herdr-tree)/config.toml`. Every key is optional.

```toml
[glyphs]
# The defaults are Nerd Font icons. These three work in any font.
tab = "⇥"
pane = "›"
worktree = "⎇"

[colors]
blocked = "#f38ba8"
working = "#f9e2af"
done = "#a6e3a1"
idle = "#89b4fa"
unknown = "#6c7086"
# The repository line.
repo = "#94e2d5"
```

## Use in scripts

`tree --once` prints the tree as plain text and exits. States are `!` blocked, `*` working, `+` done, `o` idle, `·` unknown. It must run inside a Herdr pane.

## Test

```sh
go test ./...
test/smoke.sh    # starts and stops its own Herdr server; needs tmux
```
