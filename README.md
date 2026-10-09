# herdr-plugins

Plugins for [Herdr](https://herdr.dev), the terminal workspace manager for coding agents.

| Plugin | What it does |
|---|---|
| [dispatch](dispatch/) | Supervise coding agents without polling them: a durable mailbox, push wakes that will not land in a half-typed prompt, and a report when a worker blocks or disappears. |
| [tree](tree/) | Agents as a tree by supervisor, with the state of each, and a jump to the selected one. |
| [status-pane](status-pane/) | A pane beside a Claude Code session that shows a live "current status?" answer. |

Install one plugin by its subdirectory:

```sh
herdr plugin install mago0/herdr-plugins/dispatch
herdr plugin install mago0/herdr-plugins/status-pane
herdr plugin install mago0/herdr-plugins/tree
```

Each plugin directory has its own README.

## License

MIT. See [LICENSE](LICENSE).
