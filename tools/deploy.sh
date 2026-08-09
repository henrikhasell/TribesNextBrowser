#!/bin/bash
# Push the mod (and tests) into a running container and rebuild the resource
# index, so exec() can find the new files.
#
# The engine builds its resource index when mod paths are set, from the mod
# directories' loose files plus their .vl2 contents. A file copied in later is
# invisible -- "Missing file: ..." -- until rebuildModPaths() runs. Hence the
# rebuild here rather than in every caller.
#
# Compiled .dso files are deleted first: the engine only recompiles when the
# source is newer, and a stale .dso silently shadows an edited script.
#
# Usage: ./tools/deploy.sh [port] [mod-dir-name]
set -euo pipefail

PORT="${1:-2325}"
MOD="${2:-TNBrowser}"
NAME="tribes2-${PORT}"
GAME_DATA=/opt/tribes2/prefix/drive_c/Dynamix/Tribes2/GameData
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

T2="${CLAUDE_PLUGIN_ROOT:-$HOME/.claude/plugins/cache/tribes-2-modding-skill/tribes2-modding/1.1.0}"

docker exec "$NAME" bash -lc "find '$GAME_DATA' -name '*.dso' -newermt '2020-01-01' -delete 2>/dev/null || true"

# The mod's own tree.
docker cp "$ROOT/$MOD/." "${NAME}:${GAME_DATA}/${MOD}/"

# Tests live outside the shipped mod but need to be reachable by exec(), so
# they go into the same mod directory.
# Note the trailing "/." : `docker cp dir container:dest` nests dir *inside*
# dest when dest already exists, which silently leaves the previous copy in
# place to shadow the new one.
if [ -d "$ROOT/tests" ]; then
    docker exec "$NAME" mkdir -p "${GAME_DATA}/${MOD}/tests"
    docker cp "$ROOT/tests/." "${NAME}:${GAME_DATA}/${MOD}/tests/"
fi

# Refresh the resource index so exec() can see the files just copied in.
#
# NOT rebuildModPaths(): despite the name, in this build it resets the mod path
# stack to just "base", silently unloading the mod. Verified directly --
# getModPaths() reports "TNBrowser;base" before the call and "base" after.
# setModPaths() rebuilds the index too, and appends "base" itself, so passing
# the mod name alone restores exactly the boot-time stack.
python3 "$T2/scripts/t2console.py" --port "$PORT" \
    "setModPaths(\"$MOD\"); echo(\"modpaths=\" @ getModPaths());"
echo "Deployed $MOD to $NAME" >&2
