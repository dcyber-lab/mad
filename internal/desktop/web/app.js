// mad desktop: the sidebar is drawn from /api/deck; each opened agent is
// an xterm.js terminal on /api/term, a tmux client attached to its pane.
"use strict";

const token = new URLSearchParams(location.hash.slice(1)).get("token") || "";
const $ = (sel) => document.querySelector(sel);
const el = (tag, cls, text) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
};

let deck = { projects: [] };
const agents = new Map(); // id → snapshot entry, with project
const opened = new Map(); // id → { term, fit, ws, box, tab }
let current = null;

// ---- the sidebar ----

function label(a) {
  return a.name || a.title || `${a.kind} ${a.id.slice(0, 8)}`;
}

function shownStatus(a) {
  return a.done ? "done" : a.status;
}

function money(v) {
  if (!v) return "";
  return v < 10 ? `$${v.toFixed(2)}` : `$${Math.round(v)}`;
}

function drawQuota(q) {
  const box = $("#quota");
  box.replaceChildren();
  for (const [kind, w] of Object.entries(q || {})) {
    for (const [key, name] of [["five_hour", "5h"], ["seven_day", "wk"]]) {
      const win = w[key];
      if (!win || (!win.used && !win.reset_at)) continue;
      const row = el("div", "q");
      row.append(el("span", "", `${kind} ${name}`));
      const bar = el("div", "bar");
      const fill = el("i", win.used >= 100 ? "over" : win.used >= 80 ? "hot" : "");
      fill.style.width = `${Math.min(100, win.used)}%`;
      bar.append(fill);
      row.append(bar, el("span", "", `${Math.round(win.used)}%`));
      if (win.reset_at) row.title = `resets ${new Date(win.reset_at).toLocaleString()}`;
      box.append(row);
    }
  }
}

function drawSidebar() {
  const nav = $("#projects");
  nav.replaceChildren();
  agents.clear();
  if (!deck.projects.length) {
    const p = el("div", "none");
    p.innerHTML = "No projects yet. Run <code>mad</code> in a git repository, or <code>mad spawn</code>.";
    nav.append(p);
  }
  for (const p of deck.projects) {
    const box = el("div", "project");
    const head = el("div", "head");
    head.append(el("b", "", p.name));
    if (p.branch) head.append(el("span", "", p.branch));
    const cost = p.agents.reduce((s, a) => s + (a.cost || 0), 0);
    const git = [p.changed ? `±${p.changed}` : "", p.ahead ? `↑${p.ahead}` : ""].filter(Boolean).join(" ");
    head.append(el("span", "git", git), el("span", "cost", money(cost)));
    head.title = p.path;
    box.append(head);
    for (const a of p.agents) {
      a.project = p;
      agents.set(a.id, a);
      const st = shownStatus(a);
      const row = el("div", `agent s-${st}${a.id === current ? " sel" : ""}`);
      row.title = `${a.kind} · ${st}${a.role ? " · " + a.role : ""}\n${a.id}`;
      row.append(el("span", "dot"));
      const icon = el("span", "icon", a.icon || a.kind[0]);
      if (a.color) icon.style.color = /^#/.test(a.color) ? a.color : xtermColor(a.color);
      const name = el("span", "name", label(a));
      const q = a.queue;
      if (q && q.items && q.items.length) {
        const n = el("span", `qn${q.paused ? " paused" : ""}`, `⇢${q.items.length}`);
        n.title = q.paused ? "queued messages, paused" : "queued messages";
        name.append(n);
      }
      row.append(icon, name);
      row.append(el("span", "meta", st === "waiting" ? "needs you" : money(a.cost)));
      if (a.detail) row.append(el("span", "detail", a.detail));
      row.onclick = () => show(a.id);
      box.append(row);
    }
    nav.append(box);
  }
  drawQueue();
  for (const [id, o] of opened) {
    const a = agents.get(id);
    if (!a) continue;
    o.tab.className = `tab s-${shownStatus(a)}${id === current ? " on" : ""}`;
    o.tab.querySelector(".t").textContent = label(a);
  }
}

// ---- the queue of the agent shown: messages typed in when its turn ends ----

function drawQueue(fresh) {
  const box = $("#queue");
  const a = current && agents.get(current);
  box.hidden = !current;
  if (!a) return;
  const q = fresh || a.queue || { items: [] };
  if (fresh) a.queue = fresh;
  const items = q.items || [];
  const list = $("#qitems");
  const key = JSON.stringify([current, items.map((i) => i.id)]);
  if (list.dataset.key !== key) {
    list.dataset.key = key;
    list.replaceChildren();
    items.forEach((it, i) => {
      const li = el("li");
      li.title = it.text;
      const x = el("span", "x", "×");
      x.title = "drop it";
      x.onclick = () => queueCall("DELETE", { item: it.id });
      li.append(el("span", "n", String(i + 1)), el("span", "t", it.text.split("\n")[0]), x);
      list.append(li);
    });
  }
  $("#qerr").textContent = q.error && items.length ? `Not sent yet: ${q.error}` : "";
  const pause = $("#qpause");
  pause.textContent = q.paused ? "Resume" : "Pause";
  pause.classList.toggle("on", !!q.paused);
}

