#!/bin/bash
# Build TNBrowser.vl2 -- the drop-in package.
#
# A .vl2 is just a zip. The engine builds its resource index from each mod
# directory's loose files *plus the contents of its .vl2 archives*, so paths
# inside the archive are resolved exactly as if they had been unpacked into the
# mod directory. That is why the archive root must be the mod root: the entry
# for the browser window has to be "tnbrowser/gui/TNBrowserGui.gui", not
# "TNBrowser/tnbrowser/gui/TNBrowserGui.gui".
#
# Install:  drop the .vl2 into GameData/<MOD>/ and launch with -mod <MOD>
#
# Stored without compression (-0). The engine reads these archives directly and
# the shipped ones are stored the same way; it costs a few hundred KB and avoids
# depending on the archive reader's deflate support.
#
# Builds two packages:
#
#   TNBrowser.vl2        the client mod -- the community screens
#   TNBrowserServer.vl2  the server mod -- clan tags for connecting players
#
# They are independent. A player needs only the client one; a server operator
# wanting tags to show needs only the server one.
#
# Baking settings in
# -----------------------------------------------------------------------------
# With no options the archives carry their defaults, and a deployment is
# configured with a loose autoexec.cs beside the .vl2 (see examples/).
#
# Passing the options below writes the values into the packaged settings.cs
# instead, so the archive is self-contained: dropping it into the active mod
# directory is the whole install, with nothing else to create or edit. That is
# the sane way to hand a server mod to someone else.
#
# The source tree is never modified -- packing happens from a copy.
#
# Usage: ./tools/build-vl2.sh [options] [output-dir]
#
#   --host URL          backend the client reads browser/clan/mail data from
#   --server-host URL   backend the game-server mod looks clans up in
#   --server-key KEY    must match the backend's TNB_SERVER_KEY
#
# Note the server key ends up inside the archive, so a .vl2 built with one is a
# secret: it speaks for every player, not one account. Build your own rather
# than passing someone else's around.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_HOST=""
SERVER_HOST=""
SERVER_KEY=""
OUTDIR=""

while [ $# -gt 0 ]; do
    case "$1" in
        --host)        CLIENT_HOST="${2:?--host needs a URL}"; shift 2 ;;
        --server-host) SERVER_HOST="${2:?--server-host needs a URL}"; shift 2 ;;
        --server-key)  SERVER_KEY="${2:?--server-key needs a value}"; shift 2 ;;
        -*) echo "Unknown option: $1" >&2; exit 1 ;;
        *) OUTDIR="$1"; shift ;;
    esac
done
OUTDIR="${OUTDIR:-$ROOT/dist}"

command -v zip >/dev/null || { echo "zip is required" >&2; exit 1; }
mkdir -p "$OUTDIR"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Replace the default in a guarded setting. The settings files are written as
#   if ($X $= "")
#      $X = "default";
# so rewriting the default line is enough, and the guard still lets a loose
# autoexec.cs override a baked-in value later.
bake() {
    local file="$1" var="$2" value="$3"
    [ -n "$value" ] || return 0
    python3 - "$file" "$var" "$value" <<'PYEOF'
import re, sys
path, var, value = sys.argv[1], sys.argv[2], sys.argv[3]
src = open(path).read()
pattern = re.compile(r'(\$' + re.escape(var) + r'\s*=\s*)"[^"]*"')
new, n = pattern.subn(lambda m: m.group(1) + '"' + value + '"', src, count=1)
if n == 0:
    sys.exit("could not find a default for $" + var + " in " + path)
open(path, "w").write(new)
PYEOF
    echo "  baked \$$var = $value" >&2
}

# $1 mod directory, $2 archive name, $3.. directories to include
pack() {
    local moddir="$1" name="$2"; shift 2
    local out="$OUTDIR/$name"
    local stage="$WORK/$moddir"
    rm -f "$out"

    mkdir -p "$stage"
    cp -r "$ROOT/$moddir/." "$stage/"

    # .dso files are compiled output and must never ship: the engine only
    # recompiles when the source is newer, so a stale .dso baked into the
    # archive would shadow the script it was built from on every install.
    find "$stage" -name '*.dso' -delete

    case "$moddir" in
        TNBrowser)
            bake "$stage/tnbrowser/settings.cs" "TNB::Host" "$CLIENT_HOST" ;;
        TNBrowserServer)
            bake "$stage/tnbserver/settings.cs" "TNBS::Host" "$SERVER_HOST"
            bake "$stage/tnbserver/settings.cs" "TNBS::ServerKey" "$SERVER_KEY" ;;
    esac

    cd "$stage"
    zip -0 -q -r "$out" "$@" -x '*.dso' -x '.*' -x '*/.*'
    echo "Built $out"
    unzip -l "$out" | tail -n +4 | head -n -2 | awk '{print "  " $4}' | sed '/^  $/d'
    echo
}

pack TNBrowser       TNBrowser.vl2       scripts tnbrowser
pack TNBrowserServer TNBrowserServer.vl2 scripts tnbserver

echo "Install: copy either into GameData/<MOD>/ and launch with -mod <MOD>"
