#!/bin/bash
# Run the TNBrowser test suites against a patched container and a fresh mock.
#
# The mock holds its fixtures in memory and every suite writes to them -- the
# sweep alone sends mail, deletes mail, invites a warrior and edits two
# profiles -- so it is restarted before *each* suite rather than once per run.
# Sharing one mock across suites makes an earlier suite's writes surface as a
# later suite's failures, which is exactly what happened the first time this was
# wired up.
#
# Usage: ./tools/run-tests.sh [port]
set -uo pipefail

PORT="${1:-2325}"
MOCK_PORT=8099
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
T2="${CLAUDE_PLUGIN_ROOT:-$HOME/.claude/plugins/cache/tribes-2-modding-skill/tribes2-modding/1.1.0}"
CONSOLE="python3 $T2/scripts/t2console.py --port $PORT"

# The container reaches the host through the default docker bridge gateway.
HOST_ADDR="http://172.17.0.1:${MOCK_PORT}"

restart_mock() {
    # Anchor the pattern on the python process. A bare -f pattern also matches
    # this script's own command line, so the runner would kill its own shell.
    pkill -f "^python3 .*mockserver\.py" 2>/dev/null
    sleep 0.4
    (setsid python3 "$ROOT/tools/mockserver.py" --port "$MOCK_PORT" --open-auth \
        > /tmp/tnbrowser-mock.log 2>&1 &)
    sleep 1
}

# The shim is loaded by the mod's own autoexec at boot, so nothing needs
# exec'ing here beyond the test files. GuidOverride stands in for a TribesNext
# login the container does not have: the mock accepts any account, and the guid
# is what its fixtures are keyed on.
SETUP='$TNB::GuidOverride = "4510186";'

echo "== deploying =="
"$ROOT/tools/deploy.sh" "$PORT" >/dev/null || exit 1

echo
echo "== json parser =="
$CONSOLE "$SETUP" 'exec("tests/json_test.cs"); TNBJsonSelfTest();' 2>&1 \
    | grep -E "^FAIL|TNBJSONRESULT"

echo
echo "== ordinal sweep =="
restart_mock
$CONSOLE "$SETUP" 'exec("tests/sweep_test.cs");' \
    "TNBSweepSelfTest(\"$HOST_ADDR\");" \
    --until '$TNBSweep::Done' --until-timeout 180 >/dev/null 2>&1

# The steps run from schedule() callbacks, so their console output lands
# between a runner's polls instead of on our connection. Read the tally back.
$CONSOLE 'echo("TNBSWEEPRESULT pass=" @ $TNBSweep::Pass @ " fail=" @ $TNBSweep::Fail); if ($TNBSweep::Fail > 0) echo($TNBSweep::Failures);' 2>&1 \
    | grep -E "TNBSWEEPRESULT|\(got |\(missing "

echo
echo "== browser =="
restart_mock
$CONSOLE "$SETUP" 'exec("tests/browser_test.cs");' \
    "TNBBrowserSelfTest(\"$HOST_ADDR\");" \
    --until '$TNBBrowserTest::Done' --until-timeout 120 >/dev/null 2>&1

$CONSOLE 'echo("TNBBROWSERRESULT pass=" @ $TNBBrowserTest::Pass @ " fail=" @ $TNBBrowserTest::Fail); if ($TNBBrowserTest::Fail > 0) echo($TNBBrowserTest::Failures);' 2>&1 \
    | grep -E "TNBBROWSERRESULT|\(got |\(missing "

echo
echo "== mail =="
restart_mock
$CONSOLE "$SETUP" 'exec("tests/mail_test.cs");' \
    "TNBMailSelfTest(\"$HOST_ADDR\");" \
    --until '$TNBMailTest::Done' --until-timeout 120 >/dev/null 2>&1

$CONSOLE 'echo("TNBMAILRESULT pass=" @ $TNBMailTest::Pass @ " fail=" @ $TNBMailTest::Fail); if ($TNBMailTest::Fail > 0) echo($TNBMailTest::Failures);' 2>&1 \
    | grep -E "TNBMAILRESULT|\(got |\(missing "

echo
echo "== chat =="
restart_mock
# Last, and after a restart, because chat is stateful in a way the other panes
# are not: who is in which room lives in the mock's memory rather than in a
# fixture, so a suite that ran before this one leaves rooms behind. The suite
# also leaves a stream open, which is why nothing follows it.
$CONSOLE "$SETUP" 'exec("tests/chat_test.cs");' \
    "TNBChatSelfTest(\"$HOST_ADDR\");" \
    --until '$TNBChatTest::Done' --until-timeout 120 >/dev/null 2>&1

$CONSOLE 'echo("TNBCHATRESULT pass=" @ $TNBChatTest::Pass @ " fail=" @ $TNBChatTest::Fail); if ($TNBChatTest::Fail > 0) echo($TNBChatTest::Failures);' 2>&1 \
    | grep -E "TNBCHATRESULT|\(got |\(missing "
