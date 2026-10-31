#!/bin/bash
# Runs the UI tests in a dedicated simulator, one scenario at a time: for each it starts
# the fixture server (internal/desktop/fixture) on a free port, runs that scenario's
# XCUITest classes against it, and stops the server. At the end every screenshot is
# gathered into ios/build/uitest-report/index.html. Run it with `make ios-uitest`.
#
#   ONLY="UsageTests AppTests"   run only these test classes
#   FIXTURE_ROOT=<dir>           a checkout that has internal/desktop/fixture (default: this one)
#   SIM_NAME=<name>              the simulator to use or make (default: "mad UITest")
#
# One simulator, one xcodebuild, one server at a time: nothing runs in parallel.
set -u
cd "$(dirname "$0")/../.." || exit 1
ROOT=$PWD
FIXTURE_ROOT=${FIXTURE_ROOT:-$ROOT}
RESULTS=ios/build/uitest-results
REPORT=ios/build/uitest-report
# The dedicated simulator. SIM_NAME=<name> uses (and makes) another, so two runs never share one.
SIM_NAME="${SIM_NAME:-mad UITest}"
SERVER_BIN=ios/build/fixture-server

die() { printf 'make ios-uitest: %s\n' "$1" >&2; exit 1; }

command -v xcodegen >/dev/null || die "xcodegen is missing: brew install xcodegen"
[ -d "$FIXTURE_ROOT/internal/desktop/fixture" ] || die "no internal/desktop/fixture in $FIXTURE_ROOT: the fixture server has not landed (set FIXTURE_ROOT to a checkout that has it)"

