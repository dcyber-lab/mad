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
| `internal/ui`        | Bubble Tea sidebar, project picker, session picker               |
| `internal/deck`      | tmux layout, agent lifecycle, generated tmux/claude configs      |
| `internal/transcript`| Titles, prompts, tool calls and tokens from agents' transcripts  |
| `internal/discover`  | Per-agent providers: history, sessions, transcript lines, processes outside the deck |
| `internal/status`    | Hook reports and running/waiting/idle inference                  |
| `internal/notify`    | Desktop notifications and the user's notify command              |
| `internal/poke`      | Socket for other mad processes to reach the sidebar              |
| `internal/agent`     | Agent kinds (built-in + `agents.json`) and their commands        |
| `internal/git`       | Checkout info for the sidebar; worktrees for agents               |
| `internal/tmux`      | Thin wrapper around the private tmux server                      |
| `internal/state`     | Persistent project/agent tree                                    |
| `internal/paths`     | Config/state locations, path and shell helpers                   |
| `internal/textutil`  | Width-aware truncation/padding for the terminal                  |

### Adding an agent

Launching an agent is configuration: an entry in `agent.builtin` (or a
user's `agents.json`) with its commands, waiting patterns and icon. That
alone runs it in the deck.

Everything mad reads from the agent's own files goes through
`discover.Provider`: where its sessions live and which a person had, what a
transcript line says (tokens, title, prompt, tool call, usage limits), how
to recognize it running in another terminal or a desktop app, and how to
read its `mad hook` reports. Each agent is one file, `internal/discover/claude.go`
and `codex.go`; a new one implements the interface in its own file and is
added to `providers`. The sidebar, session picker, sync and `mad scan`
work from that list; past it, agents are only named where they are
launched (the `{claude_settings}` and `{codex_notify}` placeholders,
claude's status line).

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

## Pull requests

- Keep changes focused; one topic per PR.
- Run `gofmt` and `make test` before pushing.
- Describe how you tested the change (ideally with a real agent in the deck).
