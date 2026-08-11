#!/bin/bash
# Launch a real client, the way a player runs one, against the live backend.
#
# On screen, logged in, with the packaged .vl2 installed into a mod directory --
# which is the configuration the test suites cannot reach and the one every
# interesting failure has needed. The suites run headless with -nologin, no
# account and the mod injected as loose files; three fixes shipped from that
# footing turned out to be wrong because the fault only appears against real TLS
# with a real GUID.
#
# What this arranges, and why each part is load-bearing:
#
#   -online     sets $fromLauncher. Without it a launch that is not -nologin
#               stops at "In order to play Tribes 2 online, you must launch the
#               game using the supplied shortcuts" (console_start.cs:1097).
#   no -nologin registers TribesNext's t2csri_* natives, without which there is
#               no login screen, no account and no certificate.
#   the .vl2    is the artifact players install, with the backend address baked
#               into its settings.cs. Testing the loose source tree instead
#               tests something nobody runs.
#
# The game therefore boots to the login screen and stops there: console_end.cs,
# and with it the mod's scripts/autoexec, does not run until you log in. That is
# the path being exercised, not an obstacle -- the identity bug fixed in cd162cb
# lived precisely in what was read before the login completed, and a client
# started with -nologin cannot reach it at all.
#
# The telnet console is still published, so drive it from another shell:
#
#   T2="$HOME/.claude/plugins/cache/tribes-2-modding-skill/tribes2-modding/1.1.0"
#   python3 "$T2/scripts/t2console.py" --port 2323 'echo($TNB::Host);'
#
# Usage: ./tools/run-live-client.sh [--host URL] [--mod NAME] [--no-account] [port]
#
#   --host URL     backend baked into both packages
#                  (default https://tnb.k8s.henrik.si)
#   --mod NAME     mod directory the packages are installed into, and the mod
#                  the game launches with (default Classic, which is what the
#                  shipped Classic_online.bat runs)
#   --no-account   do not inject public.store/private.store. You then get the
#                  login screen with no accounts on it and have to create one.
#   --no-gpu       render in software. The container is given the host's GPU by
#                  default, including the 32-bit driver libraries a 32-bit Wine
#                  process needs and nvidia-container-toolkit does not inject.
#   port           host port for the telnet console (default 2323)
#
# 2323 is also the port a Tribes 2 running natively on this machine listens on,
# so pick another one to run both at once.
#
# The container is --rm with no volume: it writes nothing to the real install,
# and everything in it -- prefs, .dso files, the injected key stores -- goes
# away when the game exits.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

HOST="https://tnb.k8s.henrik.si"
MOD="Classic"
PORT="2323"
WITH_ACCOUNT=1
WITH_GPU=1

while [ $# -gt 0 ]; do
    case "$1" in
        --host) HOST="${2:?--host needs a URL}"; shift 2 ;;
        --mod) MOD="${2:?--mod needs a mod name}"; shift 2 ;;
        --no-account) WITH_ACCOUNT=0; shift ;;
        --no-gpu) WITH_GPU=0; shift ;;
        -*) echo "Unknown option: $1" >&2; exit 1 ;;
        *) PORT="$1"; shift ;;
    esac
done

[ -n "${DISPLAY:-}" ] || {
    echo "No \$DISPLAY. This launcher puts the game on your screen; for a" >&2
    echo "headless one use tools/run-tn-container.sh." >&2
    exit 1
}

# Built from the working tree rather than downloaded, so uncommitted changes are
# what gets tested -- the point of running this is usually to try something not
# yet pushed.
#
# Into dist/live rather than a temp directory for two reasons: dist/ normally
# holds a localhost build and clobbering it would be a nasty surprise, and the
# exact bytes just run stay on disk afterwards to unzip and check.
OUT="$ROOT/dist/live"
echo "== building packages for $HOST ==" >&2
"$ROOT/tools/build-vl2.sh" --host "$HOST" "$OUT" >/dev/null

ARGS=(--login --online --foreground --mod "$MOD"
      --vl2 "$OUT/TNBrowser.vl2"
      --vl2 "$OUT/TNBrowserServer.vl2")

# The client package is the one under test. The server package rides along
# because it is inert on a client -- it makes no HTTP requests at all -- and a
# listen server hosted from this container then tags connecting players, which
# is the other half of the deployment for free.

# On by default here, unlike run-tn-container.sh where it is opt-in: a live
# authenticated session is the entire purpose of this script.
[ "$WITH_ACCOUNT" -eq 1 ] && ARGS+=(--account)
[ "$WITH_GPU" -eq 0 ] && ARGS+=(--no-gpu)

echo "== launching $MOD against $HOST ==" >&2
exec "$ROOT/tools/run-tn-container.sh" "${ARGS[@]}" "$PORT"
