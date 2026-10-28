{
  description = "Herdr plugins: fleet, a supervisor for worker agents";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      homeManagerModules.default = import ./nix/module.nix self;

      packages = forAll (pkgs: rec {
        fleet = pkgs.callPackage ./fleet/package.nix { };
        tree = pkgs.callPackage ./tree/package.nix { };
        default = fleet;
      });

      # `nix run .#try`: the fork and both plugins in a Herdr session of their own.
      apps = forAll (pkgs: {
        try = {
          type = "app";
          program = nixpkgs.lib.getExe (
            pkgs.writeShellApplication {
              name = "herdr-fleet-try";
              runtimeInputs = [
                pkgs.coreutils
                pkgs.git
              ];
              text = builtins.readFile ./nix/try.sh;
            }
          );
          meta.description = "Try the Herdr fork with fleet and tree in a session of their own";
        };
      });
    };
}
