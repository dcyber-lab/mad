# How mad runs

A map for working on mad: which processes there are, what is kept where,
and who does what. The [README](../README.md) says how to use mad, and
[CONTRIBUTING.md](../CONTRIBUTING.md) lists the packages. Where this is
meant to go next is in [agent-runtime.md](agent-runtime.md).

## In short

mad is one binary, run as many small processes around a private tmux
server. None of them is a daemon holding the truth in its memory: what
is true is in files under the state directory, and in tmux. A process
reads what it needs when it needs it, and one that changes a file takes
a lock around reading, changing and writing it.

The sidebar, the window's server and the apps are surfaces over those
same files. One of them, the sidebar, also does the deck's work by the
clock, so a deck is running whenever anything is to happen in it, seen
or not.

```text
        you                         phone · Mac app · mad desktop
         │ attach                             │ HTTP and WebSocket, with a token
┌────────▼─────────────────  tmux -L mad  ────┼─────────────────────────┐
│ main    sidebar │ stage                     │                         │
│ _pool   agents (their TUI, or mad host) · mad stream · run runners    │
│         mad desktop -serve  ◄───────────────┘                         │
└────────┬──────────────────────────────────────────────────────────────┘
         │ read and written by all of them, and by mad hook, mad mcp, the CLI
         ▼
   ~/.local/state/mad/   state.json · status/ · queue/ · bus/ · create/ · …
```

## The processes

| Process | Where | What it does |
| --- | --- | --- |
| tmux server (`tmux -L mad`) | by itself | Holds every pane. `main` is the deck you attach to: the sidebar on the left, the stage on the right. `_pool` is hidden and holds everything else; showing an agent swaps its pane onto the stage. |
| `mad sidebar` | pane 0 of `main`, in a loop that starts it again | The terminal UI, and the keeper of the deck: it polls, works out each agent's status, notifies, and has the rounds made (below). |
| an agent | a pane of `_pool` | Its own TUI (`claude`, `codex`, …), or in window mode `mad host <id>`, which runs it headless. |
| `mad host <id>` | the agent's pane, in place of its TUI | Drives the agent through its structured protocol and serves what it does and what it asks on a Unix socket, for the window and the apps to draw. |
| `mad hook <kind>` | started by the agent, a few times per tool call | Writes what the agent reports (its status, the end of a turn, usage limits) and pokes the sidebar. The agent waits for it, so it must be quick. |
| `mad mcp --agent <id>` | started by the agent, as an MCP server | The bus's tools for that agent: post, read, hand off, start an agent or a team. |
| `mad stream` | a pane of `_pool` | Keeps the message each agent is writing as it is written, fed by a tap per kind. |
| `mad run exec <id>` | a pane of `_pool`, one per run | The run's runner: sends each role its step, reads the reply, decides what comes next, and draws the run's panel. |
| `mad desktop -serve` | a pane of `_pool`, while the phone is on | The API and the page on 127.0.0.1:7484: for the phone (through Tailscale or a Cloudflare Tunnel), the Mac app and `mad desktop`. |
| `mad desktop` | your shell | Opens the window: on the phone's server when that runs, else on a server of its own. |
| the Mac app, the iPhone app | their own | Clients of that API. The Mac app uses the phone's server when it is up, else starts a `mad desktop -no-window` of its own. |
| `mad spawn`, `send`, `wait`, `queue`, `bus`, `run`, `team`, … | wherever they are typed, an agent's shell included | One act each, on the files below. |

## What is kept where

Under the state directory (`~/.local/state/mad`, or `$XDG_STATE_HOME/mad`;
a deck on another tmux socket, `MAD_SOCKET=name`, keeps its own under
`decks/<name>/`):

| Path | What | Written by |
| --- | --- | --- |
| `state.json` | The projects, their agents and runs. What runs right now is not here: tmux knows. | Everything that adds or changes an agent, under a lock (`state.Update`). The sidebar holds a copy and merges it in field by field when it saves. |
| `status/<agent>.json`, `.turn.json`, … | What each agent last reported, how its last turn ended, what its host has open, since when it has been in its state; `quota-<kind>.json`. | `mad hook`, `mad host`, the pollers. Written without a sync: every hook writes here. |
| `queue/` | Messages waiting for an agent's turn to end. | `mad queue`, the window, the bus (a pointer per digest), the rounds as they send. |
| `bus/` | Channels and DMs as logs, each agent's place in them, the board's tasks, timers, the day's spending, teams. | `mad mcp`, `mad bus`, the window, the rounds, under the bus's lock. |
| `create/` | Agents' requests to start an agent or a team, until the person answers. | `mad mcp` asks; the window or the deck's inbox answers. |
| `skills/` | Skills agents saved for the person to approve. | `mad mcp`; approving moves one to its store. |
| `h-<agent>.sock` | A window-mode agent's host. | `mad host`. |
| `sidebar-<socket>.sock` | How other processes reach the sidebar at once (`poke`): a hook's report, panes that changed. | `mad sidebar`. |
| `stream.json` | Where `mad stream` listens. | `mad stream`. |
| `phone_token`, `phone_devices.json`, `phone_pairs.json`, `push_*.json` | The master token, the paired devices and their tokens, pairing codes, Web Push subscriptions and keys. | `mad phone`, the server. |

