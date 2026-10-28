{
  lib,
  stdenvNoCC,
  bash,
  coreutils,
  flock,
  git,
  gnugrep,
  gnused,
  iconv,
  inotify-tools,
  jq,
  util-linux,
}:

let
  # Herdr itself stays on the caller's PATH: fleet needs the fork's server, not a pinned one.
  runtimePath = lib.makeBinPath (
    [
      bash
      coreutils
      flock
      git
      gnugrep
      gnused
      iconv
      jq
      util-linux
    ]
    ++ lib.optional stdenvNoCC.hostPlatform.isLinux inotify-tools
  );
in
stdenvNoCC.mkDerivation {
  pname = "herdr-fleet";
  version = (lib.importTOML ./herdr-plugin.toml).version;

  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.unions [
      ./herdr-plugin.toml
      ./README.md
      ./skill
    ];
  };

  buildInputs = [ bash ];
  dontBuild = true;

  installPhase = ''
    runHook preInstall
    share=$out/share/herdr-plugins/fleet
    mkdir -p $share $out/bin
    cp -r herdr-plugin.toml README.md skill $share/
    # The old dispatch names are links for live sessions in a checkout; a fresh install has none.
    find $share/skill/scripts -type l -delete
    for script in $share/skill/scripts/*.sh; do
      sed -i '1a export PATH="${runtimePath}''${PATH:+:$PATH}"' "$script"
    done
    patchShebangs $share/skill/scripts
    # Run by path, not linked: the scripts find each other from $0.
    printf '#!%s\nexec %s "$@"\n' ${bash}/bin/bash $share/skill/scripts/fleet.sh > $out/bin/fleet
    chmod +x $out/bin/fleet
    runHook postInstall
  '';

  meta = {
    description = "Supervise worker agents in Herdr worktrees: runs, ledger, acked mail and a fleet view";
    homepage = "https://github.com/mago0/herdr-plugins";
    license = lib.licenses.mit;
    mainProgram = "fleet";
    platforms = lib.platforms.unix;
  };
}
