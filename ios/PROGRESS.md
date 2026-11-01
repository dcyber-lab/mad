# iOS and Mac apps: where the work stands

The record of team ios's work on mad's phone and Mac apps, so it can be picked up after the agents are
cleared. How to build, pair and test is in [README.md](README.md); this file says what is done, what was
decided and what is left. Last updated 2026-11-01.

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
- **Mac layout:** no title bar; the name floats as a pill with a fade under it; one centred column (bubbles
  at most 58%); 15pt messages with loose line spacing; our own sidebar, 260–420pt, 320 at first, hidden and
  shown with ⌘\ (animated unless Reduce Motion); the usage line at its foot; @ offers a channel's members.
- **Language:** the UI is Chinese; tool names (Read, Bash), claude and codex, slash commands and agent text
  stay as they are. The server's labels stay English (the TUI draws them); the clients translate by verb.

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

- **Native screens still on the page:** terminal (SwiftTerm), diff, board, usage, skills, devices,
  settings. Today the ⋯ menu and the conversation's terminal button open the page on that view.
- **Checks by hand on the iPhone:** a channel scrolls (iOS 18's simultaneousGesture can block a ScrollView);
  the 群信息 panel slides in and out, including the edge swipe.
- **More Chinese:** "Nothing in its transcript yet.", the new-chat hint, the page's other hints and menus,
  the iPhone's pairing errors.
- **Polish:** a fade where a long sidebar list meets its footer; the collapsed sidebar is not remembered;
  @ works only at the end of the text (SwiftUI's TextField has no cursor API).
- **Flaky UI test:** EdgeHomeTests.testSearchFindsTheTeamRow passes and fails in turn, on main too.
- **The codex reviewer stalled** and holds three old handoffs (h-1801bebd, h-82f08b03, h-a7b066d5); close or
  restart it.
