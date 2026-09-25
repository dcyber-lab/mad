#!/usr/bin/env python3
"""Play the demo on a real deck and capture it: WORKDIR/frames.json.

Builds mad from this checkout, sets up throwaway git repos (with local
remotes) and a private HOME, starts a deck on its own tmux server inside an
outer tmux that stands in for your terminal, and sends real keys to it on a
timeline while capturing the screen every 100ms. The agents are fake-agent,
which behaves like claude/codex as far as mad can tell (hooks, transcripts,
file edits). Run render.py afterwards.

    python3 docs/demo/record.py [WORKDIR]

WORKDIR defaults to a fresh directory under /tmp; keep it short, the
sidebar's socket lives in it and socket paths are limited to ~100 bytes.
"""
import json, os, shutil, subprocess, sys, tempfile, threading, time

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
W = os.path.abspath(sys.argv[1]) if len(sys.argv) > 1 else tempfile.mkdtemp(prefix="madrec-", dir="/tmp")
H, ST, BIN = f"{W}/home", f"{W}/s", f"{W}/bin"
COLS, ROWS, SIDEBAR = 112, 26, 40
OUT, DECK = "madrec-out", "madrec"
ID = lambda n: f"00000000-0000-4000-8000-{n:012d}"
sh = lambda c, **k: subprocess.run(c, shell=True, stderr=subprocess.DEVNULL, stdout=subprocess.DEVNULL, **k)

sh(f"tmux -L {OUT} kill-server; tmux -L {DECK} kill-server")
for d in ("home", "s", "bin", "roles", "trig", "remotes"):
    shutil.rmtree(f"{W}/{d}", ignore_errors=True)
for d in (f"{H}/.config/mad", f"{H}/.codex", f"{ST}/mad", BIN, f"{W}/roles", f"{W}/trig", f"{W}/remotes"):
    os.makedirs(d)
subprocess.run(["go", "build", "-o", f"{BIN}/mad", "."], cwd=REPO, check=True)
shutil.copy(f"{HERE}/fake-agent", f"{BIN}/fake-agent")
fa = f"{BIN}/fake-agent"
json.dump([{"name": "claude", "icon": "✻", "color": "173", "start": f"{fa} claude {{sid}}", "resume": f"{fa} claude {{sid}}", "hooks": True},
           {"name": "codex", "icon": ">_", "color": "252", "start": f"{fa} codex {{sid}}", "resume": f"{fa} codex {{sid}}",
            "waiting": ["Would you like to"]}], open(f"{H}/.config/mad/agents.json", "w"))
json.dump({"notify": {"on": []}}, open(f"{H}/.config/mad/config.json", "w"))
open(f"{ST}/mad/sidebar_width", "w").write(str(SIDEBAR))

# Projects: real repos with a local remote, so branch, ±changed, ↑unpushed,
# worktrees and merges are all real.
ident = dict(GIT_AUTHOR_NAME="dev", GIT_AUTHOR_EMAIL="dev@example.com", GIT_COMMITTER_NAME="dev", GIT_COMMITTER_EMAIL="dev@example.com")
genv = dict(os.environ, HOME=H, **ident)
files = {"api": {"cache/store.go": "package cache\n", "cache/redis.go": "package cache\n", "cache/store_test.go": "package cache\n",
                 "auth/login.go": "package auth\n", "cmd/api/main.go": "package main\n"},
         "web": {"config/cdn.yaml": "ttl: 300\n", "src/app.ts": "export {}\n"},
         "infra": {"staging/main.tf": "# staging\n"}}
for name, fs in files.items():
    path, remote = f"{H}/code/{name}", f"{W}/remotes/{name}.git"
    sh(f"git init -q --bare -b main {remote} && git init -q -b main {path}", env=genv)
    for f, body in fs.items():
        os.makedirs(os.path.dirname(f"{path}/{f}"), exist_ok=True)
        open(f"{path}/{f}", "w").write(body)
    sh(f"git -C {path} add -A && git -C {path} commit -qm init && git -C {path} remote add origin {remote} "
       f"&& git -C {path} push -qu origin main && git -C {path} remote set-head origin main", env=genv)
open(f"{H}/code/api/README.md", "w").write("# api\n")  # one commit not pushed yet: ↑1
sh(f"git -C {H}/code/api add -A && git -C {H}/code/api commit -qm readme", env=genv)

# Agents, and what each one plays (fake-agent reads roles/<id>).
agents = {"api": [("claude", "chat"), ("claude", "work"), ("codex", "ask")], "web": [("claude", "done")], "infra": [("codex", "idle")]}
state, n = {"projects": []}, 0
for name, ags in agents.items():
    rows = []
    for kind, role in ags:
        n += 1
        a = {"id": ID(n), "kind": kind, "created_at": "2026-09-25T10:00:00Z"}
        if kind == "codex":
            a["session_id"] = ID(n)  # the thread id codex would have reported
        open(f"{W}/roles/{ID(n)}", "w").write(role)
        rows.append(a)
    state["projects"].append({"name": name, "path": f"{H}/code/{name}", "agents": rows})
