#!/bin/bash
# Run the TNBrowser test suites against a patched container and a fresh mock.
#
# The mock holds its fixtures in memory and the write tests mutate them
# (promotions, invitations, profile edits), so it is restarted before *each*
# suite rather than once per run. Sharing one mock across suites makes the API
# suite's writes surface as GUI-suite failures -- which is exactly what happened
# the first time this was wired up: the API suite promotes a member and rewrites
# a profile, then the GUI suite asserts on the original fixture values.
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
    (setsid python3 "$ROOT/tools/mockserver.py" --port "$MOCK_PORT" \
        > /tmp/tnbrowser-mock.log 2>&1 &)
    sleep 1
}

LOAD='exec("tnbrowser/settings.cs"); exec("tnbrowser/json.cs");
      exec("tnbrowser/session.cs"); exec("tnbrowser/api.cs");
      exec("tnbrowser/panes.cs"); exec("tnbrowser/cert.cs");
      exec("tnbrowser/clanprops.cs"); exec("tnbrowser/playerprops.cs");
      exec("tnbrowser/mail.cs");'

echo "== deploying =="
"$ROOT/tools/deploy.sh" "$PORT" >/dev/null || exit 1

echo
echo "== json parser =="
$CONSOLE "$LOAD" 'exec("tests/json_test.cs"); TNBJsonSelfTest();' 2>&1 \
    | grep -E "^FAIL|TNBJSONRESULT"

echo
echo "== api + session =="
restart_mock
$CONSOLE "$LOAD" 'exec("tests/api_test.cs");' \
    "TNBApiSelfTest(\"$HOST_ADDR\");" \
    --until '$TNBApiTest::Pass + $TNBApiTest::Fail >= 36' \
    --until-timeout 120 >/dev/null 2>&1

$CONSOLE 'echo("TNBAPIRESULT pass=" @ $TNBApiTest::Pass @ " fail=" @ $TNBApiTest::Fail); if ($TNBApiTest::Fail > 0) echo($TNBApiTest::Failures);' 2>&1 \
    | grep -E "TNBAPIRESULT|\(got "

echo
echo "== gui =="
restart_mock
$CONSOLE "$LOAD" 'exec("tests/gui_test.cs");' \
    "TNBGuiSelfTest(\"$HOST_ADDR\");" \
    --until '$TNBGuiTest::Done' --until-timeout 120 >/dev/null 2>&1

# The GUI steps run from schedule() callbacks, so their console output lands
# between --until polls instead of on our connection. Read the recorded tally.
$CONSOLE 'echo("TNBGUIRESULT pass=" @ $TNBGuiTest::Pass @ " fail=" @ $TNBGuiTest::Fail); if ($TNBGuiTest::Fail > 0) echo($TNBGuiTest::Failures);' 2>&1 \
    | grep -E "TNBGUIRESULT|\(got |\(missing "

echo
echo "== mail =="
restart_mock
$CONSOLE "$LOAD" 'exec("tests/mail_test.cs");' \
    "TNBMailSelfTest(\"$HOST_ADDR\");" \
    --until '$TNBMailTest::Done' --until-timeout 120 >/dev/null 2>&1

$CONSOLE 'echo("TNBMAILRESULT pass=" @ $TNBMailTest::Pass @ " fail=" @ $TNBMailTest::Fail); if ($TNBMailTest::Fail > 0) echo($TNBMailTest::Failures);' 2>&1 \
    | grep -E "TNBMAILRESULT|\(got |\(missing "
