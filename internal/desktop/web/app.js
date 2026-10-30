// mad desktop: the sidebar is drawn from /api/deck; each opened agent is
// its conversation, drawn from its transcript (/api/chat), or an xterm.js
// terminal on /api/term, a tmux client attached to its pane.
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
const opened = new Map(); // id → { term, fit, ws, box, tab, chat, log, view, ... }
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
    o.wait.hidden = a.status !== "waiting";
    // Asking you something: what it asks, and the keys to answer, are in
    // its own TUI.
    if (a.status === "waiting" && o.status !== "waiting" && id === current && o.view === "chat") setView(id, "term");
    o.status = a.status;
  }
  $("#bar").hidden = opened.size === 0;
  drawViews();
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
    const o = opened.get(current);
    if (o && o.view === "chat") {
      loadChat(current, o);
      try {
        drawLive(o); // what the agent is doing, as of this look
      } catch (err) {
        console.error("live", err);
      }
    }
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

  // The conversation, for kinds whose transcript reads as one. The
  // terminal connects when first shown: reading starts nothing.
  const chat = el("div", "chat");
  const log = el("div", "log");
  const live = el("div", "live"); // the message being written, last in the log
  log.append(live);
  const wait = el("div", "wait");
  wait.hidden = true;
  wait.append(el("span", "", "Waiting for you: answer in its terminal "), el("kbd", "", "Alt"), "+", el("kbd", "", "t"));
  wait.onclick = () => setView(id, "term");
  chat.append(log, wait);
  $("#terms").append(chat);

  const a = agents.get(id);
  const o = {
    id, term, fit, box, tab, ws: null, chat, log, live, wait,
    view: a && a.chat ? "chat" : "term",
    status: a && a.status,
    file: "", offset: 0, loaded: false, loading: false, entries: 0,
    calls: new Map(), // tool call id → its node, which its result joins
    parts: new Map(), // message id → its parts the transcript has (claude)
    partIDs: new Set(), // parts the transcript has, by their own id (codex)
    msg: null, version: 0, msgAt: 0, follow: null, // the message being written (/api/live)
  };
  opened.set(id, o);

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
    if (prev) {
      prev.box.classList.remove("on"); prev.chat.classList.remove("on"); prev.tab.classList.remove("on");
      sendCtl(prev, { type: "blur" });
      unfollow(prev);
    }
  }
  current = id;
  const o = opened.get(id) || open(id);
  $("#empty").style.display = "none";
  present(id, o);
  o.tab.scrollIntoView({ block: "nearest" });
  drawSidebar();
}

// present shows agent id's tab in its view: the conversation, or the
// terminal, connected the first time it is shown.
function present(id, o) {
  const term = o.view === "term";
  o.box.classList.toggle("on", term);
  o.chat.classList.toggle("on", !term);
  if (term) {
    unfollow(o);
    if (!o.ws && !o.box.querySelector(".msg")) connect(id, o);
    requestAnimationFrame(() => { safeFit(o); o.term.focus(); });
  } else {
    sendCtl(o, { type: "blur" });
    loadChat(id, o);
    follow(id, o);
  }
}

function setView(id, view) {
  const o = opened.get(id);
  if (!o || o.view === view || view === "chat" && !agents.get(id)?.chat) return;
  o.view = view;
  if (id === current) present(id, o);
  drawViews();
}

function drawViews() {
  const o = opened.get(current);
  $("#views").hidden = !o;
  if (!o) return;
  for (const b of document.querySelectorAll("#views button")) {
    b.classList.toggle("on", b.dataset.view === o.view);
    if (b.dataset.view === "chat") b.disabled = !agents.get(current)?.chat;
  }
}
for (const b of document.querySelectorAll("#views button")) b.onclick = () => setView(current, b.dataset.view);

function close(id) {
  const o = opened.get(id);
  if (!o) return;
  opened.delete(id);
  unfollow(o);
  o.ws?.close();
  o.term.dispose();
  o.box.remove();
  o.chat.remove();
  o.tab.remove();
  if (current === id) {
    current = null;
    const next = [...opened.keys()].pop();
    if (next) show(next); else $("#empty").style.display = "";
  }
  drawSidebar();
}

// ---- the conversation: what the agent's transcript says ----

// Links open in a window of their own, not in place of the deck's.
DOMPurify.addHook("afterSanitizeAttributes", (n) => {
  if (n.tagName === "A") { n.setAttribute("target", "_blank"); n.setAttribute("rel", "noopener noreferrer"); }
});

// md renders markdown an agent or a person wrote. It is sanitized: the
// page holds the token, and an agent writes what it read anywhere.
function md(text) {
  const div = el("div", "md");
  div.innerHTML = DOMPurify.sanitize(marked.parse(text, { gfm: true, breaks: true }));
  return div;
}

async function loadChat(id, o) {
  if (o.loading) return;
  o.loading = true;
  try {
    const qs = new URLSearchParams({ token, id, file: o.file, from: o.offset });
    const r = await fetch(`/api/chat?${qs}`);
    if (!r.ok) throw new Error(await r.text());
    const c = await r.json();
    if (c.reset) {
      o.log.replaceChildren(o.live);
      o.calls.clear();
      o.parts.clear();
      o.partIDs.clear();
      o.entries = 0;
      if (c.earlier) put(o, el("div", "note", "Earlier messages are in the transcript, not shown here."));
    }
    const atEnd = nearEnd(o);
    for (const e of c.entries) {
      try { addEntry(o, e); } catch (err) { console.error("entry", e, err); }
    }
    o.file = c.file;
    o.offset = c.offset;
    if (!o.loaded && !o.entries) {
      o.none = el("div", "note", "Nothing in its transcript yet.");
      put(o, o.none);
    }
    if (c.entries.length) drawLive(o);
    if (!o.loaded || atEnd && c.entries.length) o.chat.scrollTop = o.chat.scrollHeight;
    o.loaded = true;
  } catch (_) {
    // The next poll tries again.
  } finally {
    o.loading = false;
  }
}

