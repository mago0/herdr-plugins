---
name: herdr-status-pane
description: Open, close, or refresh a herdr pane beside this Claude Code session that shows a live "current status?" answer, refreshed when each turn ends. Use when the user says "open the status pane", "show status on the right", "close the status pane", or "refresh the status pane". Requires HERDR_ENV=1 and a Claude Code session.
---

# Herdr status pane

A viewer pane to the right of this session shows the answer to "current status?" in a fixed layout, most pressing first: a Summary title in the header (what the session is for), then Open questions, Last (the latest turn), In progress, Next and Done. Each refresh forks this session in print mode with every tool call blocked, so the answer comes from the full conversation and adds nothing to it. Refresh runs at turn end, at most once per 120s, and only while the pane is open.

Check `test "${HERDR_ENV:-}" = 1` first. If it fails, say the pane needs herdr and stop. Then find the plugin:

```bash
root="$(herdr plugin list --plugin herdr-status-pane --json | jq -r '.result.plugins[0].plugin_root')"
```

## Open or close

```bash
"$root/bin/open-status-pane" "$HERDR_PANE_ID"
```

The same command toggles: it closes the pane when one is already open for this session.

## Refresh now

```bash
"$root/bin/status-refresh" "$HERDR_PANE_ID" --force
```

Each refresh edits the previous status in place (rules in `$root/update.md`), and the pane marks new or changed items with a yellow `▌`. The status is sized to fit the pane without scrolling (budget rules in `$root/length.md`). In the pane: `r` refreshes, `a` shows every item, `j`/`k` scroll, `q` closes. A refresh started mid-turn describes the turn as in progress.

## Usage and state

- Each refresh reads this session's context from the prompt cache, so its input tokens are about the session's context size. The pane header shows input (with cache share), output, and the running total for this pane.
- State lives in `~/.local/state/herdr-status-pane/<pane>/`: `status.md`, `meta.json` (tokens, time, last error), `fork.err`. `usage.tsv` in the parent directory logs every refresh.

## Tuning

- Layout: copy `$root/prompt.md` to `~/.config/herdr/plugins/config/herdr-status-pane/prompt.md` and edit it.
- Environment read by `status-refresh`: `HERDR_STATUS_MIN_INTERVAL` (seconds, default 120), `HERDR_STATUS_PROMPT_FILE`, `HERDR_STATUS_CLAUDE_BIN`.
