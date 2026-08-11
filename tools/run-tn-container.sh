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
#   --mod DIR   host directory copied into GameData/ and launched with -mod.
#               A value that is NOT an existing directory is taken as the name
#               of a mod already in the image (Classic), and nothing is copied.
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
#   --online    pass -online, which sets $fromLauncher. Without it a launch that
#               is not -nologin dies at "In order to play Tribes 2 online, you
#               must launch the game using the supplied shortcuts."
#               (console_start.cs:393 sets the flag, :1097 is the refusal.) So
#               --login and --online belong together for anything interactive.
#   --foreground
#               attach the container to this terminal and to the host's X
#               display, with sound, instead of running detached and headless.
#               The telnet console is still published on <host-port>.
#   --vl2 FILE  copy a built package into the mod directory before the game
#               boots, which is how a player installs one. Repeatable. Needs
#               --mod, since that names the directory it lands in.
#   --no-gpu    do not try to give the container the host's GPU. On by default;
#               see the block below for what it takes on an NVIDIA host, which
#               is more than --device /dev/dri. Turn it off if the wiring
#               misbehaves -- the game then renders in software, slowly but
#               correctly.
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
DO_ONLINE=0
FOREGROUND=0
WITH_GPU=1
GAME_DIR_HOST=""
VL2S=()

while [ $# -gt 0 ]; do
    case "$1" in
        --mod) MOD_NAME="${2:?--mod needs a directory}"; shift 2 ;;
        --account) WITH_ACCOUNT=1; shift ;;
        # Without --rm the container survives a crash, so `docker logs` still
        # has the engine's output. Essential when a script kills the game.
        --keep) KEEP=1; shift ;;
        --login) DO_LOGIN=1; shift ;;
        --online) DO_ONLINE=1; shift ;;
        --foreground) FOREGROUND=1; shift ;;
        --no-gpu) WITH_GPU=0; shift ;;
        --vl2) VL2S+=("${2:?--vl2 needs a file}"); shift 2 ;;
        --game-dir) GAME_DIR_HOST="${2:?--game-dir needs a directory}"; shift 2 ;;
        -*) echo "Unknown option: $1" >&2; exit 1 ;;
        *) PORT="$1"; shift ;;
    esac
done

[ -n "$PORT" ] || { echo "Usage: $0 [--mod DIR] [--account] <host-port>" >&2; exit 1; }

