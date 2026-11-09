# iOS and Mac apps: where the work stands

The record of team ios's work on mad's phone and Mac apps, so it can be picked up after the agents are
cleared. How to build, pair and test is in [README.md](README.md); this file says what is done, what was
decided and what is left. Last updated 2026-11-09.

The goal, from the person: an iOS version of mad, modelled on Grok Bot, that controls the deck remotely
(Tailscale or a Cloudflare tunnel). They later asked for native apps on both the iPhone and the Mac.

## Shipped

| PRs | What |
| --- | --- |
| #72–#86 | iOS app that pairs by QR code; Cloudflare tunnel as a second way in; pairing codes, a token per device, revocation; no tokens in URLs; `make ios-device` with the 7-day note |
| #87–#101 | Phone type and the Usage page; pictures in channels; the deck page as the whole app; the calm list and channel (mockup v4); fixture server and UI tests with a screenshot report; bubbles never wider than their column |
| #102–#109 | Native foundation: MadKit stores, MadUI (characters, home list, conversation, channel view), MadShell mappings, the Mac app |
| #110–#114 | mad's own budget on the Usage page; the page reloads itself after a deploy; the characters of the approved design on the page and in the apps |
| #115, #119 | Native screens are the default in both apps (the page covers what is not native yet); `mad desktop` opens madmac.app |
| #116–#118 | A desktop-width bounds check for channel bubbles; a team's members as 小组 and 帮手 (page and native) |
| #121 | The Mac window laid out as a chat app, after the Grok Bot desktop app (includes #120's type scale and message column) |
| #122, #123 | The UI in Chinese: act labels, status words, the overview, the composer, pairing, open acts, 批准计划 |
| v5 (this branch) | The approved redesign v5 (`docs/design/v5.html`) in both apps: the status ring on every avatar; Chinese and English after the system's language (String Catalogs); the ask card at the end of the conversation (a bottom sheet on the iPhone); the sidebar with 待处理 pinned on top, the machine capsule, ⌘K search, project heads with git and cost and only the person at its foot; 今日 on the Mac; a native Usage screen; the agent page with 对话 / 改动 and a Details pane (⌘I); the group chat's light own messages, @ in identity colors and avatar receipts |

Installed on the person's devices on 2026-11-01: `/Applications/madmac.app` (`make mac-install`) and the
iPhone (`make ios-device`, over the network when `xcrun devicectl list devices` says "available (paired)").
A free Apple ID's iPhone build stops opening after 7 days; run `make ios-device` again.

## Decisions

- **Characters:** eight shapes from the agent's id; pastel body, no outline, the face in the strong colour
  darkened; eight expressions by state; each bot moves its own way (`plan(id)`). The page and the apps share
  test vectors (`internal/desktop/web/testdata/character-vectors.json`). The reference is the approved
  `characters-final.html` mockup.
- **Channel:** the v4 design: a one-line head, the 群信息 side panel, quiet messages folded into runs,
  a one-line composer.
- **Members:** 小组 is the team's roles (lead first; a role with no agent or a gone agent stays as a faded
  row, 未启动 or 已离开); 帮手 is the rest (busy first, gone ones left out). Counts of members are the team's;
  counts of activity (在工作, 在等你, the red dot) cover everyone, so a waiting helper is always seen.
- **Mac layout:** no title bar; (v5) a head bar of 60pt with a hairline replaces the floating name pill in the
  conversation and the channel, and with no row open the detail is 今日; one centred column; bubbles,
  folded lines, tool lines and the ask card as wide as their content, at most 58% of it; 15pt messages at a
  line pitch of about 23pt; our own sidebar, 260–420pt, 320 at first, hidden and shown with ⌘\ (animated
  unless Reduce Motion); the usage line at its foot; @ offers a channel's members in a list that floats over
  the messages.
- **Language (changed in v5):** Chinese and English, following the system's language (iOS's per-app language
  too). The words are written in Chinese in the code and looked up in each package's `Localizable.xcstrings`
  (source language zh-Hans, `defaultLocalization: "zh-Hans"`): MadUI's with `L`/`Lf`, MadShell's with its own `L`/`Lf`,
  and the apps' own words through MadShell's `madLocalized` (an app target has no catalog). Tool names (Read,
  Bash), claude and codex, commands, agent text and the accessibility labels the UI tests look up (切换 Mac, 更多,
  搜索, 新建, 群信息, 返回, needs you, working ...) stay as they are in both languages. The server's labels stay
  English; the clients translate by an act's verb, kind and target (`ActWords` in MadShell). Unit tests pin
  `MadLocale.language = "zh-Hans"`; the UI tests launch with `-AppleLanguages (zh-Hans)`. MadKit's own few
  Chinese words (connection errors, the expiry note) are not in a catalog yet.
