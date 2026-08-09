#!/bin/bash
# Verify TNBrowser against the real TribesNext backend.
#
# This is the one check the mock cannot do: answering a genuine RSA challenge
# needs the account private key, and private.store is encrypted with the account
# password. So this script asks for that password, logs the game in with it, and
# then runs a live session negotiation followed by an authenticated API call.
#
# The password is read with `read -s` (never echoed) and handed to the game over
# the console connection's stdin -- not as a command-line argument, so it does
# not appear in the process list, and never touches disk.
#
# Two things this got wrong the first time, both now handled:
#
#   * It booted with -nologin. That flag does not merely skip the login screen:
#     with it the patch never registers its t2csri_* console functions, so
#     t2csri_loginAccount does not exist and no account can be loaded. The
#     container has TribesNext either way -- HTTPS and sha1sum work fine -- but
#     the account subsystem is absent. Hence --login below.
#   * It printed whatever `$s = t2csri_loginAccount(...)` evaluated to. Calling a
#     *missing* function in this engine prints "Unable to find function" and the
#     assignment still yields a meaningless number, so the script cheerfully
#     reported "LOGIN=23" for a call that never happened. It now checks for that
#     message and requires the literal status "SUCCESS".
#
# Usage: ./tools/live-check.sh [port]
set -uo pipefail

PORT="${1:-2327}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
T2="${CLAUDE_PLUGIN_ROOT:-$HOME/.claude/plugins/cache/tribes-2-modding-skill/tribes2-modding/1.1.0}"
SRC="${TN_SOURCE_INSTALL:-$HOME/.wine/drive_c/Dynamix/Tribes2/GameData}"
CONSOLE="python3 $T2/scripts/t2console.py --port $PORT"

# The account name is the first tab-separated field of public.store.
ACCOUNT="$(cut -f1 "$SRC/public.store" 2>/dev/null | head -1)"
[ -n "$ACCOUNT" ] || { echo "Could not read an account name from $SRC/public.store" >&2; exit 1; }

echo "== starting a patched container (login flow enabled, account injected) =="
"$ROOT/tools/run-tn-container.sh" --keep --login --account --mod "$ROOT/TNBrowser" "$PORT" || exit 1

echo "== waiting for the game to boot =="
$CONSOLE --until 'true' --until-timeout 240 >/dev/null 2>&1 || {
    echo "Game did not come up" >&2; exit 1; }

# With --login the game stops at the login screen, so console_end.cs never runs
# and the mod's autoexec never fires. Load it by hand.
GD=/opt/tribes2/prefix/drive_c/Dynamix/Tribes2/GameData
docker exec "tribes2-${PORT}" mkdir -p "$GD/TNBrowser/tests" 2>/dev/null
docker cp "$ROOT/tests/." "tribes2-${PORT}:$GD/TNBrowser/tests/" 2>/dev/null

echo "== checking the account subsystem is present =="
$CONSOLE 'echo("ACCOUNTS=[" @ t2csri_listAccounts() @ "]");' 2>&1 \
    | grep -qE "Unable to find function" && {
        echo "  t2csri_* functions are missing -- the patch did not register them." >&2
        echo "  (This is what -nologin causes; run-tn-container.sh needs --login.)" >&2
        exit 1; }
echo "  ok"

echo
read -rsp "TribesNext password for ${ACCOUNT}: " TN_PASSWORD
echo
echo

echo "== logging in as ${ACCOUNT} =="
LOGIN_OUT=$(ACCOUNT="$ACCOUNT" TN_PASSWORD="$TN_PASSWORD" \
T2CONSOLE="$T2/scripts/t2console.py" PORT="$PORT" python3 -c '
import os, subprocess, sys
def q(s):
    return "\"" + s.replace("\\", "\\\\").replace("\"", "\\\"") + "\""
acct, pw = os.environ["ACCOUNT"], os.environ["TN_PASSWORD"]
stmts = [
    "setModPaths(\"TNBrowser\");",
    "exec(\"tnbrowser/settings.cs\"); exec(\"tnbrowser/json.cs\"); exec(\"tnbrowser/session.cs\"); exec(\"tnbrowser/api.cs\"); exec(\"tnbrowser/panes.cs\"); exec(\"tests/live_check.cs\");",
    "echo(\"LOGIN=\" @ t2csri_loginAccount(%s, %s));" % (q(acct), q(pw)),
    "$LoginCertificate = t2csri_getAccountCertificate(); echo(\"CERTGUID=\" @ getField($LoginCertificate, 1));",
]
p = subprocess.run([sys.executable, os.environ["T2CONSOLE"], "--port", os.environ["PORT"]],
                   input="\n".join(stmts) + "\n", text=True, capture_output=True)
sys.stdout.write(p.stdout)
' 2>&1)
unset TN_PASSWORD

echo "$LOGIN_OUT" | grep -E "^LOGIN=|^CERTGUID=" | sed 's/^/   /'

if ! echo "$LOGIN_OUT" | grep -q "^LOGIN=SUCCESS"; then
    echo
    echo "Login did not return SUCCESS -- stopping before the live calls." >&2
    echo "INVALID_PASSWORD means the password was wrong; anything else means the" >&2
    echo "account could not be loaded from public.store/private.store." >&2
    exit 1
fi

echo
echo "== negotiating a live session and calling the JSON browser API =="
$CONSOLE 'TNBLiveStart();' >/dev/null 2>&1
$CONSOLE --until '$TNBLive::Stage $= "done"' --until-timeout 90 >/dev/null 2>&1

$CONSOLE 'echo("RESULT=" @ $TNBLive::Status); echo("SESSION=" @ (TNBSessionReady() ? "yes" : "no")); echo("NAME=" @ $TNBLive::Name); echo("CLANS=" @ $TNBLive::Clans); echo("DETAIL=" @ $TNBLive::Detail);' 2>&1 \
    | grep -E "^RESULT=|^SESSION=|^NAME=|^CLANS=|^DETAIL=" | sed 's/^/   /'

echo
echo "RESULT=ok means the whole chain works: RSA challenge/response, a live"
echo "session UUID, and that UUID authorising the documented JSON browser API."
echo
echo "Container ${PORT} left running; stop it with:  docker rm -f tribes2-${PORT}"
