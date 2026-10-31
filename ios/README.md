# mad for iOS

A SwiftUI app for mad. It pairs with a Mac by the QR code `mad phone` shows
(or its pasted link), lists what waits on you as native cards you can act on,
and shows the full deck page in a second tab. The README at the top of the
repository tells how to build and install it; this file is for working on it.

- `MadKit/` is a Swift package with the core, tested with `swift test` on the
  Mac (no simulator needed): pairing-link parsing (`Pairing`), the pairing
  exchange (`PendingPairing`), `PairingBook`, the rules for the paired Macs,
  the Mac's name from `/api/deck` (`MadClient`, `DeckSnapshot`), what the page
  may say to the app (`DeckMessage`, `WebOrigin`) and the provisioning
  profile's expiry (`ProvisioningProfile`).
- `App/` is the SwiftUI app: the Macs list (`MacsView`), an opened Mac, which is
  the deck page in a full-screen web view (`MacView`, `MacModel` with
  `DeckWebHost`), the camera (`ScannerScreen`) and storage (`MacStore`).
- `project.yml` is the XcodeGen spec. The `.xcodeproj` is generated and
  gitignored.

## Build

    make ios          # swift test, xcodegen generate, simulator build

It needs Xcode with the iOS platform installed (Xcode > Settings > Components)
and `brew install xcodegen`. To open it: `cd ios && xcodegen generate && open
mad.xcodeproj`.

The simulator build is ad hoc signed (`CODE_SIGN_IDENTITY=-`), not unsigned: an
unsigned app has no Keychain access, so it would forget every pairing.

To put it on a phone with a free Apple ID, run `make ios-device` (the
README at the top of the repository has the steps). `ios/scripts/device.sh` is
what it runs: it finds your Personal Team (from Xcode's accounts, the profiles
Xcode made, or your signing certificates) and writes `Config/Local.xcconfig`,
picks the one connected iPhone (`DEVICE=<id>` otherwise), builds Release with
`-allowProvisioningUpdates`, installs with `devicectl` and launches. With a team
you already know, write `Config/Local.xcconfig` yourself (see
`Config/Local.xcconfig.example`): `DEVELOPMENT_TEAM` and `MAD_BUNDLE_ID`.

The project has no entitlements file, on purpose: a free account has no Push
Notifications (`aps-environment`), and asking for it fails signing.

A free account's provisioning profile lasts 7 days. The app reads the
`embedded.mobileprovision` it carries (`ProvisioningProfile` in MadKit) and,
with 2 days or fewer left, shows a note on the Macs list. The profile is not in
the simulator, so nothing shows there; Debug builds take `-profileHoursLeft N`
to see the note.

## How pairing works

`mad phone` shows `https://<host>[:port]/#pair=<32 hex>`: a code that works
once, within 10 minutes. The app:

1. makes a token of its own (16 random bytes, 32 lowercase hex digits),
2. saves the token to the Keychain as a pending pairing before sending
   anything (the code and the Mac's address go to UserDefaults: the code
   is single-use, and a replay also needs the token),
