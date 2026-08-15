#!/bin/bash
# Run the TNBrowser test suites inside a patched container, against the backend.
#
# There used to be two runners: this one drove the suites against a Python mock
# of the old PHP clan API, and run-conformance.sh drove the same suites against
# the Go server, so the two could be diffed. There is only one backend now, and
# a mock of a protocol nobody speaks was checking a shape nothing produces. One
# runner, one backend.
#
# The four suites:
#
#   json     the JSON parser, entirely in-engine -- no backend, no database
#   sweep    every ordinal, against real rows
#   browser  the Tribe & Warrior Browser panes
#   mail     the email pane
#
# The database is reseeded before each of the three that touch it. They write to
# it -- the sweep alone sends mail, deletes mail, invites a warrior and edits two
# profiles -- so sharing one dataset across suites makes an earlier suite's
# writes surface as a later suite's failures.
#
# Usage: ./tools/run-tests.sh [game-port]
set -uo pipefail

PORT="${1:-2325}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
T2="${CLAUDE_PLUGIN_ROOT:-$HOME/.claude/plugins/cache/tribes-2-modding-skill/tribes2-modding/1.1.0}"
CONSOLE="python3 $T2/scripts/t2console.py --port $PORT"

DATA="http://172.17.0.1:8080"       # the backend, as seen from the container
DATA_LOCAL="http://127.0.0.1:8080"  # and as seen from here

reseed() {
    docker exec -i tnb-postgres psql -q -U tnbrowser -d tnbrowser -v ON_ERROR_STOP=1 \
        < "$ROOT/server/testdata/seed.sql" >/dev/null || exit 1
}

# Preflight: the backend has to be up, and it has to be running with the test
# bypass. The containers hold no account key material, so a client in one cannot
# answer a challenge; without the bypass every suite reports a wall of empty
# panes, which looks like a backend fault. That failure mode cost two wrong
# diagnoses before this check existed.
#
#   ./tnserver -dsn ... -dev-trust-guid
preflight() {
    local code
    code=$(curl -s -o /dev/null -w '%{http_code}' \
           --data 'guid=4510186&payload={"form":"scalar","ordinal":"0","args":""}' \
           "$DATA_LOCAL/db")
    case "$code" in
        200) return 0 ;;
        401) echo "The backend refused a bare guid." >&2
             echo "Start it with -dev-trust-guid (or TNB_DEV_TRUST_GUID=1)." >&2
             exit 1 ;;
        000) echo "No backend answering on $DATA_LOCAL." >&2
             exit 1 ;;
        *)   echo "The backend answered /db with $code." >&2
             exit 1 ;;
    esac
}
preflight

echo "== deploying =="
"$ROOT/tools/deploy.sh" "$PORT" >/dev/null || exit 1

# The shim is loaded by the mod's own autoexec at boot; only the tests need
# exec'ing. GuidOverride stands in for a TribesNext login the container does
# not have -- the seed data is keyed on that guid.
LOAD='$TNB::GuidOverride = "4510186";'

echo
echo "== json parser =="
$CONSOLE "$LOAD" 'exec("tests/json_test.cs"); TNBJsonSelfTest();' 2>&1 \
    | grep -E "^FAIL|TNBJSONRESULT"

echo
echo "== ordinal sweep =="
reseed
$CONSOLE "$LOAD" 'exec("tests/sweep_test.cs");' \
    "TNBSweepSelfTest(\"$DATA\");" \
    --until '$TNBSweep::Done' --until-timeout 180 >/dev/null 2>&1

# The steps run from schedule() callbacks, so their console output lands
# between a runner's polls instead of on our connection. Read the tally back.
$CONSOLE 'echo("TNBSWEEPRESULT pass=" @ $TNBSweep::Pass @ " fail=" @ $TNBSweep::Fail); if ($TNBSweep::Fail > 0) echo($TNBSweep::Failures);' 2>&1 \
    | grep -E "TNBSWEEPRESULT|\(got "

echo
echo "== browser =="
reseed
$CONSOLE "$LOAD" 'exec("tests/browser_test.cs");' \
    "TNBBrowserSelfTest(\"$DATA\");" \
    --until '$TNBBrowserTest::Done' --until-timeout 150 >/dev/null 2>&1

$CONSOLE 'echo("TNBBROWSERRESULT pass=" @ $TNBBrowserTest::Pass @ " fail=" @ $TNBBrowserTest::Fail); if ($TNBBrowserTest::Fail > 0) echo($TNBBrowserTest::Failures);' 2>&1 \
    | grep -E "TNBBROWSERRESULT|\(got |\(missing "

echo
echo "== mail =="
reseed
$CONSOLE "$LOAD" 'exec("tests/mail_test.cs");' \
    "TNBMailSelfTest(\"$DATA\");" \
    --until '$TNBMailTest::Done' --until-timeout 150 >/dev/null 2>&1

$CONSOLE 'echo("TNBMAILRESULT pass=" @ $TNBMailTest::Pass @ " fail=" @ $TNBMailTest::Fail); if ($TNBMailTest::Fail > 0) echo($TNBMailTest::Failures);' 2>&1 \
    | grep -E "TNBMAILRESULT|\(got |\(missing "
