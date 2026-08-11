#!/bin/bash
# Run the client test suites against the Go backend instead of the mock.
#
# The suites were written against tools/mockserver.py, which was written against
# TribesNext's published PHP. Loading the same fixtures into the Go server and
# running them unchanged is the conformance check: a failure here is a real
# behavioural difference between the two backends.
#
# One host, where this used to need two. The session used to be negotiated with
# the mock (standing in for TribesNext's robot login) while the data came from
# the Go server, because the Go server verified sessions by asking TribesNext
# about them. It now verifies the account certificate itself, so there is
# nothing upstream to stand in for.
#
# What the containers cannot do is answer a challenge: they hold no account key
# material, and t2csri_rsa_decrypt does not exist in a client launched
# -nologin. So the backend is started with -dev-trust-guid, which accepts a bare
# GUID. That is a test-only switch and the reason the real exchange is covered
# by Go tests (server/internal/auth) rather than by these suites.
#
# Usage: ./tools/run-conformance.sh [game-port]
set -uo pipefail

PORT="${1:-2325}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
T2="${CLAUDE_PLUGIN_ROOT:-$HOME/.claude/plugins/cache/tribes-2-modding-skill/tribes2-modding/1.1.0}"
CONSOLE="python3 $T2/scripts/t2console.py --port $PORT"

DATA="http://172.17.0.1:8080"       # the Go backend, as seen from the container
DATA_LOCAL="http://127.0.0.1:8080"  # and as seen from here

reseed() {
    docker exec -i tnb-postgres psql -q -U tnbrowser -d tnbrowser -v ON_ERROR_STOP=1 \
        < "$ROOT/server/testdata/seed.sql" >/dev/null || exit 1
}

# Preflight: the backend has to be up, and it has to be running with the test
# bypass. Without the bypass every suite reports a wall of empty panes, which
# looks like a backend fault -- the failure mode that cost two wrong diagnoses
# when this check did not exist.
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

"$ROOT/tools/deploy.sh" "$PORT" >/dev/null || exit 1

# The shim is loaded by the mod's own autoexec at boot; only the tests need
# exec'ing. GuidOverride stands in for a TribesNext login the container does
# not have -- the seed data is keyed on that guid.
LOAD='$TNB::GuidOverride = "4510186";'

echo
echo "== ordinal sweep, against the Go backend =="
reseed
$CONSOLE "$LOAD" 'exec("tests/sweep_test.cs");' \
    "TNBSweepSelfTest(\"$DATA\");" \
    --until '$TNBSweep::Done' --until-timeout 180 >/dev/null 2>&1
$CONSOLE 'echo("CONFORMANCE-API pass=" @ $TNBSweep::Pass @ " fail=" @ $TNBSweep::Fail); if ($TNBSweep::Fail > 0) echo($TNBSweep::Failures);' 2>&1 \
    | grep -E "CONFORMANCE-API|\(got "

echo
echo "== browser, against the Go backend =="
reseed
$CONSOLE "$LOAD" 'exec("tests/browser_test.cs");' \
    "TNBBrowserSelfTest(\"$DATA\");" \
    --until '$TNBBrowserTest::Done' --until-timeout 150 >/dev/null 2>&1
$CONSOLE 'echo("CONFORMANCE-GUI pass=" @ $TNBBrowserTest::Pass @ " fail=" @ $TNBBrowserTest::Fail); if ($TNBBrowserTest::Fail > 0) echo($TNBBrowserTest::Failures);' 2>&1 \
    | grep -E "CONFORMANCE-GUI|\(got |\(missing "

echo
echo "== mail, against the Go backend =="
reseed
$CONSOLE "$LOAD" 'exec("tests/mail_test.cs");' \
    "TNBMailSelfTest(\"$DATA\");" \
    --until '$TNBMailTest::Done' --until-timeout 150 >/dev/null 2>&1
$CONSOLE 'echo("CONFORMANCE-MAIL pass=" @ $TNBMailTest::Pass @ " fail=" @ $TNBMailTest::Fail); if ($TNBMailTest::Fail > 0) echo($TNBMailTest::Failures);' 2>&1 \
    | grep -E "CONFORMANCE-MAIL|\(got |\(missing "
