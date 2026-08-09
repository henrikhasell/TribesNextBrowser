#!/bin/bash
# Run a TribesNext-patched Tribes 2 container for developing/testing TNBrowser.
#
# The stock plugin image ships an unpatched client: its Tribes2.exe is
# byte-identical to a patched install's, but IFC22.dll is Sierra's original
# (md5 97fdb632... — the same file a patched install keeps as IFC22.dll.bak).
# TribesNext works by hijacking that DLL, so this script copies the patched
# IFC22.dll and its support files out of a real patched install and into the
# container *before the game boots*.
#
# That ordering is the whole point of create/cp/start: the DLL is loaded at
# process start, and the container is --rm with no volume, so anything copied
# after `docker start` is too late to matter.
#
# Usage: ./tools/run-tn-container.sh [--mod DIR] [--account] [--keep] <host-port>
#
#   --mod DIR   host directory copied into GameData/ and launched with -mod
#   --account   also inject public.store/private.store so the container can log
#               in as the installed TribesNext account. Off by default: those
#               are the user's RSA key material and are not needed for anything
#               except a live authenticated session.
#   --keep      do not pass --rm, so a crashed container survives for `docker
#               logs`. Without it a crash erases the evidence.
#   --login     do NOT pass -nologin, so the game runs its TribesNext login
#               flow. Required for anything touching an account: with -nologin
#               the patch never registers its t2csri_* console functions at all
#               (t2csri_listAccounts and friends report "Unable to find
#               function"), so no login and no certificate is possible. Costs
#               you the autoexec pass, since the game stops at the login screen
#               and console_end.cs never runs -- exec the mod by hand instead.
#   --game-dir D
#               bind-mount a real Tribes 2 install at D (the directory holding
#               GameData) and point the image's GAME_DIR at it, instead of
#               injecting patch files into the image's own install. Simpler and
#               always complete, but the game then writes prefs, .dso files and
#               the injected mod into that real install.
set -euo pipefail

SRC="${TN_SOURCE_INSTALL:-$HOME/.wine/drive_c/Dynamix/Tribes2/GameData}"
GAME_DATA=/opt/tribes2/prefix/drive_c/Dynamix/Tribes2/GameData

PORT=""
MOD_NAME=""
MOD_SRC=""
WITH_ACCOUNT=0
KEEP=0
DO_LOGIN=0
GAME_DIR_HOST=""

while [ $# -gt 0 ]; do
    case "$1" in
        --mod) MOD_NAME="${2:?--mod needs a directory}"; shift 2 ;;
        --account) WITH_ACCOUNT=1; shift ;;
        # Without --rm the container survives a crash, so `docker logs` still
        # has the engine's output. Essential when a script kills the game.
        --keep) KEEP=1; shift ;;
        --login) DO_LOGIN=1; shift ;;
        --game-dir) GAME_DIR_HOST="${2:?--game-dir needs a directory}"; shift 2 ;;
        -*) echo "Unknown option: $1" >&2; exit 1 ;;
        *) PORT="$1"; shift ;;
    esac
done

[ -n "$PORT" ] || { echo "Usage: $0 [--mod DIR] [--account] <host-port>" >&2; exit 1; }

if [ -n "$MOD_NAME" ] && [ -d "$MOD_NAME" ]; then
    MOD_SRC="${MOD_NAME%/}"
    MOD_NAME="$(basename "$MOD_SRC")"
fi

# The TribesNext hook DLL plus everything it pulls in. Tribes2.exe.local is a
# one-byte marker that enables per-application DLL redirection — without it the
# loader may resolve IFC22.dll from the system path instead of the game dir, and
# the patch silently never loads.
#
# The Miles Sound System files are not optional cosmetics: the patched
# IFC22.dll imports _AIL_mem_use_malloc, which only the newer Mss32.dll
# exports. Ship the old one and the game aborts during startup with
# "wine: ... unimplemented function Mss32.dll._AIL_mem_use_malloc".
PATCH_FILES=(
    IFC22.dll             # TribesNext hook: HTTPObject/libcurl, t2csri_* natives
    Tribes2.exe.local     # forces app-dir DLL resolution
    libcurl.dll           # HTTPS transport
    curl-ca-bundle.crt    # CA roots for certificate verification
    SDL3.dll
    soft_oal.dll
    kver.pub
    Mss32.dll             # newer Miles; IFC22.dll depends on its exports
    Mp3dec.asi
    Mssds3dh.m3d
    Mssds3ds.m3d
    Mssdx7sn.m3d
    Msseax.m3d
    Msseax2.m3d
    Mssfast.m3d
    Reverb3.flt
)

# Only needed for the injection path; --game-dir brings its own patched install.
if [ -z "$GAME_DIR_HOST" ]; then
    for f in "${PATCH_FILES[@]}"; do
        [ -e "$SRC/$f" ] || { echo "Missing patch file: $SRC/$f" >&2; exit 1; }
    done
    [ -e "$SRC/base/t2csri.vl2" ] || { echo "Missing $SRC/base/t2csri.vl2" >&2; exit 1; }
fi

NAME="tribes2-${PORT}"
docker rm -f "$NAME" >/dev/null 2>&1 || true

# -mod must come last and -telnetParams needs its third (listen) argument
# spelled out; see run-container.sh in the plugin for why.
GAME_ARGS=(-telnetParams 2323 password listen)
[ "$DO_LOGIN" -eq 0 ] && GAME_ARGS=(-nologin "${GAME_ARGS[@]}")
[ -n "$MOD_NAME" ] && GAME_ARGS+=(-mod "$MOD_NAME")

RM_FLAG=(--rm)
[ "$KEEP" -eq 1 ] && RM_FLAG=()

MOUNT_ARGS=()
if [ -n "$GAME_DIR_HOST" ]; then
    [ -d "$GAME_DIR_HOST/GameData" ] || {
        echo "--game-dir must name the directory containing GameData" >&2; exit 1; }
    GAME_DATA="/opt/tribes2/game/GameData"
    MOUNT_ARGS=(-v "$(cd "$GAME_DIR_HOST" && pwd):/opt/tribes2/game"
                -e GAME_DIR=/opt/tribes2/game)
fi

docker create "${RM_FLAG[@]}" "${MOUNT_ARGS[@]}" --device /dev/dri \
    -p "$PORT:2323" --name "$NAME" \
    tribes2 "${GAME_ARGS[@]}" >/dev/null

if [ -z "$GAME_DIR_HOST" ]; then
    for f in "${PATCH_FILES[@]}"; do
        docker cp "$SRC/$f" "${NAME}:${GAME_DATA}/$f"
    done
    docker cp "$SRC/base/t2csri.vl2" "${NAME}:${GAME_DATA}/base/t2csri.vl2"
    echo "Injected TribesNext patch from $SRC" >&2
else
    echo "Using the real install mounted from $GAME_DIR_HOST" >&2
fi

if [ "$WITH_ACCOUNT" -eq 1 ] && [ -z "$GAME_DIR_HOST" ]; then
    for f in public.store private.store; do
        [ -e "$SRC/$f" ] && docker cp "$SRC/$f" "${NAME}:${GAME_DATA}/$f"
    done
    echo "Injected account key stores" >&2
fi

if [ -n "$MOD_SRC" ]; then
    docker cp "$MOD_SRC" "${NAME}:${GAME_DATA}/${MOD_NAME}"
    echo "Injected ${MOD_SRC} as mod '${MOD_NAME}'" >&2
fi

docker start "$NAME" >/dev/null
echo "Started $NAME (console on host port $PORT)" >&2
