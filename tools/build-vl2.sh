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
# Usage: ./tools/build-vl2.sh [output-dir]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTDIR="${1:-$ROOT/dist}"

command -v zip >/dev/null || { echo "zip is required" >&2; exit 1; }
mkdir -p "$OUTDIR"

# $1 mod directory, $2 archive name, $3.. directories to include
pack() {
    local moddir="$1" name="$2"; shift 2
    local out="$OUTDIR/$name"
    rm -f "$out"

    cd "$ROOT/$moddir"
    # .dso files are compiled output and must never ship: the engine only
    # recompiles when the source is newer, so a stale .dso baked into the
    # archive would shadow the script it was built from on every install.
    find . -name '*.dso' -print -delete | sed 's/^/  dropping stale /' >&2 || true

    zip -0 -q -r "$out" "$@" -x '*.dso' -x '.*' -x '*/.*'
    echo "Built $out"
    unzip -l "$out" | tail -n +4 | head -n -2 | awk '{print "  " $4}' | sed '/^  $/d'
    echo
}

pack TNBrowser       TNBrowser.vl2       scripts tnbrowser
pack TNBrowserServer TNBrowserServer.vl2 scripts tnbserver

echo "Install: copy either into GameData/<MOD>/ and launch with -mod <MOD>"
