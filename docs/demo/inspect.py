#!/usr/bin/env python3
"""Check a recording before and after rendering.

    inspect.py text WORKDIR T [T...]      the screen as text at those seconds
    inspect.py sheet GIF OUT.png T [T...] a contact sheet of the GIF at those seconds

`text` reads WORKDIR/frames.json (record.py) and prints the capture nearest
each time with its caption and keys: the quick way to see that every step of
the timeline did what it should. `sheet` needs Pillow.
"""
import json, re, sys

def text(workdir, times):
    frames = json.load(open(f"{workdir}/frames.json"))
    for t in times:
        at, screen, caption, keys = min(frames, key=lambda f: abs(f[0] - t))
        print(f"=== t={at:.1f}s  keys={keys!r}  caption={caption!r}")
        print(re.sub(r"\x1b\[[0-9;:]*m", "", screen).rstrip())

def sheet(gif, out, times, cols=3, scale=0.32):
    from PIL import Image
    im = Image.open(gif)
    starts, t = [], 0.0  # when each frame starts, in seconds
    for i in range(im.n_frames):
        im.seek(i); starts.append(t); t += im.info.get("duration", 100) / 1000
    shots = []
    for want in times:
        i = max(k for k, s in enumerate(starts) if s <= want) if want >= 0 else 0
        im.seek(i); shots.append(im.convert("RGB"))
    w, h = int(shots[0].width * scale), int(shots[0].height * scale)
    rows = (len(shots) + cols - 1) // cols
    board = Image.new("RGB", (cols * (w + 10) + 10, rows * (h + 10) + 10), (11, 12, 16))
    for n, s in enumerate(shots):
        board.paste(s.resize((w, h), Image.LANCZOS), (10 + n % cols * (w + 10), 10 + n // cols * (h + 10)))
    board.save(out)
    print(f"{len(shots)} frames → {out}")

if __name__ == "__main__":
    if len(sys.argv) < 4 or sys.argv[1] not in ("text", "sheet"):
        sys.exit(__doc__)
    if sys.argv[1] == "text":
        text(sys.argv[2], [float(x) for x in sys.argv[3:]])
    else:
        sheet(sys.argv[2], sys.argv[3], [float(x) for x in sys.argv[4:]])
