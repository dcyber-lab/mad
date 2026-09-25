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

| File            | Responsibility                                            |
| --------------- | --------------------------------------------------------- |
| `main.go`       | CLI entry point and subcommands                           |
| `deck.go`       | tmux layout, agent lifecycle, generated configs           |
| `tmux.go`       | Thin wrapper around the private tmux server               |
| `sidebar.go`    | Bubble Tea sidebar TUI                                    |
| `picker.go`     | Project picker (fuzzy search + path completion)           |
| `sesspicker.go` | Session picker for new claude/codex agents                |
| `agents.go`     | Agent kinds (built-in + `agents.json`)                    |
| `hook.go`       | `mad hook` — status reports from agents                   |
| `sessions.go`   | Claude/Codex session history readers                      |
| `discover.go`   | Auto-sync: history scan and external process detection    |
| `scan.go`       | `mad scan` debugging output                               |
| `state.go`      | Persistent project/agent state                            |
| `paths.go`      | Config/state paths and helpers                            |

## Pull requests

- Keep changes focused; one topic per PR.
- Run `gofmt` and `make test` before pushing.
- Describe how you tested the change (ideally with a real agent in the deck).