// put adds a node to the log, before the message being written.
function put(o, node) {
  o.log.insertBefore(node, o.live);
}

function nearEnd(o) {
  return o.chat.scrollHeight - o.chat.scrollTop - o.chat.clientHeight < 60;
}

function addEntry(o, e) {
  if (e.msg) o.parts.set(e.msg, (o.parts.get(e.msg) || 0) + 1);
  if (e.part) o.partIDs.add(e.part);
  if (e.kind === "other" || e.kind === "text" && !(e.text || "").trim()) return;
  o.none?.remove();
  o.none = null;
  o.entries++;
  switch (e.kind) {
    case "user": {
      const m = el("div", "msg from-user");
      m.append(md(e.text));
      put(o, m);
      break;
    }
    case "text": {
      const m = el("div", "msg from-agent");
      m.append(md(e.text));
      put(o, m);
      break;
    }
    case "command":
      put(o, el("div", "cmd", e.label));
      break;
    case "tool": {
      const d = callNode(e.tool, e.label);
      if (e.input) d.append(el("pre", "input", e.input));
      put(o, d);
      if (e.id) o.calls.set(e.id, d);
      break;
    }
    case "result": {
      const out = el("pre", `output${e.error ? " err" : ""}`, e.text || "(no output)");
      const call = e.id && o.calls.get(e.id);
      if (call) {
        call.classList.add(e.error ? "failed" : "done");
        call.append(out);
      } else {
        put(o, out);
      }
      break;
    }
  }
}

function callNode(tool, label) {
  const d = el("details", "call");
  const s = el("summary");
  s.append(el("span", "st"), el("span", "tool", tool), el("span", "label", label || ""));
  d.append(s);
  return d;
}

// ---- the message being written, as it is written (/api/live) ----

// follow asks for the message agent id's model is writing, and again as
// soon as it changed, while the conversation is shown.
async function follow(id, o) {
  if (o.follow) return;
  const stop = new AbortController();
  o.follow = stop;
  while (!stop.signal.aborted) {
    try {
      const r = await fetch(`/api/live?${new URLSearchParams({ token, id, since: o.version })}`, { signal: stop.signal });
      if (r.status === 200) {
        const m = await r.json();
        if (m.version > o.version) {
          o.version = m.version;
          o.msg = m;
          o.msgAt = Date.now();
          drawLive(o);
        }
        continue;
      }
      if (r.status !== 204) await sleep(2000);
    } catch (_) {
      if (!stop.signal.aborted) await sleep(2000);
    }
  }
}

function unfollow(o) {
  o.follow?.abort();
  o.follow = null;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// drawLive shows the parts of the message being written that the
// transcript does not have yet; the transcript's take their place. A
// message ended a while ago is the transcript's whole. Then, while the
// agent works, what it is doing.
function drawLive(o) {
  const atEnd = nearEnd(o);
  const box = o.live;
  box.replaceChildren();
  const m = o.msg;
  const fresh = m && m.id && !(m.done && Date.now() - o.msgAt > 5000);
  let shown = false;
  if (fresh) {
    const had = o.parts.get(m.id) || 0;
    for (const [i, b] of m.blocks.entries()) {
      // The transcript's take its place: the first parts of the message,
      // or ones of the same id.
      if (i < had || b.id && (o.partIDs.has(b.id) || o.calls.has(b.id))) continue;
      if (b.kind === "text" && (b.text || "").trim()) {
        const n = el("div", `msg from-agent${b.done ? "" : " streaming"}`);
        n.append(md(b.text));
        box.append(n);
        shown = true;
      } else if (b.kind === "thinking" && !b.done) {
        const n = el("div", "thinking");
        n.append(el("span", "what", "Thinking"));
        if (b.text) n.append(el("span", "", b.text.slice(-240)));
        box.append(n);
        shown = true;
      } else if (b.kind === "tool") {
        const d = callNode(b.tool, hint(b.text));
        d.classList.add("pending");
        box.append(d);
        shown = true;
      }
    }
  }
  if (shown) {
    o.none?.remove();
    o.none = null;
  }
  // Working, and not only by the deck's late look: a message being
  // written, or a call that has not returned.
  const a = agents.get(o.id);
  const last = o.live.previousElementSibling;
  const busy = m && m.id && !m.done || last && last.matches("details.call:not(.done):not(.failed)");
  if (!shown && a && a.status === "running" && busy) {
    const n = el("div", "working");
    n.append(el("span", "st"), el("span", "", a.detail || "Working"));
    box.append(n);
  }
  if (atEnd) o.chat.scrollTop = o.chat.scrollHeight;
}

// hint picks what a call is pointed at out of its input as far as it
// came (JSON, cut off anywhere).
function hint(partial) {
  const m = /"(description|command|file_path|pattern|url|query|prompt)"\s*:\s*"((?:[^"\\]|\\.)*)/.exec(partial || "");
  if (!m) return "";
  try { return JSON.parse(`"${m[2].replace(/\\$/, "")}"`).split("\n")[0]; } catch (_) { return m[2]; }
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
  } else if (e.code === "KeyT" && current) {
    e.preventDefault(); e.stopPropagation();
    setView(current, opened.get(current)?.view === "chat" ? "term" : "chat");
    return;
  } else {
    return;
  }
  e.preventDefault();
  e.stopPropagation();
  if (target) show(target);
}, true);

window.addEventListener("focus", () => { const o = opened.get(current); if (o && o.view === "term") sendCtl(o, { type: "focus" }); });
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