if [ ${#VL2S[@]} -gt 0 ]; then
    [ -n "$MOD_NAME" ] || { echo "--vl2 needs --mod: a package is installed into a mod directory" >&2; exit 1; }
    for f in "${VL2S[@]}"; do
        [ -f "$f" ] || { echo "No such package: $f" >&2; exit 1; }
    done
fi

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
#
# -online is safe anywhere in the list: console_start.cs:393 handles it without
# touching $i, so unlike -mod and -telnetParams it consumes exactly one slot.
GAME_ARGS=(-telnetParams 2323 password listen)
[ "$DO_ONLINE" -eq 1 ] && GAME_ARGS=(-online "${GAME_ARGS[@]}")
[ "$DO_LOGIN" -eq 0 ] && GAME_ARGS=(-nologin "${GAME_ARGS[@]}")
[ -n "$MOD_NAME" ] && GAME_ARGS+=(-mod "$MOD_NAME")

RM_FLAG=(--rm)
[ "$KEEP" -eq 1 ] && RM_FLAG=()

# X11 and sound from the host, as the plugin's own run-container.sh does it.
# The xauth cookie is rewritten to the wildcard family (ffff) so it is accepted
# from inside the container's network namespace, where the hostname differs.
CREATE_ARGS=()
GUI_ARGS=()
if [ "$FOREGROUND" -eq 1 ]; then
    [ -n "${DISPLAY:-}" ] || { echo "--foreground needs \$DISPLAY set" >&2; exit 1; }
    command -v xauth >/dev/null || { echo "--foreground needs xauth" >&2; exit 1; }

    CREATE_ARGS=(-it)

    touch ~/.tribes2-xauth
    xauth nlist "$DISPLAY" | sed -e 's/^..../ffff/' | xauth -f ~/.tribes2-xauth nmerge -
    chmod 644 ~/.tribes2-xauth

    GUI_ARGS=(
        -e DISPLAY="$DISPLAY"
        -e XAUTHORITY=/tmp/xauth
        -v "$HOME/.tribes2-xauth:/tmp/xauth:ro"
        -v /tmp/.X11-unix:/tmp/.X11-unix
    )

    # Sound is optional: no PulseAudio socket just means a silent game, which
    # is not a reason to refuse to launch.
    PULSE="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/pulse/native"
    if [ -S "$PULSE" ]; then
        GUI_ARGS+=(-v "$PULSE:/tmp/pulse-native" -e PULSE_SERVER=unix:/tmp/pulse-native)
    else
        echo "No PulseAudio socket at $PULSE; running without sound" >&2
    fi
fi

# Hardware acceleration.
#
# The image already gets --device /dev/dri, which is enough on Mesa hardware but
# does nothing on a machine running NVIDIA's proprietary driver: Mesa looks for
# nouveau_dri.so, the kernel side is nvidia.ko, and the game falls back to
# llvmpipe. `docker logs` says so out loud --
#
#   libGL error: glx: failed to create dri3 screen
#   libGL error: failed to load driver: nouveau
#
# Two things are needed to fix that, and the second is the one everybody misses.
#
#   1. The devices and the 64-bit userspace, which `--gpus all` supplies through
#      nvidia-container-toolkit. NVIDIA_DRIVER_CAPABILITIES must include
#      `graphics`; the default (`utility,compute`) gives you nvidia-smi and CUDA
#      and no GL at all.
#   2. The **32-bit** userspace, which the toolkit does not inject. Tribes 2 is a
#      32-bit Windows binary and this Wine build has an i386-unix half, so the
#      process is a 32-bit ELF that can only load 32-bit GL. Verified by reading
#      /proc/<pid>/maps in a running container: i386-unix, i386-linux-gnu, and
#      not one nvidia library among them.
#
# So the host's own 32-bit driver libraries are bind-mounted in, each at its
# soname under the container's i386 multiarch directory, which is on the default
# loader path. Everything else they need -- libc, libX11, libxcb -- resolves to
# the container's own i386 libraries, which is why only the nvidia-specific
# files are mounted and nothing else from /usr/lib32.
GPU_ARGS=()
GPU_DESC="software (llvmpipe)"
if [ "$WITH_GPU" -eq 1 ]; then
    GPU_GIDS=()

    # Let the unprivileged user in the container open the card node: it is
    # mode 660 root:video, and wineuser is in no host group.
    for n in /dev/dri/card* /dev/dri/renderD*; do
        [ -e "$n" ] || continue
        g="$(stat -c %g "$n")"
        case " ${GPU_GIDS[*]-} " in *" $g "*) ;; *) GPU_GIDS+=("$g") ;; esac
    done
    for g in ${GPU_GIDS[@]+"${GPU_GIDS[@]}"}; do
        GPU_ARGS+=(--group-add "$g")
    done

    if [ -e /dev/nvidiactl ]; then
        if docker info --format '{{range .Runtimes}}{{.}}{{end}}' 2>/dev/null \
             | grep -q nvidia || command -v nvidia-container-runtime >/dev/null; then
            GPU_ARGS+=(--gpus all
                       -e NVIDIA_DRIVER_CAPABILITIES=graphics,display,utility)
        else
            # No toolkit: hand over the device nodes directly. They are
            # world-readable, so this is all the 32-bit libraries below need.
            for d in /dev/nvidia[0-9]* /dev/nvidiactl /dev/nvidia-modeset; do
                [ -e "$d" ] && GPU_ARGS+=(--device "$d")
            done
        fi

        # The 32-bit half. Symlinks are mounted resolved but under the link's
        # own name, because the name the loader asks for -- libGLX_nvidia.so.0 --
        # is a symlink on the host and a bind mount does not follow it.
        NV32=""
        for d in /usr/lib32 /usr/lib/i386-linux-gnu /usr/lib/i386-linux-gnu/nvidia; do
            [ -e "$d/libGLX_nvidia.so.0" ] && { NV32="$d"; break; }
        done
        if [ -n "$NV32" ]; then
            n=0
            for f in "$NV32"/libGLX_nvidia.so.[0-9]* "$NV32"/libEGL_nvidia.so.[0-9]* \
                     "$NV32"/libnvidia-*.so.[0-9]*; do
                [ -e "$f" ] || continue
                GPU_ARGS+=(-v "$(readlink -f "$f"):/usr/lib/i386-linux-gnu/$(basename "$f"):ro")
                n=$((n + 1))
            done
            GPU_DESC="NVIDIA (${n} 32-bit driver libraries mounted)"
        else
            echo "No 32-bit NVIDIA libraries on this host (lib32-nvidia-utils" >&2
            echo "or libnvidia-gl-*:i386). The 32-bit game cannot use the GPU." >&2
        fi
    elif [ -e /dev/dri/renderD128 ]; then
        GPU_DESC="Mesa (/dev/dri)"
    fi
fi

MOUNT_ARGS=()
if [ -n "$GAME_DIR_HOST" ]; then
    [ -d "$GAME_DIR_HOST/GameData" ] || {
        echo "--game-dir must name the directory containing GameData" >&2; exit 1; }
    GAME_DATA="/opt/tribes2/game/GameData"
    MOUNT_ARGS=(-v "$(cd "$GAME_DIR_HOST" && pwd):/opt/tribes2/game"
                -e GAME_DIR=/opt/tribes2/game)
fi

docker create "${RM_FLAG[@]}" "${CREATE_ARGS[@]}" "${GUI_ARGS[@]}" \
    ${GPU_ARGS[@]+"${GPU_ARGS[@]}"} \
    "${MOUNT_ARGS[@]}" --device /dev/dri \
    -p "$PORT:2323" --name "$NAME" \
    tribes2 "${GAME_ARGS[@]}" >/dev/null

echo "Rendering: $GPU_DESC" >&2

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

# Packages last, so one dropped in here wins over the same file arriving with a
# --mod directory. docker cp fails loudly if the mod directory does not exist,
# which is the check we want for a --mod naming a mod that is not in the image.
for f in ${VL2S[@]+"${VL2S[@]}"}; do
    docker cp "$f" "${NAME}:${GAME_DATA}/${MOD_NAME}/$(basename "$f")"
    echo "Installed $(basename "$f") into ${MOD_NAME}/" >&2
done

if [ "$FOREGROUND" -eq 1 ]; then
    echo "Starting $NAME on $DISPLAY (console on host port $PORT)" >&2
    exec docker start -ai "$NAME"
fi

docker start "$NAME" >/dev/null
echo "Started $NAME (console on host port $PORT)" >&2
