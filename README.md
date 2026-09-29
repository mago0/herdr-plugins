# herdr-status-pane

A [herdr](https://herdr.dev) pane beside a Claude Code session that shows a live answer to "current status?", refreshed each time a turn ends.

```
status · w3:p9  ✓ idle
updated 40s ago · 15s · 116k in (97% cached) · 1k out · total 480k
------------------------------------------------------------------
The migration PR is approved and the load test is still running.

**Done**
- ...

**In progress**
- ...

**Next**
- ...

**Open questions**
- Merge api#42 now, or wait for the load test?
```

## How it works

1. A Claude Code `Stop` hook runs when a turn ends. It exits at once unless a status pane is open for that herdr pane.
2. `bin/status-refresh` gets the pane's Claude session id from herdr and forks the session: `claude -p --resume <id> --fork-session --no-session-persistence`, with the prompt in `prompt.md`. The fork has the full conversation, so the answer is the same one you get by asking. Nothing is added to the parent session.
3. The fork keeps the parent's tool definitions, so it reads the parent's prompt cache. A `PreToolUse` hook passed with `--settings` blocks every tool call, so the fork can only answer from context. Removing tools with `--tools ""` would change the tool list and miss the cache.
4. Refreshes are serialized per pane and run at most once per `HERDR_STATUS_MIN_INTERVAL` seconds (default 120). Triggers during the wait collapse into one trailing refresh.
5. `bin/status-view` renders `status.md` with `bat`. Refresh is enabled only while the viewer runs.

The fork runs with `HERDR_*` and `ORCA_*` variables removed, so its own hooks cannot rebind the parent's herdr pane or trigger another refresh.

## Requirements

- herdr 0.9.0 or later, Linux or macOS
- Claude Code with `--fork-session` and `--no-session-persistence`
- `bash`, `jq`, `bat`, `flock`, `setsid`

## Install

```sh
herdr plugin install mago0/herdr-status-pane
root="$(herdr plugin list --plugin herdr-status-pane --json | jq -r '.result.plugins[0].plugin_root')"
```

Add the Stop hook to `~/.claude/settings.json` (replace `$root` with the path printed above):

```json
{
  "hooks": {
    "Stop": [
      { "hooks": [ { "type": "command", "command": "$root/hooks/claude-stop.sh", "timeout": 5 } ] }
    ]
  }
}
```

Bind the toggle in `~/.config/herdr/config.toml`, then run `herdr server reload-config`:

```toml
[[keys.command]]
key = "prefix+shift+s"
type = "plugin_action"
command = "herdr-status-pane.toggle"
description = "toggle Claude status pane"
```

Optional agent skill, so you can say "open the status pane":

```sh
ln -s "$root/skill" ~/.claude/skills/herdr-status-pane
```

## Use

- `prefix+shift+s` in a Claude Code pane opens or closes its status pane.
- In the pane: `r` refreshes now, `j`/`k` scroll, `g` goes to the top, `q` closes.

## Configure

- **Layout:** copy `prompt.md` to `~/.config/herdr/plugins/config/herdr-status-pane/prompt.md` and edit it.
- **Environment:** `HERDR_STATUS_MIN_INTERVAL`, `HERDR_STATUS_PROMPT_FILE`, `HERDR_STATUS_CLAUDE_BIN`.

## Usage

Each refresh sends the session's full context as input, mostly read from cache, so input tokens per refresh are about the session's context size. The header shows the last refresh and a running total. `~/.local/state/herdr-status-pane/usage.tsv` logs every refresh.

## Limits

- Claude Code only.
- The pane updates when a turn ends, or on `r`. It does not change during a long turn.
- Your other Stop hooks also run inside the fork.

## License

MIT