async function queueCall(method, params, body, path = "/api/queue") {
  const id = current;
  const qs = new URLSearchParams({ token, id, ...params });
  const r = await fetch(`${path}?${qs}`, { method, body });
  if (!r.ok) { $("#qerr").textContent = await r.text(); return false; }
  const q = await r.json();
  if (id === current) drawQueue(q);
  drawSidebar();
  return true;
}

$("#qinput").addEventListener("keydown", async (e) => {
  if (e.key !== "Enter" || e.shiftKey || e.isComposing) return;
  e.preventDefault();
  const t = e.target;
  const text = t.value;
  if (!text.trim() || !current) return;
  // Cleared at once, so what is typed next is a message of its own; put
  // back if it was not queued.
  t.value = "";
  t.style.height = "";
  if (!(await queueCall("POST", {}, text)) && !t.value) t.value = text;
});
$("#qinput").addEventListener("input", (e) => {
  e.target.style.height = "";
  e.target.style.height = `${Math.min(120, e.target.scrollHeight)}px`;
});
$("#qpause").onclick = () => {
  const q = agents.get(current)?.queue;
  queueCall("POST", { paused: q && q.paused ? "0" : "1" }, null, "/api/queue/pause");
};

// xterm-256 color number → css, for the kinds' icon colors.
function xtermColor(n) {
  n = Number(n);
  const base = ["#000", "#c33", "#3c3", "#cc3", "#36c", "#c3c", "#3cc", "#ccc",
    "#666", "#f66", "#6f6", "#ff6", "#69f", "#f6f", "#6ff", "#fff"];
  if (n < 16) return base[n];
  if (n >= 232) { const v = 8 + (n - 232) * 10; return `rgb(${v},${v},${v})`; }
  n -= 16;
  const c = (x) => (x ? 55 + x * 40 : 0);
  return `rgb(${c(Math.floor(n / 36))},${c(Math.floor(n / 6) % 6)},${c(n % 6)})`;
}

async function poll() {
  try {
    const r = await fetch(`/api/deck?token=${encodeURIComponent(token)}`);
    if (!r.ok) throw new Error(r.status);
    deck = await r.json();
    $("#conn").classList.add("ok");
    $("#conn").title = "connected";
    drawQuota(deck.quota);
    drawSidebar();
  } catch (e) {
    $("#conn").classList.remove("ok");
    $("#conn").title = `no connection to mad (${e.message || e})`;
  }
  setTimeout(poll, 1000);
}

// ---- terminals ----