- **Status (v5):** one set of colors, one place: the ring around the avatar. Waiting is an amber ring (#c26a04,
  dark #fbbf24) with the body at 82% inside; working a faint track and a turning green arc (#15803d / #4ade80),
  still under Reduce Motion; just finished (`CharacterState.done`, the server's "finished") a blue dot
  (#2563eb / #60a5fa); idle nothing; stopped the whole avatar at 45% with shut eyes, never red. Red is for
  errors only; the group tile's dot is amber. Secondary text is #6b6b76 / #8a8a95.
- **Usage (v5):** its own native screen (the person's row at the sidebar's foot, ⋯ → 用量): the four windows,
  what the agents cost by project, the top spenders, mad's own budget and 今天不限 (`POST /api/budget`). No quota
  bars in the sidebar or the iPhone's home. Costs are the agents' session costs (the deck has no per-day split).
- **Agent page (v5):** two tabs, 对话 and 改动; the terminal tab and button are gone, 在终端打开 is in ⋯ (it asks
  the Mac to open the deck, `POST /api/terminal`). The 改动 tab shows the branch and counts; the diff itself is
  still the page's. The Mac's Details pane (⌘I): status, git, cost, session; Rename and Sleep are native
  (`PATCH /api/agents`, `POST /api/agents/stop`), Push / Open PR / Rebase open the page on the agent, Stop
  interrupts.

## How the team works

- **Roles:** architect leads #ios and merges; builder and helper agents implement; claude-b0da reviews
  code; a designer agent (Opus) does visual acceptance. No UI PR merges without the designer's PASS. The
  designer runs a debug madmac against the fixture at 1512×945 and 1200×800, light and dark, and the iPhone
  on its own temporary simulator, and measures against the reference. Its brief has to be given again after
  a clear: own the design and the UI acceptance, show new designs to the person as a phone-readable mockup
  before they are built, never touch the person's madmac or simulators.
- **The person approves new designs by mockup first.** Mockups and fixture screenshots go on a tailnet-only
  static server; never real transcripts.
- **Merging:** force pushes are refused here, so a branch takes `git merge origin/main`, and the PR is
  squash-merged with `--match-head-commit` at the reviewed head. Check the head SHA yourself: a worker's
  report can claim a push that was refused.
- **Deploying the page:** in the main checkout, `git pull --ff-only && make build && env -u TMUX ./mad < /dev/null`.
  Open pages reload themselves (#112).
- **Testing:** `go run ./internal/desktop/fixture -addr 127.0.0.1:0 -scenario <name>`, with scenarios
  edge-home, empty, many-channels, channel-fold, usage, pairing, team-members and long-title. Never port
  7484 or the real state directory. Window screenshots fail while the screen is locked.
- **The agent budget** is the person's to raise (⋯ → 用量 → 今天不限); `mad bus budget` refuses agents.

## Left to do

- **v5, not compiled yet:** the v5 change was written without a Swift toolchain; build both apps and run
  `swift test` before anything else, then the designer's acceptance against the mockup.
- **v5 gaps:** the handoff card and folded handoff runs (the channel still hides the agents' bookkeeping, as v4
  decided; only long messages fold, now as a dashed capsule); the ⌘K command palette (the field filters the list);
  the cost pane's in/out tokens and model (the deck does not send them); the empty group's members with @ wake;
  the 改动 tab's own diff; the iPhone footer shows "我" (the phone does not know the person's name).

- **Native screens still on the page:** diff, board, skills, devices (the iPhone's), settings. The ⋯ menu and the
  agent page's ⋯ open the page on that view.
- **Checks by hand on the iPhone:** a channel scrolls (iOS 18's simultaneousGesture can block a ScrollView);
  the 群信息 panel slides in and out, including the edge swipe.
- **More words in the catalogs:** MadKit's connection errors and expiry note, and the page's (the web page has
  its own string table).
- **Polish:** a fade where a long sidebar list meets its footer; the collapsed sidebar is not remembered;
  @ works only at the end of the text (SwiftUI's TextField has no cursor API).
- **Flaky UI test:** EdgeHomeTests.testSearchFindsTheTeamRow passes and fails in turn, on main too.
- **The codex reviewer stalled** and holds three old handoffs (h-1801bebd, h-82f08b03, h-a7b066d5); close or
  restart it.
