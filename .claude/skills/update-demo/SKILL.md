---
name: update-demo
description: Re-record docs/demo.gif, the animated demo at the top of the README, from a real mad deck. Use when the sidebar, keys or features changed and the demo no longer shows them, or when asked to update, redo or extend the README demo/animation/GIF.
---

# Update the README demo

`docs/demo.gif` is recorded, not drawn: `docs/demo/record.py` builds mad from
this checkout, sets up real git repos with local remotes and a private HOME,
runs a deck on its own tmux server inside an outer tmux (the "terminal"),
presses real keys on a timeline and captures the screen every 100ms.
`docs/demo/render.py` draws each capture with a caption bar and writes the GIF.
The only fakes are the agents: `docs/demo/fake-agent` plays a role per agent
and does what mad can observe of claude/codex — `mad hook claude` reports,
transcripts (title, prompts, tool calls, token usage) where mad reads them,
file edits and commits in its checkout. Read `docs/demo/README.md` first.

## 1. Decide the story

List what changed since the demo was made (`git log -- docs/demo.gif` for when,
then the commits since). Pick what a newcomer should see in about a minute:
the overview first, then the few flows that matter most. Every step gets a
short caption; keys show as badges. Keep it under ~60s and one take.

## 2. Edit the timeline and the agents

- Timeline: the end of `record.py`. `say(t, caption, keys)` sets the caption,
  `key(t, ...)` / `typ(t, text)` press keys in the outer terminal, `trig(t, name)`
  tells an agent to move on (fake-agent waits for `$REC/trig/<id>-<name>`, or
  `new-<name>` for agents created during the demo, whose ids aren't known in
  advance).
- Setup: projects, files, remotes and agents (with their roles) are in the top
  half of `record.py`; roles live in `fake-agent`. If a feature reads new data
  (a transcript field, a git state), make fake-agent or the setup produce it
  the way the real thing does — check the reader in `internal/` for the exact
  format rather than guessing.

## 3. Record

```sh
python3 docs/demo/record.py WORKDIR     # WORKDIR: short path, e.g. /tmp/madrec
```

Needs tmux, Go, Python 3, git. The sidebar's socket lives in WORKDIR and Unix
socket paths are limited (~104 bytes on macOS, 108 on Linux): if the path is
long, mad silently falls back to polling and the demo gets laggy.

## 4. Check every step before rendering

Print the screen at each step's time and confirm it shows what the caption
says (the key did land, the status changed, the counts moved):

```sh
python3 docs/demo/inspect.py text WORKDIR 3 11 14 20 ...
```

Things that have gone wrong before:
- **Focus.** Opening an agent, a diff (`v`) or a finish command (`f`) moves
  focus to the stage; quitting the diff or the finish pane brings it back to
  the sidebar. `Alt-s` *toggles*, so pressing it when focus is already in the
  sidebar sends the next keys into the agent (you'll see them typed at its
  prompt). Only press `Alt-s` when focus is on the stage.
- **Statuses of screen-read agents** (codex, pi, shell): one started off stage
  prints, stops, and turns `● done`. Park it on stage for a few seconds during
  setup (as `record.py` does for infra's codex) if it should read idle.
- **Transcript-driven info** (titles, the line under an agent, tokens) refreshes
  every ~5s and when a turn ends, so give it time before the caption that
  points at it.
- Timings are wall-clock: a slow machine can push a step past its caption.
  Leave slack between steps rather than tightening.

Fix, re-record, re-check until every step is right.

## 5. Render and look at it

```sh
pip install playwright pillow && playwright install chromium   # once
python3 docs/demo/render.py WORKDIR                             # → docs/demo.gif
python3 docs/demo/inspect.py sheet docs/demo.gif /tmp/sheet.png 2 12 20 30 ...
```

`CHROMIUM=/path/to/chrome` renders with an existing browser (in a Claude Code
cloud session: `CHROMIUM=/opt/pw-browsers/chromium`). Open the contact sheet
and a frame or two at full size: text crisp, nothing cut at the right or
bottom edge, captions readable. Aim for under ~2 MB.

`make demo` runs record + render in one go once the timeline is settled.

## 6. Ship

- Update the caption under the GIF in `README.md` if what it shows changed.
- Commit `docs/demo.gif` with the script changes, and say in the message what
  the demo now shows.
- Show the user the GIF (and the contact sheet) before calling it done.
