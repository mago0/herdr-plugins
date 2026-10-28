#!/usr/bin/env bash
# Try the mago0/herdr fork with the fleet and tree plugins in a session of its own.
# Your own Herdr, its plugins, sessions and config stay as they are.
#
#   nix run github:mago0/herdr-plugins#try -- [--setup-only] [session-id]
#
# The session id defaults to a new one. Run it again with the same id to come back.
set -euo pipefail

setup_only=false
if [ "${1:-}" = "--setup-only" ]; then setup_only=true; shift; fi
id="${1:-f$(date +%m%d%H%M%S)}"
herdr_ref="${HERDR_REF:-github:mago0/herdr/fork}"
plugins_ref="${HERDR_PLUGINS_REF:-github:mago0/herdr-plugins}"
root="${XDG_CACHE_HOME:-$HOME/.cache}/hft/$id"

# The server socket lives under the session's config dir, and a Unix socket path has a length limit.
sock="$root/c/herdr/sessions/$id/herdr-client.sock"
if [ "${#sock}" -gt 100 ]; then
  echo "herdr-fleet-try: the socket path would be ${#sock} characters; use a shorter id or XDG_CACHE_HOME" >&2
  exit 1
fi

mkdir -p "$root/gc" "$root/c/herdr" "$root/s" "$root/repo"

echo "Building herdr and the plugins (the first run takes a while)..."
nix build --out-link "$root/gc/herdr" "$herdr_ref"
nix build --out-link "$root/gc/fleet" "$plugins_ref#fleet"
nix build --out-link "$root/gc/tree" "$plugins_ref#tree"

# Your other config and state stay visible (gh, git, agent logins); only herdr and fleet are new.
for f in "$HOME"/.config/*; do
  n=${f##*/}
  [ "$n" = herdr ] || [ -e "$root/c/$n" ] || ln -s "$f" "$root/c/$n"
done
for f in "$HOME"/.local/state/*; do
  n=${f##*/}
  case "$n" in agent-fleet | agent-dispatch) ;; *) [ -e "$root/s/$n" ] || ln -s "$f" "$root/s/$n" ;; esac
done
# Your herdr settings (theme, keys), read-only, if you have them.
if [ -e "$HOME/.config/herdr/config.toml" ] && [ ! -e "$root/c/herdr/config.toml" ]; then
  ln -s "$HOME/.config/herdr/config.toml" "$root/c/herdr/config.toml"
fi

cat >"$root/env.sh" <<EOF
# Source this in another shell to work in the trial session.
unset HERDR_SOCKET_PATH HERDR_PANE_ID HERDR_WORKSPACE_ID HERDR_TAB_ID HERDR_BIN_PATH HERDR_ENV HERDR_PLUGIN_ID
export HERDR_SESSION='$id'
export XDG_CONFIG_HOME='$root/c' XDG_STATE_HOME='$root/s'
export PATH='$root/gc/herdr/bin:$root/gc/fleet/bin':"\$PATH"
EOF
# shellcheck source=/dev/null
. "$root/env.sh"

share_fleet="$root/gc/fleet/share/herdr-plugins/fleet"
herdr plugin link "$share_fleet" >/dev/null
herdr plugin link "$root/gc/tree/share/herdr-plugins/tree" >/dev/null

# A repo to dispatch workers in, with the fleet skill for Claude Code, pi and Codex.
cd "$root/repo"
mkdir -p .claude/skills .agents/skills
ln -sfn "$share_fleet/skill" .claude/skills/fleet
ln -sfn "$share_fleet/skill" .agents/skills/fleet
if [ ! -d .git ]; then
  printf '.claude/\n.agents/\n' >.gitignore
  git init -q && git add .gitignore && git commit -qm 'herdr fleet trial'
fi

cat <<EOF

$(herdr --version), session '$id'
  repo:     $root/repo  (start an agent here and ask it to dispatch work)
  another shell:  . $root/env.sh
  stop:     . $root/env.sh && herdr server stop
  remove:   rm -rf $root
Run this in a new terminal window, not inside another Herdr, so the prefix keys do not clash.
EOF

$setup_only && exit 0
exec herdr --session "$id"
