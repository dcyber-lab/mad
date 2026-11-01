# mad for iOS

A SwiftUI app for mad. It pairs with a Mac by the QR code `mad phone` shows
(or its pasted link), lists what waits on you as native cards you can act on,
and shows the full deck page in a second tab. The README at the top of the
repository tells how to build and install it; this file is for working on it.
[PROGRESS.md](PROGRESS.md) records where the work stands: what shipped, what was
decided and what is left.

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

## Native apps (the foundation)

The iPhone app and the Mac app are meant to be native SwiftUI; the page stays as the fallback. They
share three local packages:

- `MadKit` (no UI; iOS 17+ and macOS 14+): the models of mad's JSON API, the client, the stores.
- `MadUI` (SwiftUI; the same): the views, which take plain view-model structs (`HomeModel`,
  `HomeSection`, `HomeRow`), never MadKit types.
- `MadShell` (depends on both): the glue. `HomeMapping.model(from: Deck, fallbackName:) -> HomeModel`
  maps a deck to what the home view shows (tested against the fixture decks), and
  `NativeRoot(api:title:onSwitchMac:)` owns a `DeckStore`, polls it while the scene is active, and shows
  `HomeSidebar` in a `NavigationSplitView` on the Mac, `HomeView` with the detail over it on the
  iPhone. Channel and agent details are placeholders for now; the other agents' views plug in there,
  mapping their store's messages to their own view-model structs the same way.