function open(id) {
  const box = el("div", "term");
  $("#terms").append(box);
  const term = new Terminal({
    fontFamily: '"JetBrains Mono", "SF Mono", Menlo, "DejaVu Sans Mono", monospace',
    fontSize: 13,
    cursorBlink: true,
    scrollback: 0, // tmux keeps the history; the wheel scrolls it
    allowProposedApi: false,
    macOptionIsMeta: true,
    theme: { background: "#0f1115", foreground: "#d7dae0", cursor: "#d7dae0", selectionBackground: "#264f78" },
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(box);

  const tab = el("div", "tab");
  tab.append(el("span", "dot"), el("span", "t", label(agents.get(id) || { id, kind: "" })));
  const x = el("span", "x", "×");
  x.title = "close (the agent keeps running)";
  x.onclick = (e) => { e.stopPropagation(); close(id); };
  tab.append(x);
  tab.onclick = () => show(id);
  $("#tabs").append(tab);

  const o = { term, fit, box, tab, ws: null };
  opened.set(id, o);
  connect(id, o);

  term.onData((d) => send(o, new TextEncoder().encode(d)));
  term.onBinary((d) => send(o, Uint8Array.from(d, (c) => c.charCodeAt(0))));
  term.onResize(({ cols, rows }) => sendCtl(o, { type: "resize", cols, rows }));
  term.textarea.addEventListener("focus", () => sendCtl(o, { type: "focus" }));
  term.textarea.addEventListener("blur", () => sendCtl(o, { type: "blur" }));
  new ResizeObserver(() => { if (box.classList.contains("on")) safeFit(o); }).observe(box);
  return o;
}

function connect(id, o) {
  safeFit(o);
  const q = new URLSearchParams({ token, id, cols: o.term.cols, rows: o.term.rows });
  const ws = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/term?${q}`);
  ws.binaryType = "arraybuffer";
  o.ws = ws;
  ws.onopen = () => {
    // The size in the URL can be from before the tab was shown (xterm's
    // 80x24), and a fit while it connected was sent nowhere.
    safeFit(o);
    sendCtl(o, { type: "resize", cols: o.term.cols, rows: o.term.rows });
    if (id === current && document.hasFocus()) sendCtl(o, { type: "focus" });
  };
  ws.onmessage = (ev) => {
    if (typeof ev.data === "string") {
      const m = JSON.parse(ev.data);
      if (m.type === "error") message(o, `Could not open the agent: ${m.message}`);
      return;
    }
    o.term.write(new Uint8Array(ev.data));
  };
  ws.onclose = () => {
    if (!opened.has(id) || o.ws !== ws) return;
    o.ws = null;
    if (!o.box.querySelector(".msg")) message(o, "Closed: the terminal deck took this agent, or mad stopped. Click to open it here again.", () => connect(id, o));
  };
}

function message(o, text, onclick) {
  o.box.querySelector(".msg")?.remove();
  const m = el("div", "msg", text);
  if (onclick) { m.style.cursor = "pointer"; m.onclick = () => { m.remove(); onclick(); }; }
  o.box.append(m);
}

function send(o, bytes) {
  if (o.ws && o.ws.readyState === WebSocket.OPEN) o.ws.send(bytes);
}
function sendCtl(o, m) {
  if (o.ws && o.ws.readyState === WebSocket.OPEN) o.ws.send(JSON.stringify(m));
}
function safeFit(o) {
  try { o.fit.fit(); } catch (_) { /* hidden: measured when shown */ }
}

function show(id) {
  if (current && current !== id) {
    const prev = opened.get(current);
    if (prev) { prev.box.classList.remove("on"); prev.tab.classList.remove("on"); sendCtl(prev, { type: "blur" }); }
  }
  current = id;
  const o = opened.get(id) || open(id);
  o.box.classList.add("on");
  $("#empty").style.display = "none";
  requestAnimationFrame(() => { safeFit(o); o.term.focus(); });
  o.tab.scrollIntoView({ block: "nearest" });
  drawSidebar();
}

function close(id) {
  const o = opened.get(id);
  if (!o) return;
  opened.delete(id);
  o.ws?.close();
  o.term.dispose();
  o.box.remove();
  o.tab.remove();
  if (current === id) {
    current = null;
    const next = [...opened.keys()].pop();
    if (next) show(next); else $("#empty").style.display = "";
  }
  drawSidebar();
}

// ---- keys: the deck's own, so hands don't have to learn new ones ----

function order() {
  return deck.projects.flatMap((p) => p.agents.map((a) => a.id));
}

window.addEventListener("keydown", (e) => {
  if (!e.altKey || e.ctrlKey || e.metaKey) return;
  const ids = order();
  let target = null;
  if (e.code === "KeyN") {
    // Next agent that needs you: waiting first, then done.
    const start = Math.max(0, ids.indexOf(current) + 1);
    const ring = ids.slice(start).concat(ids.slice(0, start));
    target = ring.find((id) => agents.get(id)?.status === "waiting") || ring.find((id) => agents.get(id)?.done);
  } else if (e.code === "KeyJ" || e.code === "KeyK") {
    const i = ids.indexOf(current);
    target = ids[(i + (e.code === "KeyJ" ? 1 : ids.length - 1)) % ids.length];
  } else if (/^Digit[1-9]$/.test(e.code)) {
    target = [...opened.keys()][Number(e.code.slice(5)) - 1];
  } else if (e.code === "KeyW" && current) {
    e.preventDefault(); e.stopPropagation();
    close(current);
    return;
  } else {
    return;
  }
  e.preventDefault();
  e.stopPropagation();
  if (target) show(target);
}, true);

window.addEventListener("focus", () => { const o = opened.get(current); if (o) sendCtl(o, { type: "focus" }); });
window.addEventListener("blur", () => { const o = opened.get(current); if (o) sendCtl(o, { type: "blur" }); });

// ---- sidebar width ----

(() => {
  const r = $("#resizer");
  const saved = Number(localStorageGet("mad.sidebar"));
  if (saved) document.documentElement.style.setProperty("--sidebar-w", `${saved}px`);
  r.addEventListener("mousedown", (e) => {
    e.preventDefault();
    r.classList.add("drag");
    const move = (ev) => document.documentElement.style.setProperty("--sidebar-w", `${Math.max(200, Math.min(600, ev.clientX))}px`);
    const up = (ev) => {
      r.classList.remove("drag");
      window.removeEventListener("mousemove", move);
      window.removeEventListener("mouseup", up);
      localStorageSet("mad.sidebar", Math.max(200, Math.min(600, ev.clientX)));
    };
    window.addEventListener("mousemove", move);
    window.addEventListener("mouseup", up);
  });
})();

function localStorageGet(k) { try { return localStorage.getItem(k); } catch (_) { return null; } }
function localStorageSet(k, v) { try { localStorage.setItem(k, v); } catch (_) { /* private window */ } }

poll();
