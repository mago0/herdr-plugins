# tree

The [Herdr](https://herdr.dev) sidebar section that shows each workspace as an effort: the shells and agents in it, and under each agent the workers it dispatched to other workspaces, each with its state.

An effort is one piece of work, such as a ticket or a project. Its parent agent often works across several repositories, and sends a large subtask to a worker in its own workspace and worktree. The worker reports its result back to the parent. This pane keeps those workers under the agent that dispatched them, so the whole effort stays in view.

```
 ▾ GKE Migration                      proj ↗
     flosports@main
     $ Status
     ● HANDOFF.md resume
     └ ● SRE-194 job scheduler      SRE-194
          job-scheduler@sre-194-watchdog-n…
         $ shepherd

 ▾ Main
     flosports@main
     ● Herdr left pane view layout mockup
     ● Infra review conventions update
     ├ ● review-continue-watching-79     #79
     │    continue-watching@effect-local-ex…
     │   $ reviewr
     └ ● Review infra base services    #1102
          infra-base-services@jordangarri…
         $ reviewr

 ▸ Core PHP8.3 Upgrade  ● ●       SRE-923 ↗
```

The supervisor of each worker comes from Herdr: the `supervisor_pane_id` of the agent, which `herdr pane supervise` sets and `herdr agent start` sets for an agent started from another pane. The [dispatch](../dispatch/) plugin sets it for each worker. On a Herdr server without that field, no agent is a worker and each workspace lists its own agents.

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
description = "open efforts"
```

Use `--placement split` or `tab` for a view that stays open.

## Keys

| Key | Action |
|---|---|
| `↑` `↓`, `k` `j` | Move. |
| `shift+↑` `shift+↓` | Move the effort under the cursor one place in Herdr's workspace order. |
| `o` | Open the link of the row in the browser, when the row has one. |
| `enter` | Go to the selected effort, agent or shell, and close. |
| `space` | Fold or open the effort under the cursor. |
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

Herdr sets `HERDR_SIDEBAR_SECTION` for such a pane. The pane then changes in these ways:

- It shows only the rows. Herdr draws the section title.
- It stays open: `q` and `esc` do nothing, and a jump does not close it.
- The cursor follows the pane that Herdr has in focus.
- A click on a row jumps to it: an effort row to its workspace, any other row to its pane. A
  click on the fold mark of an effort folds or opens it. The wheel scrolls.
- Ctrl with a click opens the link of a row in the browser. A row has a link when its pane sets
  the `link` token to an `http` or `https` address
  (`herdr pane report-metadata <pane> --source tree --token link=<url>`), and its right edge is
  then underlined.
- A drag moves an effort: it takes the place of the effort it is dropped on, and a drop on a pane
  row is a drop on the effort that holds it. The move is that of the workspace in Herdr's order
  (`workspace.move_block`), so Herdr keeps it. Pane rows do not move.
- The last line is for the row under the pointer, or for the cursor row when the pointer is on no
  row or has not moved for five seconds. For a blocked row it says what the agent waits for, as
  Herdr reads it from the agent's screen. For any other row it says where the row works.
- A right-click on a row opens Herdr's menu for what the row shows: the workspace for an effort,
  the pane for any other row. The pane names the row in its terminal title
  (`herdr-menu;<kind>;<id>;<count>;<agent pane>;<label>`) and the fork opens the menu at the
  pointer. The agent pane is set only for an agent or worker row, so only those have `Label...`.
- The menu of a blocked Claude Code row starts with the answers of its dialog, such as
  `Answer: 1. Yes`. An answer presses that number in the agent's pane. The answers are there only
  when the dialog numbers its choices from 1 with no gap; a dialog with no numbers, such as the
  folder trust prompt, gets none, and you answer it in its pane. The answers are below `Rename`
  and `Close`, so Enter in a menu that just opened never picks one. Herdr presses the number only
  when the agent still shows the same dialog, and tells you when it does not. For such a row the
  title is `herdr-menu2;` and a JSON object with one-letter keys, because Herdr keeps 256
  characters of a pane title: `t` and `i` name what the row shows, `p` is the pane of its agent,
  `l` its label, `d` the dialog name from the agent record, and `a` the answers as `[key, text]`
  pairs. The pane cuts answer text until the title fits.
- The keys reach it only after the fork's `focus_sidebar` keybind.
- The fork's `next_workspace` and `previous_workspace` keybinds (`next_agent` and `previous_agent`
  in the agents section) move the focus to the next or previous agent or worker row. The fork
  sends the pane the keys `F20` and `F19` (`ESC [ 19 ; 2 ~` and `ESC [ 18 ; 2 ~`). A step starts
  from the row of the pane in focus, skips effort and shell rows and the rows of a folded effort,
  and wraps at the ends.

## What a row is

- **Effort:** one for each workspace, in Herdr's order. The name is bold. The right edge shows the label and link of the workspace's first agent: `SRE-923 ↗` for an issue, `proj ↗` for a Linear project. The line under it, at the column of the name, is the repo and branch, with the commits ahead of and behind the upstream.
- **Shell:** a pane with no agent, marked with a `$` in its own color. It shows the pane's name, or where it is: `<repo>/<path>` inside a repository, else the path from `~`.
- **Agent:** a pane with an agent. It shows the state dot and the name of its tab, when the tab has a name you gave it and no other agent. Otherwise it shows the title the session set, else the agent's name or kind.
- **Worker:** an agent that an agent in another workspace supervises. It hangs from a branch line under that agent's dot. The right edge shows its label, else the number of the pull request it links. The line under it is the worktree glyph for a linked worktree, the repo and branch, the commits ahead of and behind `origin/HEAD`, and `reported` once its supervisor took its result. The worker's own shells follow, at its text column.

Within an effort the shells come first, then each agent followed by its workers. A workspace whose agents are all workers is not an effort: its shells show under its first worker. A supervisor in the same workspace does not make a worker. A worker whose supervisor is gone is an agent of its own workspace again.

A folded effort is one line with a dot for each of its agents and workers, or `$` when it has only shells. An agent with workers folds the same way: its workers go, and their dots follow its name. Click the fold mark left of the dot, or press space on the row. Folds are saved.

The state dot of a row is orange (`~` in plain text) when the agent reports working and its screen has not changed for the time Herdr allows (`session.agent_stall_after_secs`, 600 by default). The color is `stalled` under `[colors]`.

A row has an envelope and a count after its name when Herdr holds prompts for that pane until it is safe to type them, for example a wake line for the pane you have in focus. The glyph is `pending` under `[glyphs]`.

The counts after a branch are `-N` for commits behind and `+N` for commits ahead. A count of zero is not shown, and a branch with nothing to count against has no counts. A detached HEAD shows its short commit id. The counts are as of the last fetch: the pane does not fetch, and it asks Git again every 10 seconds.

`reported` comes from the [dispatch](../dispatch/) ledgers in `$XDG_STATE_HOME/agent-dispatch/runs` (`~/.local/state/agent-dispatch/runs` by default): the latest dispatch of the worker's pane is settled, which means the supervisor took its `worker_done`, and the supervisor has sent it no follow-up since (`dispatch.sh send`). It is not shown while the worker is working.

## Labels

An effort or a worker can carry a short label at its right edge, such as a ticket key. It is the `label` token of the agent's pane, reported under the source `tree`, and the pane shows at most 12 cells of it. The `link` token is the web address of the issue or project.

```sh
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --token label=SRE-923 --token link=https://linear.app/<org>/issue/SRE-923
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --clear-token label --clear-token link
```

- In the fork's sidebar, the right-click menu of an agent or worker row has `Label...`. An empty text clears the label.
- The `skill/` folder tells an agent to set its own label and link when work starts on a ticket or project. Link it into your agent's skills directory:

  ```sh
  root="$(herdr plugin list --plugin herdr-tree --json | jq -r '.result.plugins[0].plugin_root')"
  ln -s "$root/skill" ~/.claude/skills/herdr-tree
  ```

`dispatch.sh run` and `dispatch.sh start` set the label from `--label`, or from an issue key at the start of the run name or the agent name.

## Panes that do not count

A pane that sets the `tree=hide` token on itself is left out. A watch loop needs no pane at all: run it as a Herdr job (`herdr job add`).

```sh
herdr pane report-metadata "$HERDR_PANE_ID" --source my-loop --token tree=hide
```

## Configuration

`$(herdr plugin config-dir herdr-tree)/config.toml`. Every key is optional.

```toml
[glyphs]
# The default worktree and pending glyphs are Nerd Font icons. These work in any font.
shell = "$"
worktree = "⎇"
pending = "+"

[colors]
blocked = "#f38ba8"
working = "#f9e2af"
done = "#a6e3a1"
idle = "#89b4fa"
unknown = "#6c7086"
# The $ of a shell row.
shell = "#cba6f7"
# The repository line.
repo = "#94e2d5"
```

## Use in scripts

`tree --once` prints the efforts as plain text and exits. States are `!` blocked, `*` working, `+` done, `o` idle, `·` unknown. It must run inside a Herdr pane.

## Test

```sh
go test ./...
test/smoke.sh    # builds bin/tree, starts and stops its own Herdr server; needs tmux
```
