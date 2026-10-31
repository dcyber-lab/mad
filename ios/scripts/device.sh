#!/bin/bash
# Puts mad on your iPhone with a free Apple ID (a Personal Team): finds your
# team and the phone, builds the Release app signed for it, installs and
# launches it. Run it with `make ios-device`; DEVICE=<id|name> picks one of
# several phones and TEAM=<id> a team. A free profile lasts 7 days: run it
# again to renew.
set -u
cd "$(dirname "$0")/../.." || exit 1

LOCAL=ios/Config/Local.xcconfig
LOG=ios/build/device.log
OUT=ios/build/device

die() {
    printf 'make ios-device: %s\n' "$1" >&2
    shift
    for line in "$@"; do printf '  %s\n' "$line" >&2; done
    exit 1
}

command -v xcodegen >/dev/null || die "xcodegen is missing." "brew install xcodegen"
command -v python3 >/dev/null || die "python3 is missing." "xcode-select --install"
xcrun --find devicectl >/dev/null 2>&1 || die "Xcode's devicectl is missing: Xcode 15 or later is needed." "Install Xcode from the App Store and run: sudo xcode-select -s /Applications/Xcode.app"

# ---- the team -------------------------------------------------------------

# teams prints the teams this Mac knows, one per line: id, name, free (1/0). It
# reads what Xcode keeps of the signed-in Apple IDs, then the profiles Xcode
# made, then the signing certificates.
teams() {
    python3 -I - <<'PY'
import glob, os, plistlib, re, subprocess, sys, time

found = {}  # id -> (name, free)

def add(i, name, free):
    if not re.fullmatch(r"[A-Z0-9]{10}", i or ""):
        return
    old = found.get(i)
    found[i] = ((name or (old[0] if old else "")), bool(free) or bool(old and old[1]))

def walk(o):
    if isinstance(o, dict):
        if "teamID" in o:
            add(o.get("teamID"), o.get("teamName"), o.get("isFreeProvisioningTeam"))
        for v in o.values():
            walk(v)
    elif isinstance(o, list):
        for v in o:
            walk(v)

try:
    out = subprocess.run(["defaults", "export", "com.apple.dt.Xcode", "-"], capture_output=True, timeout=20).stdout
    walk(plistlib.loads(out))
except Exception:
    pass

dirs = [os.path.expanduser("~/Library/MobileDevice/Provisioning Profiles"),
        os.path.expanduser("~/Library/Developer/Xcode/UserData/Provisioning Profiles")]
for d in dirs:
    for f in glob.glob(os.path.join(d, "*.mobileprovision")):
        try:
            raw = subprocess.run(["security", "cms", "-D", "-i", f], capture_output=True, timeout=20).stdout
            p = plistlib.loads(raw)
            if p["ExpirationDate"].timestamp() < time.time():
                continue
            free = (p["ExpirationDate"] - p["CreationDate"]).days <= 8  # a free profile lasts 7 days
            add(p["TeamIdentifier"][0], p.get("TeamName"), free)
        except Exception:
            pass

try:
    pems = subprocess.run(["security", "find-certificate", "-a", "-c", "Apple Development", "-p"],
                          capture_output=True, text=True, timeout=20).stdout
    for pem in re.findall(r"-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----", pems, re.S):
        s = subprocess.run(["openssl", "x509", "-noout", "-subject", "-nameopt", "RFC2253"],
                           input=pem, capture_output=True, text=True, timeout=20).stdout
        m = re.search(r"OU=([A-Z0-9]{10})", s)
        if m:
            add(m.group(1), "", False)
except Exception:
    pass

for i, (name, free) in sorted(found.items()):
    print(f"{i}\t{name}\t{1 if free else 0}")
PY
}

team_from_local() {
    [ -f "$LOCAL" ] || return 0
    sed -n 's/^[[:space:]]*DEVELOPMENT_TEAM[[:space:]]*=[[:space:]]*\([A-Z0-9]\{10\}\)[[:space:]]*$/\1/p' "$LOCAL" | tail -1
}

TEAM_ID="${TEAM:-}"
if [ -z "$TEAM_ID" ]; then
    TEAM_ID=$(team_from_local)
    [ "$TEAM_ID" = "ABCDE12345" ] && TEAM_ID="" # the example file's placeholder