Both apps open `NativeRoot` by default. What has no native screen yet opens the web page: the ⋯ menu's
用量, 看板, 技能, 手机和已配对设备, 通知, 添加项目 and 主题 open it on that view (`#open=usage|board|skills|phone|notify|addproject|theme`
in the page's URL fragment; `WebTarget` in MadKit builds the URL), in a sheet on the iPhone and a window of its own on the Mac;
an agent's conversation has a header button that opens the page on that agent (`#agent=<id>`) for its terminal and changes.

"使用网页版" shows the whole old web deck instead: on the iPhone, long-press the Mac in the Macs list (the choice is
kept per Mac; the same menu says "使用原生版" to go back); on the Mac, the toggle in Settings. Debug builds also take
`-webDeck YES|NO`.

### Installing

- iPhone: `make ios-device` (free Apple ID, see above).
- Mac: `make mac` builds and runs it from `ios/build/`; `make mac-install` builds a Release `madmac.app`
  and puts it in `/Applications` (ad hoc signed; the first time, open it with right-click → Open).

### The channel view

`ChannelView(screen:actions:)` (MadUI) is the channel: a one-line head, grey bubbles for agents and blue
ones for the person, runs of one sender grouped, time lines after ten quiet minutes, and a one-line
composer (pictures from the photo library or the pasteboard, at most 6). Handoffs, results and long
messages fold into one bordered line that opens in place; the person's own and `@you` stay whole.
"群信息" slides in from the right (iPhone) or is the trailing inspector (Mac). Long-press a message for
回复/复制.

`ChannelMapping.screen(channel:messages:deck:paused:)` (MadShell) turns a channel's messages and the deck
into a `ChannelScreen`, with the web's folding rules; `ChannelDetail` owns the `ChannelStore`.

Debug launch arguments for looking at it against the fixture server (`-scenario channel-fold`):
`-nativeRoot YES` (iPhone: the native shell at once), `-nativeOpen <row id>`, `-nativePanel YES`; the Mac
app also takes `-snapshotTo <png>` (writes its window to a file and quits; the inspector does not show
in it).

`make ios` builds the iPhone app (simulator) and the Mac app. `make mac` builds the Mac app and opens
it (`ios/build/mac/Build/Products/Debug/madmac.app`, signed to run locally, ad hoc, not sandboxed).

### MadKit's contract

Models (`Deck.swift`, `Bus.swift`, `Usage.swift`, `Chat.swift`), all `Decodable`, tolerant of
unknown fields, missing ones defaulted, dates as `Date?` (Go's zero time is `nil`):

| Type | What it is |
| --- | --- |
| `Deck` | `GET /api/deck`: `machine`, `projects`, `teams`, `channels` (the JSON's `bus`), `inbox`, `usage` (the JSON's `quota`), `states`, `working`, `at`; lookups `agent(id:)`, `channel(id:)`, `word(for:)`, `tone(for:)`, `agents` |
| `Project`, `Agent`, `Team`, `StateCount` | a project with its agents; an agent has `id kind icon color name title status shown done cost role team chat headless mode since tool target reply last` and `label` (name, else title, else "kind id8"), `isNamed`, `state`, `inWindowMode` |
| `Channel` | a channel or DM: `id title topic dm yours members last lastText lastAt unread mention listed paused lead team` (`Channel.Member`, `Channel.ChannelTeam`) |
| `Look` | how a shown state reads: `state word tone glyph meaning` |
| `Inbox`, `InboxItem`, `InboxAct`, `InboxOpen` | what waits on the person; an item describes its own acts (`verb label option input primary`) |
| `Usage`, `UsageWindow`, `UsageWording` | a kind's subscription use, with "resets in 2h 10m, at 18:40" wording |
| `BusMessage`, `MessagesPage`, `BusReceipt`, `BusImage` | `GET /api/bus/messages`: `seq at from kind text mentions replyTo handoff images`; `kind` is text, handoff, done, declined; the page has `last paused receipts` |
| `PostResult`, `DMResult`, `PostImage` | what a post and a DM answer; a picture to send (`mime`, `data`) |
| `ChatEntry`, `ChatPage`, `ChatTranscript`, `LiveMessage`, `LiveState` | an agent's conversation (`/api/chat`, `/api/live`) |

The client is `MadClient(mac:session:)` (Bearer token in the header, never in the URL), and `MadAPI`
is the protocol the stores take, so a test can fake the Mac: `deck()`, `messages(channel:since:)`,
`post(channel:text:replyTo:images:)`, `dm(to:text:images:)`, `markRead(channel:seq:)`,
`imageData(id:)`, `act(key:verb:option:text:)`, and the `ChatAPI` calls `chat`, `live`, `send`,
`setMode`, `interrupt`. Errors are `ConnectionError` (`refused`, `offline`, `pairingRefused`,
`conflict`, `unexpected`). A picture is `GET /api/bus/image?id=`, which needs the token: use
`MadClient.imageRequest(id:)` or `imageData(id:)`, not the bare URL.

Stores (`Stores.swift`): `@MainActor @Observable`; each has `run()`, which polls until the task is
cancelled (start it with `.task { await store.run() }`, or `.task(id: scenePhase)` to stop it while the
app is not active), and `refresh()`, one look.

- `DeckStore(api:interval:)`: `deck: Deck?` (the last good look), `link: LinkState` (`connecting`, `ok`,
  `refused`, `offline`, `failed(String)`), `agent(id:)`, `channel(id:)`. About once a second.
- `ChannelStore(id:api:)`: `messages`, `last`, `paused`, `receipts`, `link`, `loaded`, `sending`;
  `post(_:images:replyTo:)` (one at a time, then it looks), `markRead()`, `message(_ seq:)`. It asks
  `since=last`, adds only numbers it does not have, and follows a full page of 200 at once.
- `AgentStore(id:api:)`: `transcript`, `live`, `loaded`, `problem`, `sending`, `notice`; `send(_:)`,
  `interrupt()`, `look()`, `dismissNotice()`. It wraps `ChatSession`.

### Finding the server (the Mac app)

`ServerModel` (in `MacApp/`) takes, in this order: a fixture server (below); the phone server on
127.0.0.1:7484 with the token in `<state dir>/phone_token` (`$XDG_STATE_HOME/mad` or
`~/.local/state/mad`) if it answers `/api/deck`; else it starts `mad desktop -no-window -addr
127.0.0.1:0 -exit-on-stdin-eof` (the `mad` set in Settings, else the first on PATH), reads the URL and
token it prints, and stops it when the app quits. The app holds the write end of the child's stdin, so
if the app is killed the pipe closes and the child exits by itself. These are `LocalServer` and `ServerLauncher` in MadKit.

### The fixture hook

Both apps take a server to talk to instead of the real one: `MAD_FIXTURE_URL=<url>` in the environment,
or the launch argument `-fixtureURL <url>` (`FixtureHook`). Run the server with
`go run ./internal/desktop/fixture -addr 127.0.0.1:0 -scenario edge-home`. On the iPhone (Debug builds)
it is opened at once as a Mac named "Fixture" (native screens; `-webDeck YES` shows the page instead). For the simulator pass the variable as `SIMCTL_CHILD_MAD_FIXTURE_URL`.

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
runs some classes only; `FIXTURE_ROOT=<dir>` takes the fixture server from another checkout. `SIM_NAME=<name>` uses (and makes) another simulator, so two runs do not share one; the name must start with "mad UITest" (anything else, your own simulators included, is refused before any simulator is touched; `bash ios/scripts/uitest_guard_test.sh` checks that).
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

### The Mac window, for screenshots

Debug builds of the Mac app take `-windowSize 1200x800` (leaves full screen and sizes the window), `-windowAppearance
light|dark`, `-mad.sidebarWidth 300` (the sidebar's stored width) and `-windowInfo <path>`, which writes the window's number there once it is
drawn: `screencapture -x -o -l $(cat <path>) shot.png` then takes it (the shell has the permission the app lacks).
With `-fixtureURL <url> -nativeOpen <row id>` it opens a channel or an agent at once.

### Type and the message column

`MadType` (MadUI) is the one type scale: on the Mac the sizes are chosen (row names 16, messages 15, last lines
and pills 14, labels 13), on the iPhone they are the text styles. `MadColumn` is the message column of a channel
or a conversation: at most 820pt on the Mac (720 on the phone), centred, with 32pt (14) at the sides; the avatar
is in a 34pt gutter, others' bubbles start after it, mine end at the right edge, a bubble is as wide as its text
up to 75% of the column, and the lines (handoffs, tools) and the composer have the column's two edges.

### The Mac window

The Mac window is laid out as a chat app is, not as a NavigationSplitView (`MacShell`, MadShell): the content goes
under the traffic lights (no title bar), a sidebar of ours (`MacSidebar`: round search and new buttons, the
machine's tile and name, the list with a soft grey row for the open one, the usage and ⋯ at the bottom) is held
between 260 and 420pt (320 at first), dragged by its edge, and hidden and shown with ⌘\ or its button, animated. In a channel or
a conversation there is no bar: the name floats at the top as a pill, the messages are one column (bubbles at most
58% of it, no avatars beside them, the sender's small character by its name), and the composer is a rounded bar
with the column's edges. Typing @ in a channel offers its members (↑ ↓ and ⏎ or Tab, or a click).

Debug launch arguments: `-sidebarHidden YES`, `-mad.sidebarWidth 420`, `-composerText "@r"`.

### What the device keeps of a Mac

`SnapshotCache` (MadKit) keeps the Mac's own answers on the device, in `Application Support/mad/cache/<Mac id>/`:
the deck's last look (`deck.json`), each channel's last 200 messages (`ch.<id>.json`, a `/api/bus/messages` answer) and
each agent's last 200 transcript entries (`chat.<id>.json`, a `/api/chat` answer that starts over). `DeckStore`,
`ChannelStore` and `ChatSession` read them when they are made, so a screen draws at once, and then ask only for what
came after (`since`, or the file and offset); `MadClient` merges each answer in, and writes are put off 0.5s and merged
(`flushAll()` when the app leaves the foreground). A Mac's folder goes when it is unpaired (`PairingBook.remove`) and
when its token is refused. No token is kept, and the fixture server is never cached (`Mac.fixtureID`).
