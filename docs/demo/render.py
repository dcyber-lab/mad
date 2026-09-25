#!/usr/bin/env python3
"""Turn WORKDIR/frames.json (from record.py) into docs/demo.gif.

Each captured screen is converted from ANSI to HTML, drawn in a window with
a caption bar by Chromium, and the frames are put together as a GIF with
one shared palette, so colours don't shimmer.

    pip install playwright pillow && playwright install chromium
    python3 docs/demo/render.py WORKDIR [OUT.gif]

CHROMIUM=/path/to/chrome uses that browser instead of Playwright's.
"""
import html, json, os, re, sys, tempfile
from PIL import Image
from playwright.sync_api import sync_playwright

HERE = os.path.dirname(os.path.abspath(__file__))
W = sys.argv[1]
OUT = sys.argv[2] if len(sys.argv) > 2 else os.path.join(os.path.dirname(HERE), "demo.gif")
COLS, ROWS, SCALE = 112, 26, 1.5

# xterm's 16 colours as a dark theme draws them, then the 256-colour cube.
BASE = ["#1a1b26", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#a9b1d6",
        "#414868", "#ff7a93", "#b9f27c", "#ff9e64", "#7da6ff", "#c0a8ff", "#0db9d7", "#c0caf5"]
FG, BG = "#c0caf5", "#16161e"
def c256(n):
    if n < 16:
        return BASE[n]
    if n < 232:
        n -= 16; lv = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (lv[n // 36], lv[n // 6 % 6], lv[n % 6])
    v = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (v, v, v)

def ansi_html(text):
    """SGR colours and attributes of a `tmux capture-pane -e` dump → spans."""
    out = []
    for line in text.rstrip("\n").split("\n"):
        st = dict(fg=None, bg=None, b=False, d=False, i=False, u=False, r=False)
        spans = []
        for part in re.split(r"(\x1b\[[0-9;:]*m)", line):
            if part.startswith("\x1b["):
                ps = [int(x) if x else 0 for x in re.split("[;:]", part[2:-1] or "0")]
                k = 0
                while k < len(ps):
                    c = ps[k]
                    if c == 0: st.update(fg=None, bg=None, b=False, d=False, i=False, u=False, r=False)
                    elif c in (1, 2, 3, 4, 7): st[{1: "b", 2: "d", 3: "i", 4: "u", 7: "r"}[c]] = True
                    elif c == 22: st["b"] = st["d"] = False
                    elif c in (23, 24, 27): st[{23: "i", 24: "u", 27: "r"}[c]] = False
                    elif 30 <= c <= 37: st["fg"] = BASE[c - 30]
                    elif 90 <= c <= 97: st["fg"] = BASE[c - 82]
                    elif 40 <= c <= 47: st["bg"] = BASE[c - 40]
                    elif 100 <= c <= 107: st["bg"] = BASE[c - 92]
                    elif c == 39: st["fg"] = None
                    elif c == 49: st["bg"] = None
                    elif c in (38, 48) and k + 1 < len(ps):
                        slot = "fg" if c == 38 else "bg"
                        if ps[k + 1] == 5: st[slot] = c256(ps[k + 2]); k += 2
                        elif ps[k + 1] == 2: st[slot] = "#%02x%02x%02x" % tuple(ps[k + 2:k + 5]); k += 4
                    k += 1
                continue
            if not part:
                continue
            fg, bg = st["fg"] or FG, st["bg"]
            if st["r"]:
                fg, bg = st["bg"] or BG, st["fg"] or FG
            css = [f"color:{fg}"] + ([f"background:{bg}"] if bg else [])
            css += ["font-weight:700"] * st["b"] + ["opacity:.55"] * st["d"] + ["font-style:italic"] * st["i"]
            css += ["text-decoration:underline"] * st["u"]
            spans.append(f'<span style="{";".join(css)}">{html.escape(part)}</span>')
        out.append("".join(spans))
    return "\n".join(out)

PAGE = f"""<!doctype html><meta charset=utf-8><style>
body{{margin:0;background:#0b0c10;display:inline-block;padding:18px}}
.win{{border-radius:10px;overflow:hidden;border:1px solid #2a2b36;display:inline-block;background:{BG}}}
.bar{{height:28px;background:#20212c;display:flex;align-items:center;padding:0 12px;gap:7px;font:12px -apple-system,Helvetica,sans-serif;color:#8b8fa7}}
.dot{{width:11px;height:11px;border-radius:50%}} .t{{flex:1;text-align:center;margin-right:48px}}
pre{{margin:0;padding:8px 10px;background:{BG};color:{FG};font:13px/1.3 'DejaVu Sans Mono',Menlo,monospace;white-space:pre;
     height:{ROWS * 1.3}em;width:{COLS}ch;overflow:hidden}}
.cap{{height:46px;display:flex;align-items:center;gap:10px;padding:0 16px;background:#16171f;border-top:1px solid #2a2b36;
      font:15px 'DejaVu Sans',Helvetica,sans-serif;color:#c0caf5}}
kbd{{font:600 13px 'DejaVu Sans Mono',Menlo,monospace;color:#e6e9ff;background:#2b2e42;border:1px solid #4a4f6e;
     border-bottom-width:3px;border-radius:6px;padding:3px 9px;min-width:14px;text-align:center}}
.k{{display:flex;gap:6px}}
</style><div class=win><div class=bar><span class=dot style="background:#ff5f57"></span>
<span class=dot style="background:#febc2e"></span><span class=dot style="background:#28c840"></span><span class=t>mad</span></div>
<pre id=term></pre><div class=cap><span class=k id=keys></span><span id=cap></span></div></div>"""

frames = json.load(open(f"{W}/frames.json"))
shots = []  # [term html, keys html, caption, duration ms]; identical neighbours merged
for i, (t, ansi, cap, keys) in enumerate(frames):
    nxt = frames[i + 1][0] if i + 1 < len(frames) else t + 2.5  # hold the last frame
    if shots and shots[-1][4] == (ansi, cap, keys):
        shots[-1][3] += (nxt - t) * 1000
        continue
    kbd = "".join(f"<kbd>{html.escape(k)}</kbd>" for k in keys.split("  ") if k)
    shots.append([ansi_html(ansi), kbd, html.escape(cap), (nxt - t) * 1000, (ansi, cap, keys)])

imgs = []
with tempfile.TemporaryDirectory() as tmp, sync_playwright() as pw:
    page_file = os.path.join(tmp, "page.html")
    open(page_file, "w").write(PAGE)
    browser = pw.chromium.launch(executable_path=os.environ.get("CHROMIUM") or None)
    page = browser.new_page(device_scale_factor=SCALE, viewport={"width": 1600, "height": 1200})
    page.goto("file://" + page_file)
    win = page.locator(".win")
    for n, (term, kbd, cap, _, _) in enumerate(shots):
        page.evaluate("([t, k, c]) => { term.innerHTML = t; keys.innerHTML = k; cap.innerHTML = c }", [term, kbd, cap])
        path = os.path.join(tmp, f"{n:04d}.png")
        win.screenshot(path=path)
        imgs.append(Image.open(path).convert("RGB"))
    browser.close()

w, h = imgs[0].size
sample = Image.new("RGB", (w, h * 6))
for j, k in enumerate(range(0, len(imgs), max(1, len(imgs) // 6))[:6]):
    sample.paste(imgs[k], (0, j * h))
palette = sample.quantize(colors=255, method=Image.Quantize.MEDIANCUT)
out = [im.quantize(palette=palette, dither=Image.Dither.NONE) for im in imgs]
durations = [max(20, round(s[3] / 10) * 10) for s in shots]
out[0].save(OUT, save_all=True, append_images=out[1:], duration=durations, loop=0, disposal=1)
print(f"{len(frames)} captures → {len(shots)} frames, {w}x{h}, {sum(durations) / 1000:.1f}s, "
      f"{os.path.getsize(OUT) / 1e6:.2f} MB → {OUT}")