fi
if [ -z "$TEAM_ID" ]; then
    LIST=$(teams)
    N=$(printf '%s' "$LIST" | grep -c .)
    FREE=$(printf '%s\n' "$LIST" | awk -F'\t' '$3 == 1' | grep -c .)
    if [ "$N" -eq 1 ]; then
        TEAM_ID=$(printf '%s' "$LIST" | cut -f1)
    elif [ "$FREE" -eq 1 ]; then
        TEAM_ID=$(printf '%s\n' "$LIST" | awk -F'\t' '$3 == 1 {print $1}')
    elif [ "$N" -gt 1 ]; then
        echo "make ios-device: this Mac knows more than one team:" >&2
        printf '%s\n' "$LIST" | awk -F'\t' '{printf "  %s  %s\n", $1, $2}' >&2
        die "Pick one." "make ios-device TEAM=<id>   (or set DEVELOPMENT_TEAM in ios/Config/Local.xcconfig)"
    else
        die "No team found: no Apple ID is signed in to Xcode yet." \
            "1. Open Xcode > Settings > Accounts, press +, and sign in with your Apple ID." \
            "2. Run make ios-device again. It finds your Personal Team and writes ios/Config/Local.xcconfig." \
            "If it still finds none, read your Team ID at developer.apple.com/account (Membership details)" \
            "and run: make ios-device TEAM=<10-character id>"
    fi
fi

# Local.xcconfig: the team, and a bundle id of your own (a free team needs one
# no one else has, so it is made from the team id).
BUNDLE=""
if [ -f "$LOCAL" ]; then
    BUNDLE=$(sed -n 's/^[[:space:]]*MAD_BUNDLE_ID[[:space:]]*=[[:space:]]*\([^[:space:]]*\).*$/\1/p' "$LOCAL" | tail -1)
    [ "$BUNDLE" = "com.example.mad" ] && BUNDLE="" # the example file's placeholder
fi
[ -n "$BUNDLE" ] || BUNDLE="com.$(printf '%s' "$TEAM_ID" | tr '[:upper:]' '[:lower:]').mad"
if [ ! -f "$LOCAL" ]; then
    printf '// Written by make ios-device. Your team and bundle id; not committed.\nDEVELOPMENT_TEAM = %s\nMAD_BUNDLE_ID = %s\n' "$TEAM_ID" "$BUNDLE" > "$LOCAL"
    echo "Wrote $LOCAL: team $TEAM_ID, bundle id $BUNDLE"
fi

# ---- the phone ------------------------------------------------------------

DEVJSON=$(mktemp -t madios)
trap 'rm -f "$DEVJSON"' EXIT
xcrun devicectl list devices --json-output "$DEVJSON" >/dev/null 2>&1 || die "Could not list devices." "Open Xcode once and accept its license, then try again."

# One line per iPhone that can be reached: id, name, developer mode, UDID.
PHONES=$(WANT="${DEVICE:-}" python3 -I - "$DEVJSON" <<'PY'
import json, os, sys
want = os.environ.get("WANT", "")
try:
    devices = json.load(open(sys.argv[1]))["result"]["devices"]
except Exception:
    devices = []
for d in devices:
    hw, cp, dp = d.get("hardwareProperties", {}), d.get("connectionProperties", {}), d.get("deviceProperties", {})
    if hw.get("deviceType") != "iPhone":
        continue
    ident, udid, name = d.get("identifier", ""), hw.get("udid", ""), dp.get("name", "")
    if want:
        if want not in (ident, udid, name):
            continue
    elif cp.get("pairingState") != "paired" or cp.get("tunnelState") == "unavailable":
        continue
    print(f"{ident}\t{name}\t{dp.get('developerModeStatus', '')}\t{udid}")
PY
)
COUNT=$(printf '%s' "$PHONES" | grep -c .)
if [ "$COUNT" -eq 0 ]; then
    if [ -n "${DEVICE:-}" ]; then
        die "No iPhone matches DEVICE=$DEVICE." "xcrun devicectl list devices"
    fi
    die "No iPhone is connected." \
        "1. Connect it to this Mac with a cable and unlock it. Tap Trust if it asks." \
        "2. In Xcode, Window > Devices and Simulators, the phone should show as connected." \
        "3. Run make ios-device again."
elif [ "$COUNT" -gt 1 ]; then
    echo "make ios-device: more than one iPhone is available:" >&2
    printf '%s\n' "$PHONES" | awk -F'\t' '{printf "  %s  %s\n", $1, $2}' >&2
    die "Pick one." "make ios-device DEVICE=<id>"