# ---- the simulator ------------------------------------------------------------
# Its own device, never the person's: created once, reused after.
UDID=$(python3 -I - "$SIM_NAME" <<'PY'
import json, subprocess, sys
want = sys.argv[1]
d = json.loads(subprocess.run(["xcrun", "simctl", "list", "devices", "-j"], capture_output=True, text=True).stdout)["devices"]
for runtime, devs in d.items():
    for x in devs:
        if x["name"] == want and x.get("isAvailable", True):
            print(x["udid"]); sys.exit(0)
PY
)
if [ -z "$UDID" ]; then
    RUNTIME=$(xcrun simctl list runtimes -j | python3 -I -c '
import json, sys
rs = [r for r in json.load(sys.stdin)["runtimes"] if r.get("isAvailable") and r["identifier"].startswith("com.apple.CoreSimulator.SimRuntime.iOS-")]
rs.sort(key=lambda r: [int(p) for p in r["version"].split(".")], reverse=True)
want = [r for r in rs if r["version"].startswith("26.5")] or rs
print(want[0]["identifier"] if want else "")')
    [ -n "$RUNTIME" ] || die "no iOS simulator runtime is installed (Xcode > Settings > Components)"
    UDID=$(xcrun simctl create "$SIM_NAME" com.apple.CoreSimulator.SimDeviceType.iPhone-17 "$RUNTIME") || die "could not create the simulator"
    echo "Made the simulator "${SIM_NAME}" ($RUNTIME)."
fi
xcrun simctl boot "$UDID" 2>/dev/null
xcrun simctl bootstatus "$UDID" -b >/dev/null 2>&1
xcrun simctl ui "$UDID" appearance light >/dev/null 2>&1

# ---- build once ---------------------------------------------------------------
mkdir -p "$RESULTS" ios/build
# A full run starts clean; with ONLY, the runs of the classes not asked for are kept, so the report still shows them.
if [ -z "${ONLY:-}" ]; then rm -rf "$RESULTS"/*.xcresult "$RESULTS"/*.log; fi
rm -rf "$REPORT"
(cd ios && xcodegen generate -q) || die "xcodegen failed"
(cd "$FIXTURE_ROOT" && go build -o "$ROOT/$SERVER_BIN" ./internal/desktop/fixture) || die "the fixture server did not build"
echo "Building the app and its tests..."
xcodebuild build-for-testing -project ios/mad.xcodeproj -scheme madUITests -destination "id=$UDID" \
    -derivedDataPath ios/build/uitest CODE_SIGN_IDENTITY=- > "$RESULTS/build.log" 2>&1 || { tail -n 20 "$RESULTS/build.log" >&2; die "the build failed (see $RESULTS/build.log)"; }

# ---- servers and runs ---------------------------------------------------------
SERVER_PID=""
free_port() { python3 -I -c "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1])"; }
stop_server() { [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null && wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""; }
trap 'stop_server' EXIT

# start_server <scenario> <port>: serves the scenario (port 0 picks one) and waits for its
# "ready <url>" line, which sets URL.
URL=""
start_server() {
    (cd "$FIXTURE_ROOT" && exec "$ROOT/$SERVER_BIN" -addr "127.0.0.1:$2" -scenario "$1") > "$RESULTS/server-$1.log" 2>&1 &
    SERVER_PID=$!
    for _ in $(seq 1 120); do
        if grep -q '^ready ' "$RESULTS/server-$1.log" 2>/dev/null; then
            URL=$(sed -n 's/^ready //p' "$RESULTS/server-$1.log" | head -1)
            return 0
        fi
        kill -0 "$SERVER_PID" 2>/dev/null || break
        sleep 0.5
    done
    echo "the fixture server for "${1}" did not say ready:" >&2; tail -n 5 "$RESULTS/server-$1.log" >&2
    stop_server; return 1
}

FAILED=0
RUNS=0
# run <name> <url> <only-testing>: one xcodebuild test run; its result bundle is $RESULTS/<name>.xcresult.
run() {
    printf '  %-34s' "$1"
    RUNS=$((RUNS + 1))
    rm -rf "$RESULTS/$1.xcresult"
    TEST_RUNNER_MAD_FIXTURE_URL="$2" xcodebuild test-without-building -project ios/mad.xcodeproj -scheme madUITests \
        -destination "id=$UDID" -derivedDataPath ios/build/uitest -resultBundlePath "$RESULTS/$1.xcresult" \
        -only-testing:"madUITests/$3" -parallel-testing-enabled NO \
        -test-timeouts-enabled YES -default-test-execution-time-allowance 120 -maximum-test-execution-time-allowance 300 \
        CODE_SIGN_IDENTITY=- > "$RESULTS/$1.log" 2>&1 &
    local pid=$!
    # A run that hangs is stopped after RUN_LIMIT seconds (default 900) and counts as failed.
    ( sleep "${RUN_LIMIT:-900}"; kill "$pid" 2>/dev/null ) &
    local watch=$!
    wait "$pid"; local rc=$?
    kill "$watch" 2>/dev/null; wait "$watch" 2>/dev/null
    if [ $rc -eq 0 ]; then echo "passed"; else echo "FAILED"; FAILED=$((FAILED + 1)); fi
}

wanted() {
    [ -z "${ONLY:-}" ] && return 0
    case " $ONLY " in *" $1 "*) return 0 ;; esac
    return 1
}

# a test class and the scenario its server serves
for pair in "EdgeHomeTests edge-home" "EmptyTests empty" "ManyChannelsTests many-channels" \
            "ChannelFoldTests channel-fold" "UsageTests usage" "AppTests edge-home" "PairingTests pairing" "PairingRetryTests pairing"; do
    set -- $pair
    wanted "$1" || continue
    start_server "$2" 0 || { FAILED=$((FAILED + 1)); continue; }
    run "$1" "$URL" "$1"
    stop_server
done

# The lost-answer case needs the server's state to change between two runs: down for the
# first (nothing listens on the port), up for the second.
if wanted PairingOfflineTests; then
    PORT=$(free_port)
    run "PairingOfflineTests-1-server-down" "http://127.0.0.1:$PORT" "PairingOfflineTests/testA_ServerDownKeepsThePairingWithRetry"
    if start_server pairing "$PORT"; then
        run "PairingOfflineTests-2-server-back" "$URL" "PairingOfflineTests/testB_RetryAfterTheServerIsBackPairs"
        stop_server
    else
        FAILED=$((FAILED + 1))
    fi
fi

# ---- the report ---------------------------------------------------------------
python3 -I ios/scripts/uitest_report.py "$RESULTS" "$REPORT" || echo "make ios-uitest: the report could not be made" >&2
echo
[ "$RUNS" -gt 0 ] || { echo "make ios-uitest: no test run happened (check ONLY=${ONLY:-})" >&2; FAILED=$((FAILED + 1)); }
echo "Report: $ROOT/$REPORT/index.html"
[ "$FAILED" -eq 0 ] && echo "All runs passed." || echo "$FAILED run(s) failed."
xcrun simctl shutdown "$UDID" 2>/dev/null
[ "$FAILED" -eq 0 ]
