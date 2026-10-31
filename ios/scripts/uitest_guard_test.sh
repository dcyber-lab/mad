#!/bin/bash
# Tests the simulator-name guard of uitest.sh (SIM_NAME): it must refuse any name that does not
# start with "mad UITest", the person's own simulators among them, and say why, before touching
# any simulator. Run: bash ios/scripts/uitest_guard_test.sh
cd "$(dirname "$0")/../.." || exit 1
failed=0

# check <name> <accept|refuse> [text the message must hold]
check() {
    out=$(SIM_NAME="$1" UITEST_GUARD_ONLY=1 bash ios/scripts/uitest.sh 2>&1)
    rc=$?
    if [ "$2" = accept ]; then
        [ $rc -eq 0 ] || { echo "FAIL: \"$1\" should be accepted but was refused: $out"; failed=1; return; }
    else
        [ $rc -ne 0 ] || { echo "FAIL: \"$1\" should be refused but was accepted"; failed=1; return; }
        case "$out" in *"$3"*) ;; *) echo "FAIL: the refusal of \"$1\" does not say \"$3\": $out"; failed=1; return ;; esac
    fi
    echo "ok: \"$1\" $2"
}

check "mad UITest" accept
check "mad UITest bounds" accept
check "mad UITest 45be" accept
check "mad 体验" refuse "for using, not for tests"
check "iPhone 17" refuse "for using, not for tests"
check "iPhone 17 Pro" refuse "does not start with"
check "mad uitest" refuse "does not start with"
check "my mad UITest" refuse "does not start with"
check "UITest" refuse "does not start with"
check " mad UITest" refuse "does not start with"
# Unset or empty means the default name, which is allowed.
out=$(env -u SIM_NAME UITEST_GUARD_ONLY=1 bash ios/scripts/uitest.sh 2>&1) || { echo "FAIL: the default name was refused: $out"; failed=1; }
out=$(SIM_NAME="" UITEST_GUARD_ONLY=1 bash ios/scripts/uitest.sh 2>&1) || { echo "FAIL: an empty SIM_NAME should mean the default: $out"; failed=1; }
echo "ok: the default name (unset and empty)"

[ $failed -eq 0 ] && echo "guard: all good" || { echo "guard: FAILED"; exit 1; }