fi
PHONE_ID=$(printf '%s' "$PHONES" | cut -f1)
PHONE_NAME=$(printf '%s' "$PHONES" | cut -f2)
PHONE_UDID=$(printf '%s' "$PHONES" | cut -f4)
if [ "$(printf '%s' "$PHONES" | cut -f3)" = "disabled" ]; then
    die "Developer Mode is off on $PHONE_NAME." \
        "On the iPhone: Settings > Privacy & Security > Developer Mode, turn it on, and let it restart." \
        "Then run make ios-device again."
fi
echo "iPhone: $PHONE_NAME   team: $TEAM_ID   bundle id: $BUNDLE"

# ---- build, install, launch -----------------------------------------------

(cd ios && xcodegen generate -q) || die "xcodegen failed."
mkdir -p ios/build
echo "Building for $PHONE_NAME (the first build makes the provisioning profile and registers the phone with your team)..."
if ! xcodebuild -project ios/mad.xcodeproj -scheme mad -configuration Release \
        -destination "id=${PHONE_UDID:-$PHONE_ID}" -derivedDataPath "$OUT" \
        -allowProvisioningUpdates -allowProvisioningDeviceRegistration \
        DEVELOPMENT_TEAM="$TEAM_ID" MAD_BUNDLE_ID="$BUNDLE" build > "$LOG" 2>&1; then
    echo "make ios-device: the build failed. The log is $LOG." >&2
    if grep -qiE "No Accounts?[: ]|No Account for Team|Add (a new|your) account|not signed in" "$LOG"; then
        die "Xcode has no Apple ID for team $TEAM_ID." \
            "Open Xcode > Settings > Accounts, sign in with your Apple ID, then run make ios-device again."
    elif grep -qiE "requires a development team|Development team .* not found|No team found" "$LOG"; then
        die "The team $TEAM_ID is not one of your signed-in Apple IDs'." \
            "Check DEVELOPMENT_TEAM in $LOCAL, or remove the file and run make ios-device again."
    elif grep -qiE "no devices|has no devices|Communication with Apple failed" "$LOG"; then
        die "Xcode could not register the iPhone with your team." \
            "Keep the iPhone connected and unlocked, open Xcode > Window > Devices and Simulators," \
            "wait for it to show as connected, then run make ios-device again."
    elif grep -qiE "not available|cannot be registered|already (in use|exists)|bundle identifier.*(unavailable|taken)" "$LOG"; then
        die "The bundle id $BUNDLE is taken." \
            "Set another in $LOCAL, for example MAD_BUNDLE_ID = com.yourname.mad, and run again."
    elif grep -qiE "maximum number of|too many (apps|app ids|registered)|10 App IDs" "$LOG"; then
        die "Your free Apple ID is at one of its limits (3 apps per phone, 10 App IDs a week)." \
            "Delete an app you put there from Xcode, or wait a week, then run again."
    fi
    tail -n 15 "$LOG" >&2
    die "See $LOG for the whole build."
fi
APP="$OUT/Build/Products/Release-iphoneos/mad.app"
[ -d "$APP" ] || die "The build finished but $APP is not there."
APPID=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$APP/Info.plist" 2>/dev/null || echo "$BUNDLE")

echo "Installing on $PHONE_NAME..."
if ! OUTPUT=$(xcrun devicectl device install app --device "$PHONE_ID" "$APP" 2>&1); then
    printf '%s\n' "$OUTPUT" >&2
    if printf '%s' "$OUTPUT" | grep -qi "developer mode"; then
        die "Developer Mode is off on $PHONE_NAME." "Settings > Privacy & Security > Developer Mode, turn it on, restart, then run again."
    fi
    die "The install failed." "Keep the iPhone unlocked and connected, then run make ios-device again."
fi

echo "Launching..."
if ! OUTPUT=$(xcrun devicectl device process launch --device "$PHONE_ID" "$APPID" 2>&1); then
    printf '%s\n' "$OUTPUT" >&2
    if printf '%s' "$OUTPUT" | grep -qiE "not been explicitly trusted|invalid code signature|profile.*trust|untrusted"; then
        die "mad is installed, but the iPhone does not trust your developer certificate yet." \
            "On the iPhone: Settings > General > VPN & Device Management, tap your Apple ID under Developer App, then Trust." \
            "Then open mad from the Home Screen."
    fi
    die "mad is installed, but it did not launch." "Open it from the Home Screen. If it says the developer is not trusted: Settings > General > VPN & Device Management."
fi
echo "mad is on $PHONE_NAME. A free Apple ID's build stops opening after 7 days: run make ios-device again to renew it."
