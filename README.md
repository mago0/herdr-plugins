# herdr-plugins

Plugins for [Herdr](https://herdr.dev), the terminal workspace manager for coding agents.

| Plugin | What it does |
|---|---|
| [fleet](fleet/) | Supervise coding agents without polling them: a durable mailbox with acked messages, a ledger of attempts, and a lifecycle for each worker, on top of the delivery queue and supervision links of the [mago0/herdr](https://github.com/mago0/herdr) fork. |
| [tree](tree/) | Agents as a tree by supervisor, with the state of each, and a jump to the selected one. |
| [status-pane](status-pane/) | A pane beside a Claude Code session that shows a live "current status?" answer. |

Install one plugin by its subdirectory:

```sh
herdr plugin install mago0/herdr-plugins/fleet
herdr plugin install mago0/herdr-plugins/status-pane
herdr plugin install mago0/herdr-plugins/tree
```

fleet is also a Nix package, with its tools pinned: `devbox global add github:mago0/herdr-plugins#fleet` or `nix profile install github:mago0/herdr-plugins#fleet`. See [fleet/README.md](fleet/README.md#with-nix-or-devbox).

Each plugin directory has its own README.

## License

MIT. See [LICENSE](LICENSE).
