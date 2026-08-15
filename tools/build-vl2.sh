#!/bin/bash
# Build TNBrowser.vl2 -- the drop-in package.
#
# A .vl2 is just a zip. The engine builds its resource index from each mod
# directory's loose files *plus the contents of its .vl2 archives*, so paths
# inside the archive are resolved exactly as if they had been unpacked into the
# mod directory. That is why the archive root must be the mod root: the entry
# for the shim has to be "tnbrowser/dbproxy.cs", not
# "TNBrowser/tnbrowser/dbproxy.cs".
#
# The client package contains no .gui file, and that is the point rather than an
# omission: the community screens a player sees are the shipped ones.
#
# Install:  drop the .vl2 into GameData/base/ and start the game normally. base
#           is the mod directory the engine always loads, so there is no -mod
#           flag to pass and the screens work under whichever mod is played.
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
# is self-contained: dropping it into GameData/base/ is the whole install, with
# nothing else to create or edit. That is the sane way to hand either package to
# someone else.
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
#   --clan-key FILE     the public key game servers check clan certificates
#                       against, as written by `tnserver -genkey` (the
#                       <key>.pub.cs file beside the private one). It replaces
#                       tnbserver/clankeys.cs inside TNBrowserServer.vl2.
#                       Public data: it verifies signatures and cannot make
#                       them. Without it the server package still installs and
#                       runs, and simply tags nobody.
#
# If your game servers must reach the backend at a different address than
# players do -- an internal hostname, or split-horizon DNS -- set $TNBS::Host in
# the game server's autoexec.cs. The baked value keeps its `if empty` guard, so
# that override still wins.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST="http://localhost:8080"
CLANKEY=""
OUTDIR=""

while [ $# -gt 0 ]; do
    case "$1" in
        --host)        HOST="${2:?--host needs a URL}"; shift 2 ;;
        --clan-key)    CLANKEY="${2:?--clan-key needs a file}"; shift 2 ;;
        -*) echo "Unknown option: $1" >&2; exit 1 ;;
        *) OUTDIR="$1"; shift ;;
    esac
done

if [ -n "$CLANKEY" ]; then
    [ -f "$CLANKEY" ] || { echo "No such file: $CLANKEY" >&2; exit 1; }
    # A private key would work here and must never be shipped, so refuse the
    # one mistake that would publish one.
    if grep -q 'PRIVATE KEY' "$CLANKEY"; then
        echo "$CLANKEY looks like a private key. Pass the .pub.cs file beside it." >&2
        exit 1
    fi
    grep -q 'TNBS::ClanKeyN' "$CLANKEY" || {
        echo "$CLANKEY defines no \$TNBS::ClanKeyN; is it the file -genkey wrote?" >&2
        exit 1
    }
fi
OUTDIR="${OUTDIR:-$ROOT/dist}"

command -v zip >/dev/null || { echo "zip is required" >&2; exit 1; }
mkdir -p "$OUTDIR"

# Absolute from here on: pack() cds into the staging directory before running
# zip, so a relative output path given on the command line would be resolved
# from there and fail with "Could not create output file".
OUTDIR="$(cd "$OUTDIR" && pwd)"

# SOURCE_DATE_EPOCH is the reproducible-builds convention; honour it if set.
STAMP="${SOURCE_DATE_EPOCH:-$(git -C "$ROOT" log -1 --format=%ct 2>/dev/null || date +%s)}"

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
            bake "$stage/tnbserver/settings.cs" "TNBS::Host" "$HOST"
            # Substituted wholesale rather than rewritten line by line: the
            # file holds any number of keys, and the one -genkey wrote is
            # already exactly what the mod execs.
            if [ -n "$CLANKEY" ]; then
                cp "$CLANKEY" "$stage/tnbserver/clankeys.cs"
                echo "  clan key from $CLANKEY" >&2
            fi ;;
    esac

    # Stamp every entry with one timestamp so a rebuild of unchanged sources
    # produces an identical archive.
    #
    # The stamp is the last commit's date rather than a fixed epoch, because the
    # engine only recompiles a script when the source is newer than its .dso: a
    # 2001 timestamp would let a stale .dso from a previous install shadow every
    # file in the archive.
    find "$stage" -exec touch -h -d "@$STAMP" {} +

    cd "$stage"
    zip -0 -X -q -r "$out" "$@" -x '*.dso' -x '.*' -x '*/.*'
    echo "Built $out"
    unzip -l "$out" | tail -n +4 | head -n -2 | awk '{print "  " $4}' | sed '/^  $/d'
    echo
}

pack TNBrowser       TNBrowser.vl2       scripts tnbrowser
pack TNBrowserServer TNBrowserServer.vl2 scripts tnbserver

echo "Install: copy either into GameData/base/ -- no -mod flag needed"
