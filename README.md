# herdr-plugins

Plugins for [Herdr](https://herdr.dev), the terminal workspace manager for coding agents.

| Plugin | What it does |
|---|---|
| [dispatch](dispatch/) | Supervise worker agents in git worktrees. Workers report through a durable mailbox, and the supervisor is woken by push when a worker needs it, blocks or exits. |
| [status-pane](status-pane/) | A pane beside a Claude Code session that shows a live "current status?" answer. |

Install one plugin by its subdirectory:

```sh
herdr plugin install mago0/herdr-plugins/dispatch
herdr plugin install mago0/herdr-plugins/status-pane
```

Each plugin directory has its own README.

## License

MIT. See [LICENSE](LICENSE).
