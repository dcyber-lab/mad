# mad for iOS

A SwiftUI app for mad. It pairs with a Mac by the QR code `mad phone` shows
(or its pasted link), lists what waits on you as native cards you can act on,
and shows the full deck page in a second tab. The README at the top of the
repository tells how to build and install it; this file is for working on it.

- `MadKit/` is a Swift package with the core, tested with `swift test` on the
  Mac (no simulator needed): pairing-link parsing (`Pairing`), the `/api/deck`,
  `/api/inbox` and `/api/pair` client (`MadClient`, `PendingPairing`), snapshot
  and inbox decoding (`DeckSnapshot`), and `PairingBook`, the rules for the
  paired Macs.
- `App/` is the SwiftUI app: the Macs list (`MacsView`), an opened Mac with
  its Needs you and Deck tabs (`MacView`, `NeedsView`, `MacModel`), the camera
  (`ScannerScreen`) and storage (`MacStore`).
- `project.yml` is the XcodeGen spec. The `.xcodeproj` is generated and
  gitignored.

## Build

    make ios          # swift test, xcodegen generate, simulator build

It needs Xcode with the iOS platform installed (Xcode > Settings > Components)
and `brew install xcodegen`. To open it: `cd ios && xcodegen generate && open
mad.xcodeproj`.

The simulator build is ad hoc signed (`CODE_SIGN_IDENTITY=-`), not unsigned: an
unsigned app has no Keychain access, so it would forget every pairing.

To run on a device, copy `Config/Local.xcconfig.example` to
`Config/Local.xcconfig` (gitignored) and set `DEVELOPMENT_TEAM` and
`MAD_BUNDLE_ID`.

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

## Debug builds

Debug builds only (none of this is compiled into Release):

- Pairing also accepts `http://127.0.0.1:<port>/#pair=…` and
  `http://localhost…`, to run against a local `mad desktop -serve` (the
  Info.plist allows local networking; Release takes https only).
- Launch arguments, e.g. `xcrun simctl launch <device> dev.mad.ios -openMac YES`:
  - `-pairLink <url>` pairs when no Mac is stored yet,
  - `-openMac YES` opens the first Mac,
  - `-startTab deck` starts on the Deck tab,
  - `-fixture inbox` shows a sample Mac, "Studio", with one inbox item of each
    kind, without a server; acts send nothing and say so. Use it with
    `-openMac YES`. `-fixtureSkip N` leaves out the first N items, to see the
    ones below the fold.

Never point a test at the real deck's server and tap an act: allow, deny and
reply answer real agents. Use `-fixture inbox`, or an isolated server (a
temporary `XDG_STATE_HOME`, its own port).
