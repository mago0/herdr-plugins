{ lib, buildGoModule }:

buildGoModule {
  pname = "herdr-tree";
  version = (lib.importTOML ./herdr-plugin.toml).version;

  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.unions [
      ./go.mod
      ./go.sum
      ./cmd
      ./internal
      ./herdr-plugin.toml
      ./README.md
    ];
  };

  vendorHash = "sha256-qrX55UC7IMOZS8yDB+JIf5fAatfsRaMl38T1rDKHSAg=";
  subPackages = [ "cmd/tree" ];

  # `herdr plugin link` runs no build, so the plugin directory ships its binary.
  postInstall = ''
    share=$out/share/herdr-plugins/tree
    mkdir -p $share/bin
    cp herdr-plugin.toml README.md $share/
    ln -s $out/bin/tree $share/bin/tree
  '';

  meta = {
    description = "Herdr plugin: agents as a tree by supervisor, with the state of each";
    homepage = "https://github.com/mago0/herdr-plugins";
    license = lib.licenses.mit;
    mainProgram = "tree";
    platforms = lib.platforms.unix;
  };
}
