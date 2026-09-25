# The README demo

`docs/demo.gif` is recorded from a real deck: mad built from this checkout,
real git repositories with local remotes, real keypresses. Only the agents
are stand-ins: `fake-agent` plays a scripted part and does what mad can see
of claude or codex. It reports status through `mad hook claude`, writes the
transcript they would (title, prompts, tool calls, token usage), and edits
and commits files in its checkout.

To record it again after the UI changed:

```sh
pip install playwright pillow && playwright install chromium   # once
make demo
```

That runs `record.py`, which plays the timeline and captures the screen every
100ms, then `render.py`, which draws each capture with a caption bar and
writes `docs/demo.gif`. Needs tmux, Go, Python 3 and git.

- The story is the timeline at the end of `record.py`: `say` sets the
  caption, `key` / `typ` press keys in the outer terminal, `trig` tells an
  agent to move on. What each agent does is its role in `fake-agent`.
- Keys go through an outer tmux standing in for your terminal, so global
  bindings (`Alt-n`, `Alt-s`) work as they do for you. Mind where focus is:
  opening an agent or a diff moves it to the stage, and closing a diff
  brings it back to the sidebar.
- `CHROMIUM=/path/to/chrome` renders with that browser instead of
  Playwright's.
