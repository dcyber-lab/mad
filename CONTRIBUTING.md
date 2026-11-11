# Contributing to mad

Thanks for your interest! Bug reports, feature ideas, and pull requests are
all welcome.

## Reporting bugs

Please include:

- OS, terminal emulator, and `tmux -V`
- The agent involved (`claude --version`, `codex --version`, …)
- Steps to reproduce, and what you expected instead
- Relevant output of `mad scan [path]` and `~/.local/state/mad/sidebar.log`
  if the sidebar crashed

## Development

```sh
git clone https://github.com/dcyber-lab/mad.git
cd mad
make build        # ./mad
make test         # go vet + go test
```

mad talks to its own tmux server. To experiment without disturbing your
running deck, point it at a throwaway socket and state directory:

```sh
MAD_SOCKET=mad-dev XDG_STATE_HOME=/tmp/mad-dev ./mad
MAD_SOCKET=mad-dev ./mad kill-server
```

### Code layout

`main.go` only calls `internal/cli`. Everything else lives in `internal/`,
with dependencies pointing downwards in this list:

| Package              | Responsibility                                                   |
| -------------------- | ---------------------------------------------------------------- |
| `internal/cli`       | Subcommands (`mad`, `add`, `switch`, `scan`, `hook`, …)          |
| `internal/desktop`   | `mad desktop`: the deck in a window (web sidebar, conversations drawn from transcripts and hosts) |
| `internal/ui`        | Bubble Tea sidebar, project picker, session picker               |
| `internal/rounds`    | The deck's work by the clock, with nobody at the keys: queued messages and bus digests sent to agents whose turn is over, lapsed handoffs marked, work followed up, timers fired, idle agents put to sleep. No loop of its own: the sidebar has the rounds made on each tick |
| `internal/team`      | Teams: agents in roles started together in a channel, from a template |
| `internal/run`       | Runs: a task handed through agents in roles by a flow (steps, reviews, questions, gates, what a step must leave behind) |
| `internal/queue`     | Messages waiting for an agent's turn to end (the sidebar sends them) |
| `internal/host`      | `mad host`: an agent run headless in its pane for the desktop window to draw (window mode) |
| `internal/phone`     | `mad phone`: the window served to the user's phone over Tailscale (`tailscale serve`), its token and QR code |
| `internal/push`      | Web Push to the user's devices: subscriptions, VAPID keys, RFC 8291 encryption (stdlib only) |
| `internal/bus`       | Channels and DMs where agents and the user talk: messages, mentions, digests, guards (the sidebar delivers) |
| `internal/mcp`       | A small MCP stdio server: `mad mcp` serves the bus's tools to each agent |
| `internal/deck`      | tmux layout, agent lifecycle, generated tmux config              |
| `internal/live`      | `mad stream`: what agents write as they write it (an API proxy for claude and codex, an extension for pi) |
| `internal/transcript`| Titles, prompts, tool calls and tokens from agents' transcripts  |
| `internal/discover`  | Per-agent providers: history, sessions, transcript lines, processes outside the deck |
| `internal/status`    | Hook reports and running/waiting/idle inference                  |
| `internal/notify`    | Desktop notifications and the user's notify command              |
| `internal/poke`      | Socket for other mad processes to reach the sidebar              |
| `internal/agent`     | Agent kinds (built-in + `agents.json`) and their commands        |
| `internal/git`       | Checkout info for the sidebar; worktrees for agents               |
| `internal/tmux`      | Thin wrapper around the private tmux server                      |
| `internal/pack`      | Packs: the flows, team templates and skills that runs and teams work by, read from `packs/` (built in), the user's and the project's directories |
| `internal/state`     | Persistent project/agent tree                                    |
| `internal/paths`     | Config/state locations, path and shell helpers                   |
| `internal/textutil`  | Width-aware truncation/padding for the terminal                  |

### Packs: how work is done is not code

`internal/run` and `internal/team` know steps, roles, reviews, gates,
checks and channels, and nothing of any particular way of working. That
is in `packs/`: flows and team templates as JSON, the skills their roles
work by as markdown, built into the binary and replaceable from
`~/.config/mad/packs/` or a project's `.mad/packs/`.

- A change to how a flow works (a step, a prompt, a skill, who plays a
  role) is a change to a pack's files. `mad pack check packs/pstack`
  checks it, `go test ./internal/cli` checks the built-in ones, and
  `ln -s $PWD/packs/pstack ~/.config/mad/packs/pstack` tries it in your
  deck without a build.
- A change to what any flow can say (a new field of a step, a new check)
  is a change to `internal/run`, with a test there, and is described in
  `skills/mad-flow/SKILL.md` so that agents can write flows that use it.

### Adding an agent

Launching an agent is configuration: an entry in `agent.builtin` (or a
user's `agents.json`) with its commands, waiting patterns and icon. That
alone runs it in the deck.

Everything else about an agent goes through `discover.Provider`:

- how mad wires itself in: files written at deck start (claude's settings),
  the placeholders its commands use (`{claude_settings}`,
  `{codex_notify}`), and what `mad hook <kind>` makes of its reports
  (status, usage limits, claude's status line);
- where its sessions live and which a person had;
- what a transcript line says (tokens, title, prompt, tool call, limits);
- how to recognize it running in another terminal or a desktop app, and
  whether a deck started it.

Each agent is one file, `internal/discover/claude.go` and `codex.go`; a new
one implements the interface in its own file and is added to `providers`.
Nothing outside those files and the built-in kinds names an agent, except
`.claude/worktrees`, the worktree directory mad shares with Claude Code.

### Tests

```sh
go test ./...          # everything
go test -race ./...    # what CI runs
```

- Every package has its own `*_test.go`. Tests never touch your real home,
  state or tmux: they point `HOME`/`XDG_*` at temp dirs.
- `internal/tmux` and `internal/deck` include integration tests against a
  real tmux server on a throwaway socket; they are skipped when `tmux` isn't
  installed.
- `internal/discover` builds fake Claude/Codex histories in a temp home and
  stubs `ps`/`lsof` through package variables. A new provider gets a
  fixture there, and transcript lines for it in `internal/transcript`.
- `internal/ui` drives the Bubble Tea model with messages only; the commands
  it returns (tmux work) are not executed.

### Performance

Status arrives as events where it can (hook reports and tmux's pane-died,
through `internal/poke`); polling covers the rest: every 500ms the screens
of agents without hooks, every 3s a full poll as a safety net. Anything per
agent per poll still adds up. Benchmarks cover the full poll against a live
tmux server, status updates, rendering and switching, at up to 200 agents:

```sh
go test -run '^$' -bench . -benchtime 20x ./internal/ui
```

A poll with 50 busy agents takes about 17ms (one tmux call captures every
screen); keep it far below the 500ms interval.

Every mad process runs the init of every package, `mad hook` too, which
claude starts a few times per tool call of each agent. Package-level
values that cost anything to build, regexps included, are built on first
use (`sync.OnceValue`); `GODEBUG=inittrace=1 ./mad hook claude </dev/null`
shows what init costs.

## Pull requests

- Keep changes focused; one topic per PR.
- Run `gofmt` and `make test` before pushing.
- Describe how you tested the change (ideally with a real agent in the deck).
