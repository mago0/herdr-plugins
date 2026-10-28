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

fleet and tree are also Nix packages, with their tools pinned: `devbox global add github:mago0/herdr-plugins#fleet` or `nix profile add github:mago0/herdr-plugins#fleet`, and `#tree`. The flake also exports a Home Manager module, `homeManagerModules.default`. See [fleet/README.md](fleet/README.md#with-nix-or-devbox).

To try the fork with fleet and tree in a Herdr session of their own, apart from your own Herdr: `nix run github:mago0/herdr-plugins#try`.

Each plugin directory has its own README.

## License

MIT. See [LICENSE](LICENSE).
