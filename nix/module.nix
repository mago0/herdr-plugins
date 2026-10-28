# Home Manager module: the fleet and tree Herdr plugins, registered with `herdr plugin link`.
self:
{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.programs.herdr-plugins;
  packages = self.packages.${pkgs.stdenv.hostPlatform.system};
  # Home Manager's programs.herdr package when it is set, else a herdr on PATH or in the profile.
  herdrBin =
    if (config.programs.herdr.enable or false) && (config.programs.herdr.package or null) != null then
      lib.getExe config.programs.herdr.package
    else
      ''"$(command -v herdr || echo ${config.home.profileDirectory}/bin/herdr)"'';
  plugins = lib.filterAttrs (_: plugin: plugin.enable) {
    inherit (cfg) fleet tree;
  };
  pluginDir = name: plugin: "${plugin.package}/share/herdr-plugins/${name}";
in
{
  options.programs.herdr-plugins = {
    fleet = {
      enable = lib.mkEnableOption "fleet, the supervisor for worker agents in Herdr worktrees";
      package = lib.mkOption {
        type = lib.types.package;
        default = packages.fleet;
        defaultText = lib.literalExpression "herdr-plugins.packages.\${system}.fleet";
        description = "The fleet package. It has its tools pinned and puts `fleet` on PATH.";
      };
      skillDirs = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [
          ".claude/skills"
          ".agents/skills"
        ];
        description = ''
          Skill directories, relative to the home directory, that get a `fleet` link to the
          packaged skill. Claude Code reads {file}`~/.claude/skills`; pi and Codex read
          {file}`~/.agents/skills`.
        '';
      };
    };
    tree = {
      enable = lib.mkEnableOption "tree, agents as a tree by supervisor";
      package = lib.mkOption {
        type = lib.types.package;
        default = packages.tree;
        defaultText = lib.literalExpression "herdr-plugins.packages.\${system}.tree";
        description = "The tree package.";
      };
    };
  };

  config = lib.mkIf (plugins != { }) {
    home.packages = lib.optional cfg.fleet.enable cfg.fleet.package;

    home.file = lib.mkIf cfg.fleet.enable (
      lib.listToAttrs (
        map (dir: {
          name = "${dir}/fleet";
          value.source = "${pluginDir "fleet" cfg.fleet}/skill";
        }) cfg.fleet.skillDirs
      )
    );

    # A link replaces the last registration of the same plugin id, so each switch moves Herdr
    # to the new store path. Herdr can register plugins while no server runs.
    home.activation.herdrPlugins = lib.hm.dag.entryAfter [ "writeBoundary" ] (
      lib.concatStringsSep "\n" (
        lib.mapAttrsToList (name: plugin: ''
          run ${herdrBin} plugin link ${pluginDir name plugin} >/dev/null \
            || echo "herdr-plugins: linking ${name} failed (non-fatal)" >&2
        '') plugins
      )
    );
  };
}
