# Bot creates an agent or a team: UI spec

Approved mockup: http://hengs-macbook-pro-1.tailf4e3.ts.net:8765/bot-creates.html
(file: `share/bot-creates.html` next to this spec). The person approved it on 2026-11-09 (#ios #884, "可以，这样搞吧").
Data: PR #169 (`bot-creates`, claude-45be), fixture scenario `create-confirm`.

A bot asks for what's missing with the ask card, then a **create** item waits for the person: a confirm card that
says exactly what will be started, with 允许 / 拒绝 / 改一下…. After the answer, the card is replaced, where it
stood, by mad's own result line. The bot's words stay separate.

Read with the mockup open. Sizes are in pt. Colours are the app's tokens (MadUI `ChannelStyle`); the page uses its
own CSS variables of the same roles.

---

## 1. Data the clients read

From the inbox item (`kind: "create"`), as #169 has it plus h-d5e1ec11's additions. Field names follow what
claude-45be lands; if a name differs, use theirs.

| field | use |
|---|---|
| `agent`, `title` | who asks (agent id and its label). The label goes in the channel head and the home row. |
| `create.type` | `team` or `agent`. **Translate by this**, never by `context` ("wants to start a team") or `text`, which are English for the TUI. |
| `create.name`, `create.project_name` | name line |
| `create.roles[{role, kind, model, lead}]` | team: the role rows |
| `create.goal` (or the team's first message) | team: 第一件事 |
| `create.kind`, `create.model` | agent: the model row |
| `create.channel` | agent: the 加入群聊 row (omit the row when empty) |
| `create.message` | agent: 第一句话 |
| `from` | the channel the bot was answering in: the card also floats in that channel |
| `since` | 等了 … / 刚刚 |
| acts | allow (`primary`, not a row act: only beside the body), deny (`row: true`), and deny-with-words (`verb: deny`, `input: true`, act kind `create`, server label "Ask for changes…"). Labelled by verb + kind, never by the English label. |
| `full: true`, `where`, `body` | waiting items carry the whole body (first message, goal, template, roles with briefs, kickoff) |
| `snapshot.creates[]` | same key as the card (`create:c-…`), with `outcome {status, agent, team, channel, reason, reply, at}`. `status` is one of `creating`, `created`, `declined`, `changes` (the person's words in `reply`), `expired`, `failed` (`reason`). Items age out after 24h. |
| channel `Message.event` | `created`, `create_declined`, `create_changes`, `create_expired`, `create_failed`. `ref` = the item key; `data {type, name, agent, team, channel, reason, words}` is enough to draw the line without the item. `text` is an English fallback, not for drawing. |
| mad's DM to the bot | the outcome is DMed to the asking bot (a sentence plus JSON `{outcome, request, say, data}`). Hide it in the bot's conversation and in DM lists: it is mad's plumbing, and the result line already says it. |

Names as landed in #169 at 29c77f9 (claude-45be). The fixture `create-confirm` has all of these, including a
`changes` item.

**The tool doesn't wait** (changed in b0da's review of #169, #893 item 1). `agent_new` / `team_start` return at
once with `{status: "asked", request: "c-…"}`. The card is the inbox item. When the person answers, mad sets the
outcome on the item (`snapshot.creates["create:c-…"]`), DMs it to the asking bot, and posts a channel event line when
`from` is set. The bot's next words come after that DM.

**Model words.** Show the kind's display name as the app shows it elsewhere (Claude, Codex, Pi…). Add the model
when there is one: an alias is capitalised (`sonnet` → "Claude Sonnet"), a full model id is shown as given
("Codex gpt-5.5"). No model means the kind alone ("Codex"). Role names and the team/agent name are the bot's words:
show them as given.

---

## 2. States

| state | what the person sees |
|---|---|
| **asking** | The bot's ask card (想问你 + chips; several questions on one card, 发送 enables once each has a pick). This is builder's `native-ask-chips` (h-9599edc3) on iPhone/Mac and #163's chips on the page. Nothing new here. |
| **waiting** | The confirm card (§3). It shows in the asking agent's conversation, in the `from` channel, and as a row in home's 待处理 (§5). The phone notifies as for other waiting items: "architect 想创建一个团队「发布检查」". |
| **sending** | The tapped button keeps its place and size and reads 允许中… / 拒绝中… / 发送中…, pulsing (opacity 1 → 0.6, 1.1s, alternate). The other buttons are disabled. The card doesn't change size. Same as #163's tapped chip. |
| **change** (改一下…) | A text field appears under the card's content. The pills become 取消 / 发送, and 发送 stays disabled while the field is empty. 发送 sends the deny-with-words act; 取消 goes back to waiting. |
| **creating** | After 允许, while mad starts it (`status: creating`). The card leaves and the result line shows in its pending form: a 20pt system spinner where the tile goes, the title 正在创建团队「X」…, no sub and no 打开. A team can take several seconds. |
| **created** | The card leaves (existing transition: move down + fade), or the creating line turns into this. The result line (§4) appears where it was. The new team is the top row of 群聊; a new agent is the top row of its project (§5). |
| **declined** | Grey line: 你拒绝了，没有创建团队「X」 |
| **changes** | Grey line: 你要改一下：评审也用 Claude. The bot then proposes again with a new card. |
| **expired** | No answer within ~10 min: the card goes; grey line 没等到你确认，没有创建团队「X」 |
| **failed** | Grey line: 没建成团队「X」：<reason> (the reason is the server's text, shown as given) |
| **answered elsewhere** | Answered on another device: the card goes without a sending state, and the line appears. |
| **gone later** | The created agent or team no longer exists: the result line stays, its sub says 已经不在了, and there is no 打开. |

Setting `agents_create_without_asking` on: there is no card, but the result line still appears, so the person always
sees what was created.

---

## 3. The confirm card

### iPhone: the floating sheet (same as AskSheet's permission card)

- **Sheet:** 12 side margin, padding 18 top / 18 sides / 14 bottom, corner radius 28, the AskSheet shadows, fill
  madPaper (madRaise in dark). Blocks 12 apart. Past 420 tall it scrolls inside, as AskSheet does.
- **Head:** 17 semibold madWait, one line: 想创建一个团队 / 想创建一个 agent. In a channel, with the asker's label:
  architect 想创建一个团队. On the right, 14 madFg3: 刚刚 / 等了 3 分钟.
- **Name line:** the name in 21 semibold madFg (one line, ellipsis), then 10 gap, then 项目 mad in 15 madFg3, on one
  baseline.
- **Spec box:** madCode fill, radius 14, padding 2 vertical / 14 horizontal. Rows are at least 40 tall, separated by
  1px madLine. Left 16 madFg (one line, ellipsis); right 15 madFg3 (no wrap).
  - Team: one row per role: role name → model words. The lead's row gets a small 13 madFg3 "负责人" after the role
    name, unless the role name already is 负责人 / lead.
  - Agent: 模型 → Claude Sonnet, then 加入群聊 → ios (only when `channel` is set).
- **First block:** the label 14 madFg3 (第一件事 for a team, 第一句话 for an agent), 4 gap, then the text in 16
  madFg, line height ~1.45, in full (the sheet scrolls if long).
- **Pills:** the existing AskPill pair, 50 tall: 拒绝 outlined on the left, 允许 filled madFg on the right.
- **改一下…:** a quiet text button under the pills, 15 madFg2, full width, at least 40 tall (where 本次会话都允许
  sits on a permission card).
- **Change field:** madRaise fill, radius 14, padding 14 / 12, 17pt, 1–4 lines, focused with the keyboard up.
  Placeholder: 告诉它要改什么. It sits under the first block; the pills become 取消 / 发送.

### Mac: the column card (same as AskCardView)

- Border madWait at 45%, radius 12, at the column's foot over the composer, as the permission card is.
- **Head bar:** madWaitBg, padding 8 / 12. The asker's avatar 18 with its wait ring, then the head in 12.5 semibold
  madWait. Right: the age, 11.5 madFg3.
- **Body:** padding 10 / 12, rows 8 apart, a 1px madLine under it.
  - Name 15 semibold, then 项目 mad in 13 madFg3.
  - Roles as a two-column grid at 13: role in madFg, model words in madFg3, gaps 3 / 16.
  - "第一件事 / 第一句话" in 13 madFg3, followed by the text in madFg.
- **Acts:** padding 9 / 12, 8 apart. 允许 primary (Return), 拒绝 (Esc), then 改一下… as a plain button. The keys hint
  ⏎ 允许 esc 拒绝 sits at the right. The change field replaces the acts row, with 发送 / 取消.

### Page

Build it from the page's own permission card (`.ask`, #163/#864) so the two look alike: the head row with the avatar
and words, the body as above, and acts as chips. 允许 is the only filled chip, then 拒绝, then 改一下… as the dashed
chip (like 回复). At phone width it rises from the bottom as the page's ask does. Words go through T().

---

## 4. The result line and the grey lines

Both are mad's lines, not the bot's text.

**Where they come from.**
- **In an agent conversation,** draw them in place of the bot's `agent_new` / `team_start` tool step (mad's MCP
  tools: claude names them `mcp__mad__agent_new` / `mcp__mad__team_start`, other kinds use their own prefixes).
  - The step's result is `{status: "asked", request: "c-…"}`. Look up `snapshot.creates["create:c-…"]` and draw the
    line from that item's outcome.
  - While the item has no outcome, draw nothing at the step: the card is the place.
  - If the item is gone and no outcome was ever seen (an old conversation, or a deck restart), draw nothing.
  - Never draw from the tool's text, and never match English.
- **In a channel,** draw them from mad's event line (`event`: `created` / `create_declined` / `create_changes` /
  `create_expired` / `create_failed`, with `data`), as #133/#134 do for paused/trimmed. #169 posts it when a `from`
  item is answered. While the item in `snapshot.creates` is `creating`, the channel can show the creating line from
  it.
- **The tool step's result** is `{"outcome":"asked","request":"c-…","say":…}`. Match `request` to
  `snapshot.creates` key `create:<request>`. With `agents_create_without_asking` on, it is already
  `{"outcome":"created", …}`: draw the created line straight from it. Tool errors (unknown kind, no project, budget
  spent) stay error steps, as today.

**Result line (created).**
- **iPhone:** across the column, not in the bot's avatar column. Margins 16 top / 16 sides. 1px madLine2 border,
  radius 20, padding 12 / 12 / 12 / 14, madPaper fill.
  - A grid of: the avatar or tile at 40, gap 12, the text, gap 12, and the 打开 pill.
  - Title: 16 semibold, one line, ellipsis. 团队「发布检查」建好了 / 「scout」建好了.
  - Sub: 14 madFg3, one line. Team: 3 个成员 · 负责人在工作 (the lead's state word, or the project name if the lead
    isn't known). Agent: Claude Sonnet · 在工作.
  - Tile: the team channel's tile as the home list draws it. Agent: its avatar with its state ring.
  - 打开 pill: 34 tall, radius 17, 1px madLine2, 15 semibold madFg, then chevron.right 13 semibold madFg3, 2 gap.
  - The whole line is one button. It opens the team channel or the agent. Accessibility label: 团队「发布检查」建好了，打开.
- **Mac:** the same, at 14 / 12.5 type, a 32 avatar and a 28 pill, at most 520 wide.
- **Page:** the same, with the page's sizes.

**Grey line (declined, changes, expired, failed):** centred, 14 madFg3, line height ~1.4, 16 top margin, 24 side
margins. Wraps, with no border and no buttons.

---

## 5. Home and lists

- **待处理 row (iPhone hero, Mac sidebar wait block, page attention list).** Use the existing waiting row.
  - Name: the asker's label.
  - What line: 想创建一个团队 / 想创建一个 agent.
  - Box: one line, ellipsis, not mono. Team: 发布检查 · 3 个角色 · mad. Agent: 周报助手 · Claude Sonnet · mad.
  - Acts: 查看 (filled) and 拒绝. 查看 opens the `from` channel if set, else the asking agent. There is no 允许 here: the
    person allows on the full card.
  - **Rule (b0da's review):** 允许 appears only where the full spec shows: name, project, roles or model and channel,
    and the first message or goal. A one-line row never offers 允许. If a place can't show the whole body, offer
    查看 instead.
- **After created:**
  - The team's channel row is first in 群聊, with its unread dot.
  - A new agent is first in its project. Sort agents started in the last 30 minutes newest first, ahead of the rest.
  - The first time the new row shows after 允许 on this device, it gets a soft highlight: madAccent at 7% fading to
    0 over 1.2s, none with Reduce Motion.

---

## 6. Words (zh / en)

Every word goes through the app's string catalogue (native) or T() (page). `%@` is filled in.

| zh | en |
|---|---|
| 想创建一个团队 | Wants to create a team |
| 想创建一个 agent | Wants to create an agent |
| %@ 想创建一个团队 | %@ wants to create a team |
| %@ 想创建一个 agent | %@ wants to create an agent |
| 项目 %@ | Project %@ |
| 负责人 (lead tag) | lead |
| 模型 | Model |
| 加入群聊 | Joins |
| 第一件事 | First task |
| 第一句话 | First message |
| 允许 / 拒绝 | Allow / Deny |
| 改一下… | Change… |
| 告诉它要改什么 | Tell it what to change |
| 取消 / 发送 | Cancel / Send |
| 允许中… / 拒绝中… / 发送中… | Allowing… / Denying… / Sending… |
| 正在创建团队「%@」… | Creating team “%@”… |
| 正在创建「%@」… | Creating “%@”… |
| 团队「%@」建好了 | Team “%@” is ready |
| 「%@」建好了 | “%@” is ready |
| %lld 个成员 | %lld members |
| %lld 个角色 | %lld roles |
| 打开 | Open |
| 查看 | View |
| 你拒绝了，没有创建团队「%@」 | You declined, so team “%@” wasn't created |
| 你拒绝了，没有创建「%@」 | You declined, so “%@” wasn't created |
| 你要改一下：%@ | You asked for changes: %@ |
| 没等到你确认，没有创建团队「%@」 | No answer in time, so team “%@” wasn't created |
| 没等到你确认，没有创建「%@」 | No answer in time, so “%@” wasn't created |
| 没建成团队「%@」：%@ | Couldn't create team “%@”: %@ |
| 没建成「%@」：%@ | Couldn't create “%@”: %@ |
| 已经不在了 | No longer here |
| %@ 想创建团队「%@」 (notification) | %@ wants to create team “%@” |
| %@ 想创建「%@」 (notification) | %@ wants to create “%@” |

Existing words reused as they are: 刚刚, 等了 %@, 在工作 / 空闲 / 在等你 / 已停止.

---

## 7. Test contract and acceptance

- **Accessibility identifiers** (native) and `data-test` (page): `create.card`, `create.allow`, `create.deny`,
  `create.change`, `create.field`, `create.send`, `create.cancel`, `create.result`, `create.open`, `create.note`
  (grey line). Labels are the words above.
- **Fixture:** `create-confirm`. It has an agent item with `channel` (scout, joins #api), a team item (fix-flaky,
  2 roles, lead), one with `from`, and one already created. Add a declined and an expired one if they aren't there.
- **Screenshots for acceptance,** light and dark, iPhone and Mac (page: 1400 wide and phone width):
  - the team card and the agent card, waiting;
  - the card in its channel, with the asker in the head;
  - 改一下… open with words typed;
  - 允许中…;
  - the result line;
  - each grey line;
  - home with the 待处理 row;
  - home with the new team first and a new agent first in its project.
- **Check:** no English left in 中文 mode (no "wants to start", no tool names, no "create:" keys), nothing clipped at
  the largest Dynamic Type, and the card never covers the composer's send button.
