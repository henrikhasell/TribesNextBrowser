#!/bin/bash
# Run the client test suites against the Go backend instead of the mock.
#
# The suites were written against tools/mockserver.py, which was written against
# TribesNext's published PHP. Loading the same fixtures into the Go server and
# running them unchanged is the conformance check: a failure here is a real
# behavioural difference between the two backends.
#
# The session still comes from the mock (it mints tokens the way TribesNext's
# robot login does), while the data comes from the Go server -- which is exactly
# the split the design intends: auth upstream, data self-hosted.
#
# Usage: ./tools/run-conformance.sh [game-port]
set -uo pipefail

PORT="${1:-2325}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
T2="${CLAUDE_PLUGIN_ROOT:-$HOME/.claude/plugins/cache/tribes-2-modding-skill/tribes2-modding/1.1.0}"
CONSOLE="python3 $T2/scripts/t2console.py --port $PORT"

AUTH="http://172.17.0.1:8099"    # mock: mints session tokens
DATA="http://172.17.0.1:8080"    # Go backend: serves the data

reseed() {
    docker exec -i tnb-postgres psql -q -U tnbrowser -d tnbrowser -v ON_ERROR_STOP=1 \
        < "$ROOT/server/testdata/seed.sql" >/dev/null || exit 1
}

echo "== restarting the mock (session source) =="
pkill -f "^python3 .*mockserver\.py" 2>/dev/null
sleep 0.4
(setsid python3 "$ROOT/tools/mockserver.py" --port 8099 > /tmp/tnbrowser-mock.log 2>&1 &)
sleep 1

"$ROOT/tools/deploy.sh" "$PORT" >/dev/null || exit 1

# The shim is loaded by the mod's own autoexec at boot; only the tests need
# exec'ing. GuidOverride stands in for a TribesNext login the container does
# not have -- the seed data is keyed on that guid.
LOAD='$TNB::GuidOverride = "4510186";'

# The suites set $TNB::Host themselves; auth stays on the mock, which mints
# session tokens the way TribesNext's robot login does.
SPLIT="\$TNB::AuthHost = \"$AUTH\";"

echo
echo "== ordinal sweep, against the Go backend =="
reseed
$CONSOLE "$LOAD" 'exec("tests/sweep_test.cs");' \
    "TNBSweepSelfTest(\"$DATA\"); $SPLIT" \
    --until '$TNBSweep::Done' --until-timeout 180 >/dev/null 2>&1
$CONSOLE 'echo("CONFORMANCE-API pass=" @ $TNBSweep::Pass @ " fail=" @ $TNBSweep::Fail); if ($TNBSweep::Fail > 0) echo($TNBSweep::Failures);' 2>&1 \
    | grep -E "CONFORMANCE-API|\(got "

echo
echo "== browser, against the Go backend =="
reseed
$CONSOLE "$LOAD" 'exec("tests/browser_test.cs");' \
    "TNBBrowserSelfTest(\"$DATA\"); $SPLIT" \
    --until '$TNBBrowserTest::Done' --until-timeout 150 >/dev/null 2>&1
$CONSOLE 'echo("CONFORMANCE-GUI pass=" @ $TNBBrowserTest::Pass @ " fail=" @ $TNBBrowserTest::Fail); if ($TNBBrowserTest::Fail > 0) echo($TNBBrowserTest::Failures);' 2>&1 \
    | grep -E "CONFORMANCE-GUI|\(got |\(missing "

echo
echo "== mail, against the Go backend =="
reseed
$CONSOLE "$LOAD" 'exec("tests/mail_test.cs");' \
    "TNBMailSelfTest(\"$DATA\"); $SPLIT" \
    --until '$TNBMailTest::Done' --until-timeout 150 >/dev/null 2>&1
$CONSOLE 'echo("CONFORMANCE-MAIL pass=" @ $TNBMailTest::Pass @ " fail=" @ $TNBMailTest::Fail); if ($TNBMailTest::Fail > 0) echo($TNBMailTest::Failures);' 2>&1 \
    | grep -E "CONFORMANCE-MAIL|\(got |\(missing "
