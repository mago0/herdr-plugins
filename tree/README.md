# tree

Agents in [Herdr](https://herdr.dev) as a tree by supervisor.

Herdr's sidebar groups workspaces by repository. Supervised work has another shape: one agent dispatches workers into several repositories, and a worker can dispatch its own. This pane shows each agent under the agent that started it, with its state, and jumps to the one you select.

```
 ▾ ● ops
   ├─ ● review monitor ⇥                     ops
   │  ├─ ● review-86                         iam
   │  └─ ● review-234                        billing
   └─ ● planning ⇥                           ops
      └─ ● add-probe                         api
 ▾ ● platform migration
   ├─ ● add-vpc-routes                       infra
   └─ ● migrate-scheduler                    scheduler
      └─ ● build-images ⇥                    scheduler
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

## What a row is

A row is one pane that hosts an agent. Terminals have no row.

- A workspace with one agent is one row with the workspace's name.
- A worker that dispatch placed as a tab is a row under its supervisor, marked with the tab glyph.
- A workspace with supervising agents in several tabs is a row with one row for each tab under it.
- A tab with several agents is a row with one row for each pane under it, marked with the pane glyph.
- `No agent (n)` holds the workspaces with no agent. In practice these are the main checkouts that Herdr keeps as parents of worktree workspaces.

The right column is the ticket key on a root, when every run that agent supervises names the same key. Below a root it is the repository.

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
# The defaults are Nerd Font icons. These two work in any font.
tab = "⇥"
pane = "›"

[colors]
blocked = "#f38ba8"
working = "#f9e2af"
done = "#a6e3a1"
idle = "#89b4fa"
unknown = "#6c7086"
```

## Use in scripts

`tree --once` prints the tree as plain text and exits. States are `!` blocked, `*` working, `+` done, `o` idle, `·` unknown. It must run inside a Herdr pane.

## Test

```sh
go test ./...
test/smoke.sh    # starts and stops its own Herdr server; needs tmux
```
