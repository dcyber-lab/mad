# mad for iOS

A SwiftUI shell for mad: pair with a Mac by scanning the QR code `mad phone`
shows (or pasting its link), check the connection, and open the deck page.

- `MadKit/` — Swift package with the core: pairing-link parsing, the `/api/deck`
  client, snapshot decoding. `cd ios/MadKit && swift test`.
- `App/` — the SwiftUI app. Tokens live in the Keychain, the rest in UserDefaults.
- `project.yml` — XcodeGen spec; the `.xcodeproj` is generated and gitignored.

## Build

    make ios          # swift test, xcodegen generate, unsigned simulator build

Needs Xcode with the iOS platform installed (Xcode > Settings > Components) and
`brew install xcodegen`. To open it: `cd ios && xcodegen generate && open mad.xcodeproj`.

To run on a device, copy `ios/Config/Local.xcconfig.example` to
`ios/Config/Local.xcconfig` (gitignored) and set your `DEVELOPMENT_TEAM` and
`MAD_BUNDLE_ID`.

The token travels as `?token=` for now; `MadClient.request` is the one place to
switch to `Authorization: Bearer`.