A run keeps what it knows in its worktree, `.mad/runs/<name>/` (`run.json`,
the design, each step's reply). Config is in `~/.config/mad/`:
`config.json`, `agents.json`, `packs/`, and files mad generates
(`tmux.conf`, `claude-settings.json`).

## How a surface gets its picture

Each surface polls for itself. The sidebar lists the panes and reads
the hooks' reports when poked or every 3 seconds, and the screens of
agents without hooks twice a second. A desktop server polls once a
second, and answers `/api/deck` when its picture changes (a long poll).
What they make of what they read is shared code, so that the surfaces
agree:

- `status.Tracker` turns an agent's pane, its last report and its screen
  into a status. It remembers (a screen that changed lately means
  running), so each process keeps a tracker per agent.
- `attention` builds the inbox, the board's "Waiting for you" and each
  agent's activity line from what the poll read. It reads nothing itself.
- `transcript` and `discover` read titles, usage and conversations from
  the files the agents write.

The window and the apps get a conversation from `/api/chat` (the
transcript), `/api/live` (the message being written, from `mad stream`),
and for an agent in window mode a WebSocket to its host (`/api/host`).

## What the deck does by itself

Package `rounds`: every second it sends the next waiting message to an
agent whose turn is over (one the person queued, or a digest of what the
bus has for it); every ten seconds it marks the handoffs lapsed whose
receiver stopped short; every minute it follows up work that stands
without a word, fires the timers that are due, settles the day's
spending and considers the progress report; every half minute it puts
agents to sleep that have sat idle long enough.

`rounds` has no loop and no terminal. Whoever keeps the deck calls it on
each tick with what its poll found. Today that is the sidebar, and so:

- A deck must be running for anything to be delivered. Starting an
  agent or a run, sending to an agent that sleeps, and opening one from
  the window or the apps start a deck, detached, when there is none
  (`drive.EnsureDeck`).
- Still in the sidebar itself: the notifications that follow from a
  status change, starting a run's runner again, adding the projects of
  sessions it finds outside the deck, and ending requests nobody
  answered (the desktop server sweeps those too).

## How a message reaches an agent

- Typed in the deck, it goes into the agent's TUI: that is tmux.
- `mad send`, the rounds and the runner of a run go through `drive.Send`:
  the text is pasted into the agent's pane, or given to its host when it
  is in window mode. An agent asleep or stopped is started first; one
  that is working or asking something refuses.
- The window and the apps write to an agent's host (`/api/send`), which
  starts it in window mode when its TUI is not in the middle of
  something; the page lines up what is written for a busy agent in its
  queue (`/api/queue`).
- A post on the bus is appended to its channel's log, and a pointer goes
  into the queue of each agent it is for. When the pointer's turn comes,
  what waits for that agent on the bus is put together as one digest.

## The API and its clients

The desktop server (`internal/desktop`) answers `/api/…` with JSON and
serves the page (`internal/desktop/web`, plain scripts, no build). Three
clients read it: the page (in the window and on the phone), and the Mac
and iPhone apps through `ios/MadKit`. `ios/README.md` has the apps'
layers.

- A request carries a token: the server's own (the local window and the
  Mac app; on the phone's server that is the master token) or a paired
  device's.
- The answers' types are in `internal/desktop/api` where they need only
  the standard library; the deck's snapshot (`desktop.Snapshot`) still
  nests types of `bus`, `attention`, `status` and `queue`.
- Every answer carries `X-Mad-API`. The number goes up only on a
  breaking change; a field added is not one.
- `internal/desktop/fixture` is a stand-in server that answers from
  scenario files. The page's Chrome tests and the apps' UI tests run
  against it, and tests hold its answers, and the apps' own fixture
  files, to the server's types.
- An error is a status and a line of English text. There are no codes
  for a client to put its own words to yet.

## Three ways agents work together

- **By hand or script**: `mad spawn`, `mad send`, `mad wait` (package
  `drive`).
- **A run**: one task through roles in fixed steps, by a flow, in a
  worktree of its own; its runner drives the agents (package `run`).
  Runs are started and followed in the terminal deck. The window and the
  apps show a run's agents as any others, not the run.
- **A team**: agents in roles that stay, in a channel of the bus, one of
  them the lead; they hand work to each other with the bus's tools
  (packages `team`, `bus`).

Flows, team templates and the skills their roles work by are files in
packs (`packs/`, package `pack`), not code.

## Words

| Word | Meaning |
| --- | --- |
| deck | The tmux server with its sidebar and agents: what `mad` opens. |
| stage, pool | The right pane of the deck, where the agent you look at is; the hidden session where the others are. |
| kind | A sort of agent: claude, codex, pi, agy, shell, or one from `agents.json`. |
| window mode, host | An agent run headless by `mad host` and drawn by the window or an app, instead of as its TUI in a pane. |
| surface | Something that shows the deck: the sidebar, the page, the Mac app, the iPhone app. |
| keeper | The process that has the rounds made: the sidebar. |
| bus | Channels and DMs where agents and the person talk; a handoff passes a piece of work on and comes back as done or declined. |
| run, flow, runner | A task taken through roles; the file that says the steps; the process that takes it through them. |
| team, template | Agents in roles in a channel; the file a team is started from. |
| pack | A directory of flows, templates and skills. |

## What is not separate yet

Known, and worth knowing before changing things:

- Status is worked out by every poller, with a tracker of its own, from
  the same files. Two surfaces can differ for a moment.
- The sidebar is the only keeper. A deck used only from the Mac app
  still runs one, unseen.
- Outside `discover` and `agent`, a kind is named where it is run
  headless (`host`), tapped (`live`), given the bus (`deck/mcp.go`),
  given a role's flags (`run/agents.go`) and looked up for skills
  (`skill/find.go`). CONTRIBUTING says what a new kind needs in each.
- `team` takes a role's agent, model and effort from `run`.