json.dump(state, open(f"{ST}/mad/state.json", "w"))
with open(f"{H}/.codex/session_index.jsonl", "w") as f:  # codex thread names
    f.write(json.dumps({"id": ID(3), "thread_name": "Rebuild the API binary"}) + "\n")
    f.write(json.dumps({"id": ID(5), "thread_name": "Terraform plan for staging"}) + "\n")

env = dict(HOME=H, XDG_CONFIG_HOME=f"{H}/.config", XDG_STATE_HOME=ST, MAD_SOCKET=DECK, REC=W,
           LANG="C.UTF-8", TERM="xterm-256color", PATH=f"{BIN}:{os.environ['PATH']}", **ident)
subprocess.run(["tmux", "-L", OUT, "-f", "/dev/null", "new-session", "-d", "-s", "rec", "-x", str(COLS), "-y", str(ROWS),
                "-c", f"{H}/code/api", "env " + " ".join(f"{k}='{v}'" for k, v in env.items()) + " mad"], check=True)
sh(f"tmux -L {OUT} set -g status off")
mad = lambda *a: subprocess.run([f"{BIN}/mad", *a], env={**os.environ, **env}, stderr=subprocess.DEVNULL)
time.sleep(2)
mad("switch", "4"); time.sleep(0.8)          # web: works 5s off stage, so it ends up "done"
for i in (2, 3):
    mad("switch", str(i)); time.sleep(0.6)
mad("switch", "5"); time.sleep(3.5)          # infra's old codex settles on stage: idle, not "done"
mad("switch", "1"); time.sleep(7)

frames, cap = [], {"text": "", "keys": ""}
stop, t0 = threading.Event(), time.time()
def grab():
    while not stop.is_set():
        o = subprocess.run(["tmux", "-L", OUT, "capture-pane", "-e", "-p", "-t", "rec"], capture_output=True, text=True).stdout
        frames.append([round(time.time() - t0, 3), o, cap["text"], cap["keys"]])
        time.sleep(0.1)
th = threading.Thread(target=grab); th.start()
def at(t):
    while time.time() - t0 < t:
        time.sleep(0.01)
def say(t, text, keys=""):
    at(t); cap["text"], cap["keys"] = text, keys
def key(t, *k):
    at(t); subprocess.run(["tmux", "-L", OUT, "send-keys", "-t", "rec", *k])
def typ(t, s, gap=0.09):
    for i, ch in enumerate(s):
        at(t + i * gap); subprocess.run(["tmux", "-L", OUT, "send-keys", "-t", "rec", "-l", ch])
def trig(t, name):
    at(t); open(f"{W}/trig/{name}", "w").close()

# The timeline. Captions and key badges show up under the terminal.
say(0.0, "Agents grouped by project, named after their conversation, with what each is doing")
say(4.5, "Tokens per agent and project · branch, ±changed files, ↑unpushed commits")
say(9.0, "Jump to the agent that needs you", "Alt-n");                   key(9.5, "M-n")
say(11.8, "Answer it right there", "1");                                 key(12.5, "1")
say(14.5, "Start an agent on a branch of its own (a git worktree)", "Alt-s  w")
key(14.8, "M-s"); key(15.4, "w"); typ(16.2, "fix-login"); key(17.3, "Enter")
say(17.9, "…pick the kind", "1");                                        key(18.6, "1")
say(20.0, "It works in its own checkout; the sidebar follows along")
trig(25.5, f"{ID(2)}-stop")
say(26.0, "Meanwhile another one finished in the background → ● done")
say(29.5, "See what the new agent changed", "Alt-s  v");                 key(29.8, "M-s"); key(30.4, "v")
say(34.0, "", "q");                                                      key(34.3, "q")
say(35.0, "Have it commit");                                             trig(35.2, "new-commit")
say(38.5, "Finish the branch: open a PR, rebase, merge or push", "f");   key(39.2, "f")
say(41.5, "…merge it into main", "2");                                   key(42.2, "2")
say(44.3, "", "⏎");                                                      key(44.8, "Enter")
say(45.6, "Merged: main is one more commit ahead of origin (↑)")
say(49.0, "One line per agent when the list gets long", "i");            key(49.7, "i")
say(51.8, "", "i");                                                      key(52.3, "i")
say(53.5, "mad — multi-agent deck")
at(57.0); stop.set(); th.join()

json.dump(frames, open(f"{W}/frames.json", "w"))
sh(f"tmux -L {OUT} kill-server; tmux -L {DECK} kill-server")
print(f"{len(frames)} frames over {frames[-1][0]:.1f}s → {W}/frames.json")
