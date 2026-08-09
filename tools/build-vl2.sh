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
# Usage: ./tools/build-vl2.sh [output-path]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-$ROOT/dist/TNBrowser.vl2}"

command -v zip >/dev/null || { echo "zip is required" >&2; exit 1; }

mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"

# .dso files are compiled output and must never ship: the engine only recompiles
# when the source is newer, so a stale .dso baked into the archive would shadow
# the script it was built from on every install.
cd "$ROOT/TNBrowser"
find . -name '*.dso' -print -delete | sed 's/^/  dropping stale /' >&2 || true

zip -0 -q -r "$OUT" \
    scripts tnbrowser \
    -x '*.dso' -x '.*' -x '*/.*'

echo "Built $OUT"
unzip -l "$OUT" | tail -n +4 | head -n -2 | awk '{print "  " $4}' | sed '/^  $/d'
echo
echo "Install: copy it into GameData/<MOD>/ and launch with -mod <MOD>"
