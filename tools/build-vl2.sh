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
# The backend address is written into the packaged settings.cs, so the archive
# is self-contained: dropping it into the active mod directory is the whole
# install, with nothing else to create or edit. That is the sane way to hand
# either package to someone else.
#
# The source tree is never modified -- packing happens from a copy.
#
# Usage: ./tools/build-vl2.sh [options] [output-dir]
#
#   --host URL          the TNBrowser backend, baked into BOTH packages:
#                       $TNB::Host for the client and $TNBS::Host for the
#                       game-server mod. They are the same central server in
#                       every normal deployment, which is why this is one flag.
#                       Default http://localhost:8080.
#
# If your game servers must reach the backend at a different address than
# players do -- an internal hostname, or split-horizon DNS -- set $TNBS::Host in
# the game server's autoexec.cs. The baked value keeps its `if empty` guard, so
# that override still wins.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST="http://localhost:8080"
OUTDIR=""

while [ $# -gt 0 ]; do
    case "$1" in
        --host)        HOST="${2:?--host needs a URL}"; shift 2 ;;
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
            bake "$stage/tnbrowser/settings.cs" "TNB::Host" "$HOST" ;;
        TNBrowserServer)
            bake "$stage/tnbserver/settings.cs" "TNBS::Host" "$HOST" ;;
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