3. sends `POST /api/pair {"code","name","token"}` (the name is the iPhone's),
4. on `200 {"id","machine"}` stores the Mac under the Mac's own name and drops
   the pending entry.

If the answer is lost, the pending entry stays and the Macs list offers Retry,
which sends the same code and token again; the Mac answers with the same id.
A refusal (400 or 403) drops the entry: scan a new QR code. A `#token=` link
(a token already made) is still accepted. The app's own requests carry
`Authorization: Bearer <token>`, never the token in the URL. The embedded deck
page is different: it gets `#token=<t>&embed=app` in the fragment and, like the
page in a browser, sends the token as `?token=` on its own requests and
websockets, so those URLs carry it (over HTTPS, but a proxy that logs URLs
would see it).

`PairingBook` holds these rules and is tested with a fake Keychain and a fake
network: the token is saved before it is sent, a failed save never loses the old
token, a late answer for a deleted pairing installs nothing, and a re-pair keeps
a name only when the person renamed the Mac.

## The page and the app

The app has no screens of its own after pairing: the page is the app, and the
only thing it asks of the app is to go back to the Macs list. It posts
`window.webkit.messageHandlers.mad.postMessage({type: "switchMac"})`. The app
reads a message only from the main frame of the paired Mac's own origin
(scheme, host and port), ignores any other type, and ignores anything else.
Links to other origins, and `window.open`, open in the system browser. The web
view keeps no data (a non-persistent store), so the page's own copy of the token
goes with it; the Keychain holds the only lasting one. The page is loaded with
`#token=<t>&embed=app` in the fragment, and with the app's insets off: it keeps
clear of the status bar and the home indicator itself.

## UI tests

`make ios-uitest` taps through the special cases of the app and its page in a simulator
of its own, "mad UITest" (an iPhone 17, made on first use and kept; never your other
simulators), against the fixture server (`internal/desktop/fixture`) and its canned
scenarios. It runs one scenario at a time, one simulator and one `xcodebuild` at a time:

| Test class | Fixture scenario | What it looks at |
| --- | --- | --- |
| `EdgeHomeTests` | `edge-home` | the 60-character Mac name and its ▾, the red dots, the order of the chats, tiles of 1, 2 and 5 members, the characters' states, the untitled agent's label, the long CJK line, search, the ⋯ menu, dark mode, landscape |
| `EmptyTests` | `empty` | the empty state |
| `ManyChannelsTests` | `many-channels` | rows with a red dot first, then newest |
| `ChannelFoldTests` | `channel-fold` | folded handoff and report (Show more and Show less), the @you and own messages whole, the pictures and their lightbox, the long URL, the member strip |
| `UsageTests` | `usage` | 42% and 71%, 104% without a negative "left", no reset line for the zero time |
| `AppTests` | `edge-home` | the expiry banner and its ✕, ▾ 切换 Mac back to the list and the Mac opening again, the last Mac opening on launch |
| `PairingTests`, `PairingRetryTests` | `pairing` | a good code, a refused one ("scan a new QR"), a 503 and then Retry |
| `PairingOfflineTests` | `pairing` | the server down (the pairing is kept, with Retry), then back, and Retry pairs |

The tests find things in the page by accessibility label and attach a screenshot to each
case. When it ends, `ios/build/uitest-report/index.html` is a gallery of every screenshot,
grouped by case, with a pass or fail and the failure message. `ONLY="UsageTests AppTests"`
runs some classes only; `FIXTURE_ROOT=<dir>` takes the fixture server from another checkout.
The tests are in `UITests/`; the harness is `scripts/uitest.sh` and `scripts/uitest_report.py`.
The app takes `-resetStore YES` (Debug) so each case starts with no Mac paired.

The app now turns to landscape too (the page lays out for it), which the landscape case looks at.

## Debug builds

Debug builds only (none of this is compiled into Release):

- Pairing also accepts `http://127.0.0.1:<port>/#pair=…` and
  `http://localhost…`, to run against a local `mad desktop -serve` (the
  Info.plist allows local networking; Release takes https only).
- Launch arguments, e.g. `xcrun simctl launch <device> dev.mad.ios -openMac YES`:
  - `-pairLink <url>` pairs when no Mac is stored yet,
  - `-openMac YES` opens the first Mac (once, when the app starts),
  - `-resetStore YES` forgets every Mac and pending pairing first (the UI tests start with it),
  - `-profileHoursLeft N` shows the note for a profile with N hours left,
  - `-debugSwitchMac YES` has the page post `switchMac` to the app six seconds
    after a Mac opens, to see the way back to the list work.

Never point a test at the real deck's server and tap an act: allow, deny and
reply answer real agents. Use an isolated server (a temporary
`XDG_STATE_HOME`, its own port).
