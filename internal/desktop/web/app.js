// mad desktop: the sidebar is drawn from /api/deck; each opened agent is
// its conversation, drawn from its transcript (/api/chat) and, run here,
// from its host (/api/host: window mode), or followed as its TUI writes
// (/api/live).
"use strict";

// The token: in the URL's fragment, else the one kept from it (a phone's
// home screen opens the page without it). Out of sight once read.
const token = (() => {
  const t = new URLSearchParams(location.hash.slice(1)).get("token");
  if (!t) return localStorageGet("mad.token") || "";
  localStorageSet("mad.token", t);
  history.replaceState(null, "", location.pathname + location.search);
  return t;
})();
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

// looks is the state table the deck sent (status.Looks): every status
// word, glyph and tone is read from it, by shown state or by the raw
// status folded into one (exited reads as stopped).
let looks = new Map();
function look(state) {
  return looks.get(state) || { state, word: state, meaning: "", glyph: "", tone: "quiet" };
}

// shownStatus is the state drawn for agent a on every surface: its tab, its
// row, its avatar and the composer header. The snapshot's shown state
// (Finished, Stopped, ...) is what the deck and the TUI draw; a window-mode
// host overrides it only for what it alone knows live: working, waiting,
// starting, or idle. Its "off" (no host running) says nothing the snapshot
// does not, and an idle host does not hide a finished run not yet seen.
function shownStatus(a) {
  const o = opened.get(a.id);
  const hs = windowed(a) && o && o.hs;
  if (!hs) return a.shown;
  const s = look(hs.status).state;
  if (s === "idle") return a.shown === "finished" ? "finished" : s;
  return s === "running" || s === "waiting" || s === "starting" ? s : a.shown;
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

// avatar is agent a's kind as a tile in its color, with its status as a
// dot on the corner. It carries the status itself: a card or a message
// has no row around it to color the dot.
function avatar(a, small) {
  const glyph = a.icon || (a.kind || "?")[0];
  const live = a.id && agents.get(a.id); // a message's sender is not the snapshot's entry
  const st = live ? ` s-${shownStatus(live)}` : "";
  const v = el("span", `avatar${small ? " small" : ""}${[...glyph].length > 1 ? " wide" : ""}${st}`, glyph);
  if (a.color) v.style.setProperty("--kc", /^#/.test(a.color) ? a.color : xtermColor(a.color));
  v.append(el("span", "dot"));
  return v;
}

function statusPill(a) {
  const l = look(shownStatus(a));
  const p = el("span", `pill ${l.tone}`, l.word);
  p.title = a.status === "exited" ? `${l.meaning} (its process ended)` : l.meaning;
  return p;
}

// Projects folded away, by path.
const collapsed = new Set((() => { try { return JSON.parse(localStorageGet("mad.collapsed") || "[]"); } catch (_) { return []; } })());

// drawSidebar draws the list in attention order: what needs you (the
// inbox), what works, what finished, the channels, then the projects with
// the agents that are quiet. An agent is drawn once; a project's head
// rolls up all of its agents.
function drawSidebar() {
  keepFocus(() => {
    drawList();
    drawHome();
  });
  drawQueue();
  for (const [id, o] of opened) {
    const a = agents.get(id);
    if (!a) continue;
    o.tab.className = `tab s-${shownStatus(a)}${id === current ? " on" : ""}`;
    o.tab.querySelector(".t").textContent = label(a);
    if (windowed(a)) {
      // Drawn from its host: what it asks is a card in the conversation.
      if (!o.hostWS) hostConnect(id, o);
      if (o.follow) unfollow(o);
    } else if (a.chat) {
      // Back in its TUI; not one just put here that the deck's look has
      // yet to show.
      if ((o.hostWS || o.hs) && Date.now() - o.claimAt > 3000) hostLeave(o);
      if (id === current) {
        claim(id, o);
        if (!o.follow) follow(id, o);
      }
    }
    drawWait(o, a);
    o.status = a.status;
  }
  $("#bar").hidden = opened.size === 0;
  drawActs();
  if (nc.open && !projectOf(nc.project) && deck.projects.length) {
    nc.project = deck.projects[0].path;
    drawNew();
  }
}

// drawList is the list itself: groups in attention order (see drawSidebar).
function drawList() {
  const nav = $("#projects");
  const keep = nav.scrollTop;
  agents.clear();
  let count = 0;
  for (const p of deck.projects) {
    for (const a of p.agents) {
      a.project = p;
      agents.set(a.id, a);
      count++;
    }
  }
  pruneGone();
  settleLayout();
  const q = $("#filter").value.trim().toLowerCase();
  const has = (...s) => !q || s.some((x) => (x || "").toLowerCase().includes(q));
  const agentHas = (a) => a && has(label(a), a.title, a.kind, a.tool, a.target, a.project.name);
  const needs = needsYou(), fin = finishedItems();
  const work = (deck.working || []).map((id) => agents.get(id)).filter(Boolean);
  // Drawn above, so not again in their projects: what works, what
  // finished, and what is blocked on you (its row is its item).
  const placed = new Set([...work.map((a) => a.id), ...fin.map((it) => it.agent)]);
  for (const it of needs) if (it.blocks && it.agent) placed.add(it.agent);

  const nodes = [];
  drawChips(needs.length, work.length, fin.length, count);
  if (!deck.projects.length) {
    const p = el("div", "none");
    p.innerHTML = "No projects yet. Run <code>mad</code> in a git repository, or <code>mad spawn</code>.";
    nodes.push(p);
  }

  const shownNeeds = needs.filter((it) => !q || has(it.title, it.context, it.text) || agentHas(agents.get(it.agent)));
  if (shownNeeds.length) {
    const next = button("Next", "ghost ga nexthead", nextItem);
    next.append(el("kbd", "", "⌥N"));
    next.title = "open the next thing that needs you (Alt-N)";
    // On a phone the inbox folds earlier, so Working shows on the first screen.
    const [limit, keep] = phoneList() ? [4, 3] : [8, 6];
    nodes.push(group("inbox g-needs", ghead("Needs you", shownNeeds.length, "needs", next), capped("needs", shownNeeds, limit, keep, (it) => {
      const r = itemRow(it);
      return ib.reply?.key === it.key && ib.reply.where === "side" ? [r, replyBox(it, ib.reply)].filter(Boolean) : r;
    }, "need you", (it) => ib.reply?.key === it.key)));
  }
  const shownWork = work.filter(agentHas);
  if (shownWork.length) {
    nodes.push(group("g-working", ghead("Working", shownWork.length, "working"), capped("working", shownWork, 5, 5, agentRow, "working")));
  }
  const shownFin = fin.filter((it) => agentHas(agents.get(it.agent)));
  if (shownFin.length) {
    const all = seenActs().length ? button("Mark all seen", "ghost ga", markAllSeen) : null;
    nodes.push(group("g-finished", ghead("Finished", shownFin.length, "finished", all),
      capped("finished", shownFin, 5, 5, (it) => agentRow(agents.get(it.agent), it.key), "finished")));
  }
  nodes.push(channelsGroup(q));

  for (const p of deck.projects) {
    const rest = p.agents.filter((a) => !placed.has(a.id) && agentHas(a));
    const teams = (deck.teams || []).filter((t) => t.project === p.name && (!q || t.members.some((id) => rest.some((a) => a.id === id))));
    if (q && !rest.length) continue;
    const folded = !q && collapsed.has(p.path);
    const box = el("div", `project${folded ? " collapsed" : ""}`);
    box.append(projectHead(p, rest.length));
    if (!folded) {
      const inTeam = new Set();
      for (const t of teams) {
        const mem = rest.filter((a) => t.members.includes(a.id));
        if (!mem.length) continue; // a head with nobody under it would claim the rows below it
        mem.forEach((a) => inTeam.add(a.id));
        box.append(teamHead(t));
        box.append(...mem.map((a) => agentRow(a)));
      }
      box.append(...rest.filter((a) => !inTeam.has(a.id)).map((a) => agentRow(a)));
    }
    nodes.push(box);
  }
  nav.replaceChildren(...nodes);
  nav.scrollTop = keep;
  $("#summary").textContent = `${count} agent${count === 1 ? "" : "s"}`;
}

// drawChips: the counts of the three groups above, as they are there; a
// click goes to its group. On a phone the footer's Next says them too.
function drawChips(nNeeds, nWork, nFin, count) {
  const box = $("#chips");
  box.hidden = !count;
  const jump = (k) => () => document.querySelector(`#projects .g-${k}`)?.scrollIntoView({ block: "start", behavior: "smooth" });
  const chip = (tone, n, text, k) => {
    const b = el("button", `cchip ${tone}${n ? "" : " zero"}`);
    b.type = "button";
    b.onclick = jump(k);
    b.append(glyph(tone), el("b", "", String(n)), ` ${text}`);
    return b;
  };
  box.replaceChildren(chip("needs", nNeeds, "need you", "needs"), chip("working", nWork, "working", "working"), chip("finished", nFin, "finished", "finished"));
  const next = $("#next");
  const n = nNeeds || nFin;
  next.hidden = !n;
  next.replaceChildren(icon("right"), `Next · ${n} ${nNeeds ? "need you" : "finished"}`);
  next.classList.toggle("needs", !!nNeeds);
}

function phoneList() {
  return window.matchMedia("(max-width: 640px)").matches;
}

function group(cls, head, rows) {
  const g = el("section", `group ${cls}`);
  g.append(head, ...rows.flat());
  return g;
}

function ghead(text, n, tone, extra) {
  const h = el("div", "ghead");
  h.append(glyph(tone), el("span", "", text), el("span", "n", String(n)));
  if (extra) h.append(extra);
  return h;
}

// capped draws list, folded past limit to its first keep and a row that
// unfolds the rest: what is long never pushes the groups below it away.
// A row that must stay (must(x): its reply box is open) is never folded.
function capped(name, list, limit, keep, draw, noun, must) {
  const all = ib.more[name] || list.length <= limit || !!$("#filter").value.trim();
  const shown = all ? list : list.filter((x, i) => i < keep || (must && must(x)));
  const out = shown.map((x) => draw(x));
  if (list.length > limit && !$("#filter").value.trim()) {
    const r = row({
      cls: "one fold", lead: el("span", "chglyph", all ? "−" : "+"),
      name: all ? "Show fewer" : `${list.length - shown.length} more ${noun}`,
      onclick: () => { ib.more[name] = !all; drawSidebar(); },
    });
    out.push(r);
  }
  return out;
}

// agentRow is an agent as a row: its avatar with its live dot, its name,
// and its activity line; its team or role, else its cost, on the right.
function agentRow(a, key) {
  const st = shownStatus(a);
  const name = el("span", "");
  const qu = a.queue;
  if (qu && qu.items && qu.items.length) {
    const n = el("span", `qn${qu.paused ? " paused" : ""}`, String(qu.items.length));
    n.title = qu.paused ? "queued messages, paused" : "queued messages";
    name.append(n);
  }
  const right = [];
  if (a.role) right.push(el("span", "tag", a.role));
  else if (st !== "running" && a.cost) right.push(el("span", "meta", money(a.cost)));
  const r = row({
    key: key || `g:${a.id}`, cls: `agent s-${st}${look(st).tone === "quiet" ? " quiet" : ""}${a.id === current ? " sel" : ""}`,
    lead: avatar(a), name: label(a), ctx: a.team || "", after: name.childNodes.length ? name : null,
    line: activityLine(a), right, onclick: () => show(a.id),
    title: `${a.kind} · ${look(st).word}${a.role ? " · " + a.role : ""}\n${a.id}`,
  });
  r.dataset.agent = a.id;
  r.oncontextmenu = (e) => { e.preventDefault(); agentMenu(a, { x: e.clientX, y: e.clientY }); };
  const more = el("button", "icon-btn more");
  more.type = "button";
  more.title = "more";
  more.append(icon("more"));
  more.onclick = (e) => { e.stopPropagation(); agentMenu(a, more, { right: true }); };
  r.append(more);
  return r;
}

// projectHead: name, branch, how its agents stand; its + and ... on hover.
function projectHead(p, quiet) {
  const head = el("div", "head");
  head.tabIndex = 0;
  head.append(icon("down", "chev"), el("b", "", p.name));
  if (p.branch) {
    const b = el("span", "branch");
    b.append(icon("branch"), p.branch);
    head.append(b);
  }
  head.append(el("span", "grow"), rollupNode(p.rollup));
  const cost = p.agents.reduce((s, a) => s + (a.cost || 0), 0);
  const git = [p.changed ? `±${p.changed}` : "", p.ahead ? `↑${p.ahead}` : ""].filter(Boolean).join(" ");
  head.title = [p.path, git, money(cost)].filter(Boolean).join(" · ");
  const toggle = () => {
    if (collapsed.has(p.path)) collapsed.delete(p.path); else collapsed.add(p.path);
    localStorageSet("mad.collapsed", JSON.stringify([...collapsed]));
    drawSidebar();
  };
  head.onclick = toggle;
  head.onkeydown = (e) => { if (e.target === head && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); toggle(); } };
  head.oncontextmenu = (e) => { e.preventDefault(); projectMenu(p, { x: e.clientX, y: e.clientY }); };
  const hacts = el("span", "hacts");
  const plus = el("button", "icon-btn");
  plus.type = "button";
  plus.title = `a new chat in ${p.name}`;
  plus.append(icon("plus"));
  plus.onclick = (e) => { e.stopPropagation(); openNew(p.path); };
  const pmore = el("button", "icon-btn");
  pmore.type = "button";
  pmore.title = "more";
  pmore.append(icon("more"));
  pmore.onclick = (e) => { e.stopPropagation(); projectMenu(p, pmore); };
  hacts.append(plus, pmore);
  head.append(hacts);
  return head;
}

// teamHead is a team in its project: its name and how its members stand.
function teamHead(t) {
  const ch = (deck.bus || []).find((c) => c.team && c.team.name === t.name);
  const h = el("div", `teamhead${ch ? " link" : ""}`);
  h.append(icon("users"), el("b", "", t.name), el("span", "grow"), rollupNode(t.rollup));
  if (ch) {
    h.title = `${ch.title}: its channel`;
    h.onclick = () => openChannel(ch.id);
  }
  return h;
}

// goHome shows the overview: nothing open.
function goHome() {
  if (current) {
    const o = opened.get(current);
    if (o) {
      o.chat.classList.remove("on");
      o.tab.classList.remove("on");
      unfollow(o);
    }
    current = null;
  }
  closeNew();
  closeChannel();
  closeSkills();
  closeBoard();
  closeChanges();
  closeMenu();
  closePopup();
  $("#empty").style.display = "";
  inMain(false);
  drawSidebar();
}
$("#brand").onclick = goHome;
$("#boardbtn").onclick = () => (bd.open ? goHome() : openBoard());
$("#next").onclick = nextItem;
$("#searchbtn").onclick = () => {
  document.body.classList.toggle("searching");
  $("#filter").focus();
};

// drawWait says, over an agent its TUI has in the terminal deck, why it
// is not run here yet: it comes here once that is done.
function drawWait(o, a) {
  const busy = !windowed(a) && a.chat && a.headless && (a.status === "running" || a.status === "waiting");
  o.wait.hidden = !busy;
  if (!busy) return;
  const text = a.status === "waiting"
    ? "It asks something in the terminal deck: answer it there. It comes here once done."
    : "It works in the terminal deck: it comes here once done.";
  if (o.wait.dataset.text !== text) {
    o.wait.dataset.text = text;
    o.wait.className = `wait${a.status === "waiting" ? " asks" : ""}`;
    o.wait.replaceChildren(icon(a.status === "waiting" ? "alert" : "terminal"), el("span", "", text));
  }
}

// drawHome is the page shown with nothing open: an overview of what needs
// you (cards, to act on in place), what works and what finished. With no
// agents at all it is the hero.
function drawHome() {
  const root = $("#empty");
  if (root.style.display === "none") return $("#home").replaceChildren(); // hidden: no box of its own to take the focus
  const none = !agents.size;
  root.classList.toggle("overview", !none);
  $("#empty .hero").hidden = !none;
  $("#empty > .keys").hidden = !none;
  const box = $("#home");
  box.hidden = none;
  if (none) return box.replaceChildren();
  const needs = needsYou(), fin = finishedItems();
  const work = (deck.working || []).map((id) => agents.get(id)).filter(Boolean);
  const h = el("div", "home");
  const head = el("div", "hhead");
  head.append(el("h1", "", "Overview"));
  const sub = el("span", "hsub");
  sub.append(`${agents.size} agents in ${deck.projects.length} project${deck.projects.length === 1 ? "" : "s"} · `,
    el("b", "tone-needs", `${needs.length} need you`), " · ", el("b", "tone-working", `${work.length} working`), " · ", el("b", "tone-finished", `${fin.length} finished`));
  head.append(sub);
  const spend = spendMeter(deck.spend);
  if (spend) head.append(spend);
  h.append(head);

  const sect = (title, tone, n, extra, body) => {
    const s = el("div", "sect");
    const sh = el("div", `shead ${tone}`);
    sh.append(glyph(tone), title, el("span", "n", String(n)));
    if (extra) {
      const sa = el("span", "sa");
      sa.append(extra);
      sh.append(sa);
    }
    s.append(sh, body);
    return s;
  };
  const nothing = (t) => { const c = el("div", "icard idle"); c.append(el("div", "body muted", t)); return c; };
  const grid = (cls, cards) => { const g = el("div", cls); g.append(...cards); return g; };
  const next = button("Next", "sm", nextItem);
  next.append(el("kbd", "", "⌥N"));
  h.append(sect("Needs you", "needs", needs.length, needs.length ? next : null,
    needs.length ? grid("cards", needs.map((it) => itemCard(it))) : nothing("Nothing needs you.")));
  // the longest at work first; the sidebar has the rest
  const moreBtn = (name, n, shown) => n > shown ? button(ib.more[name] ? "Fewer" : `${n - shown} more`, "sm ghost", () => { ib.more[name] = !ib.more[name]; drawSidebar(); }) : null;
  h.append(sect("Working", "working", work.length, moreBtn("homeWork", work.length, 3),
    work.length ? grid("cards", work.slice(0, ib.more.homeWork ? 99 : 3).map(workCard)) : nothing("No agent is working.")));
  const all = seenActs().length ? button("Mark all seen", "sm ghost", markAllSeen) : null;
  h.append(sect("Recently finished", "finished", fin.length, all || moreBtn("homeFin", fin.length, 4),
    fin.length ? grid("cards four", fin.slice(0, ib.more.homeFin ? 99 : 4).map((it) => itemCard(it))) : nothing("Nothing finished since you looked.")));
  const keys = el("div", "keys");
  const k = (ks, t) => { const s = el("span", ""); s.append(...ks.map((x) => el("kbd", "", x)), ` ${t}`); return s; };
  keys.append(k(["⌥", "N"], "next"), k(["⌘", "N"], "new chat"), k(["⌘", "K"], "search"), k(["⌥", "B"], "board"));
  h.append(keys);
  box.replaceChildren(h);
}

// workCard is a working agent: its line, and its todo list's progress.
function workCard(a) {
  const c = el("div", "icard");
  const ch = el("div", "ch");
  const who = el("div", "who");
  who.append(el("span", "nm", label(a)), el("span", "sub", [a.project.name, a.team && `${a.team} ${a.role}`].filter(Boolean).join(" · ")));
  ch.append(avatar(a), who, el("span", "age", a.cost ? money(a.cost) : ""));
  const act = el("div", "act");
  act.append(...activityLine(a));
  c.append(ch, act);
  if (a.todo && a.todo.total) {
    const t = el("div", "todo");
    t.append(meter(a.todo.done / a.todo.total, ""), el("span", "tn", a.todo.now || `${a.todo.done} of ${a.todo.total}`));
    c.append(t);
  }
  c.onclick = () => show(a.id);
  return c;
}

// spendMeter is what mad's own work cost today against its budget; amber
// from 80% of it, red once spent and agents wait.
function spendMeter(s) {
  if (!s || !s.day || (!s.usd && !s.turns)) return null;
  const part = s.part || 0;
  const box = el("span", `spend${s.closed ? " closed" : part >= 0.8 ? " hot" : ""}`);
  box.title = "what mad's own work cost today, against its budget";
  const usd = `$${s.usd.toFixed(2)}` + (s.budget_usd ? ` of $${+s.budget_usd.toFixed(2)}` : "");
  box.append("mad today ", el("b", "", usd), meter(part), el("b", "pc", `${Math.round(part * 100)}%`));
  return box;
}

// ---- search, theme ----

$("#filter").addEventListener("input", () => drawSidebar());
$("#filter").addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    document.body.classList.remove("searching");
    e.target.value = "";
    drawSidebar();
    e.target.blur();
  } else if (e.key === "Enter") {
    const first = $("#projects .row:not(.fold)");
    if (first) first.click();
  }
});
window.addEventListener("keydown", (e) => {
  if (e.metaKey && !e.altKey && !e.ctrlKey && e.key.toLowerCase() === "k") {
    e.preventDefault();
    document.body.classList.add("searching");
    $("#filter").focus();
    $("#filter").select();
  }
}, true);
if (!/Mac/.test(navigator.platform)) $("#filterKey").hidden = true;

const themes = ["auto", "light", "dark"];
let theme = themes.includes(localStorageGet("mad.theme")) ? localStorageGet("mad.theme") : "auto";
function applyTheme() {
  if (theme === "auto") delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = theme;
  const b = $("#theme");
  b.replaceChildren(icon({ auto: "auto", light: "sun", dark: "moon" }[theme]));
  b.title = `Theme: ${theme === "auto" ? "as the system's" : theme} (click to change)`;
}
$("#theme").onclick = () => {
  theme = themes[(themes.indexOf(theme) + 1) % themes.length];
  localStorageSet("mad.theme", theme);
  applyTheme();
};
applyTheme();

// ---- the queue of the agent shown: messages typed in when its turn ends ----

function drawQueue(fresh) {
  const box = $("#queue");
  if (sk.open || bd.open) {
    box.hidden = true; // the Skills page and the board have no message box
    return;
  }
  const a = current && agents.get(current);
  // Nothing to say here to a shell: it is the terminal deck's.
  const off = !nc.open && !cv.open && (!a || !a.chat);
  if (box.hidden !== off) {
    box.hidden = off;
    fitInput();
  }
  $("#attach").hidden = !takesImages();
  if (cv.open) {
    // A channel: what is typed is posted, as you.
    $("#qinput").placeholder = "Message " + chanTitle(cv.open) + ", @ to wake an agent";
    $("#qitems").replaceChildren();
    $("#qitems").dataset.key = "";
    $("#qpause").hidden = true;
    drawChanWho();
    drawTodo();
    drawSend();
    return;
  }
  if (nc.open) {
    $("#qinput").placeholder = "Ask anything, or / for commands";
    $("#qitems").replaceChildren();
    $("#qitems").dataset.key = "";
    $("#qpause").hidden = true;
    drawNewWho();
    drawTodo();
    drawSend();
    return;
  }
  $("#qinput").placeholder = "Message the agent, or / for commands";
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
      const x = el("button", "x icon-btn");
      x.type = "button";
      x.title = "drop it";
      x.append(icon("x"));
      x.onclick = () => queueCall("DELETE", { item: it.id });
      li.append(el("span", "n", String(i + 1)), el("span", "t", it.text.split("\n")[0]), x);
      list.append(li);
    });
  }
  const o = opened.get(current);
  $("#qerr").textContent = windowed(a) ? (o && (o.err || o.hs && o.hs.error)) || ""
    : q.error && items.length ? `Not sent yet: ${q.error}` : "";
  const pause = $("#qpause");
  pause.hidden = !items.length && !q.paused;
  pause.replaceChildren(icon(q.paused ? "play" : "pause"));
  pause.title = q.paused ? "the queue is held: let it go" : "hold the queued messages";
  pause.classList.toggle("on", !!q.paused);
  drawWho(a);
  drawTodo();
  drawSend();
}

// drawTodo pins the agent's todo list over the message box while some of
// it is left to do: what it is on, how far along, the whole list to
// unfold.
function drawTodo() {
  const box = $("#qtodo");
  const o = !nc.open && opened.get(current);
  const items = (o && o.todo && o.todo.view.todos) || [];
  const left = items.filter((t) => t.status !== "completed");
  if (!left.length) {
    box.hidden = true;
    box.dataset.key = "";
    return;
  }
  const key = JSON.stringify([current, items, !!o.todoOpen]);
  if (box.dataset.key === key) return;
  box.dataset.key = key;
  const now = items.find((t) => t.status === "in_progress") || left[0];
  const head = el("button", "thead");
  head.type = "button";
  head.title = o.todoOpen ? "fold the list" : "the whole list";
  head.append(icon("circledot"), el("span", "now", now.text || ""),
    el("span", "count", `${items.length - left.length}/${items.length}`), icon(o.todoOpen ? "down" : "right", "chev"));
  head.onclick = () => {
    o.todoOpen = !o.todoOpen;
    drawTodo();
    fitInput();
  };
  box.replaceChildren(head);
  if (o.todoOpen) box.append(todoView(items).body);
  box.hidden = false;
}

// drawWho says, under the message box, which agent it goes to and what
// it is doing.
function drawWho(a) {
  const who = $("#who");
  const what = el("span", "what");
  what.append(el("b", "", a.kind), ` · ${a.project.name}${a.project.branch ? " / " + a.project.branch : ""}`);
  const o = opened.get(a.id);
  if (windowed(a) && o && o.hs) {
    // Run here: its model to pick, and what its host says it does.
    const parts = [avatar(a, true), what];
    const name = modelName(o.hs);
    keepModels(a.kind, o.hs.models);
    if (o.hs.models && o.hs.models.length) {
      const b = el("button", "chip model");
      b.type = "button";
      b.title = "its model";
      b.append(el("span", "", name || "Model"), icon("down"));
      b.onmousedown = (e) => { e.preventDefault(); modelMenu(b, o); };
      parts.push(b);
    }
    const perm = permissionName(o.hs);
    if (perm) {
      const b = el("button", `chip perm p-${o.hs.permission}`);
      b.type = "button";
      b.title = "what it may do without asking";
      b.append(icon("shield"), el("span", "", perm), icon("down"));
      b.onmousedown = (e) => { e.preventDefault(); permissionMenu(b, o); };
      parts.push(b);
    }
    parts.push(statusPill(a));
    who.replaceChildren(...parts);
    who.className = `s-${shownStatus(a)}`;
    return;
  }
  who.replaceChildren(avatar(a, true), what, statusPill(a));
  who.className = `s-${shownStatus(a)}`;
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

async function submit() {
  const t = $("#qinput");
  const text = t.value;
  const id = current;
  if (cv.open) {
    if (text.trim()) postChannel(text);
    return;
  }
  if (nc.open) {
    if (text.trim() || pendingImgs.length) newSubmit(text);
    return;
  }
  if (!text.trim() && !(pendingImgs.length && windowed(agents.get(id))) || !id) return;
  closeMenu();
  if (windowed(agents.get(id)) || await claim(id, opened.get(id), true)) return hostSubmit(id, text);
  // Cleared at once, so what is typed next is a message of its own; put
  // back if it was not queued.
  t.value = "";
  fitInput();
  if (!(await queueCall("POST", {}, text))) {
    if (!t.value) t.value = text;
    fitInput();
    return;
  }
}

$("#qinput").addEventListener("keydown", (e) => {
  if (e.isComposing) return;
  if (!$("#qmenu").hidden && menuKey(e)) {
    e.preventDefault();
    return;
  }
  // Esc in a channel: not a reply after all.
  if (e.key === "Escape" && cv.open && cv.reply) {
    replyTo(null);
    return;
  }
  // Esc stops one run here, as in its TUI.
  if (e.key === "Escape" && $("#qsend").classList.contains("stop")) {
    hostSend(opened.get(current), { type: "interrupt" });
    return;
  }
  if (e.key !== "Enter" || e.shiftKey) return;
  e.preventDefault();
  submit();
});
// fitInput grows the message box with what is in it, and offers to send
// it if there is something.
function fitInput() {
  const t = $("#qinput");
  drawSend();
  t.style.height = "";
  // Not drawn yet (no tab shown): measured once it is.
  if (!t.offsetParent) return;
  t.style.height = `${Math.min(220, t.scrollHeight)}px`;
  t.style.overflowY = t.scrollHeight > 220 ? "auto" : "hidden";
}
// The message box grown or shrunk (more lines, the todo list unfolded):
// a conversation read at its end stays there.
new ResizeObserver(() => {
  const o = opened.get(current);
  if (o && o.stick !== false) o.chat.scrollTop = o.chat.scrollHeight;
}).observe($("#queue"));
$("#qinput").addEventListener("input", () => {
  fitInput();
  drawMenu();
});
$("#qsend").onclick = () => {
  if ($("#qsend").classList.contains("stop")) hostSend(opened.get(current), { type: "interrupt" });
  else submit();
};
$(".composer").addEventListener("mousedown", (e) => {
  // A click anywhere in the box but its buttons is a click in the text.
  if (!e.target.closest("button, textarea, #qmenu, #qitems")) {
    e.preventDefault();
    $("#qinput").focus();
  }
});
$("#qinput").addEventListener("keyup", (e) => {
  if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) drawMenu();
});
$("#qinput").addEventListener("click", () => drawMenu());
$("#qinput").addEventListener("focus", () => current && commandsOf(current));
$("#qinput").addEventListener("blur", () => closeMenu());

// ---- slash commands, offered as / starts a message ----

// The agent's commands (/api/commands): its TUI's own and what it found
// where it runs, skills and the like. Asked for again after a minute.
const commandLists = new Map(); // id → { at, list, pending }
const menu = { items: [], sel: 0, query: null, files: false };

function commandsOf(id) {
  const c = commandLists.get(id);
  if (c && Date.now() - c.at < 60000) return c.pending || Promise.resolve(c.list);
  const pending = fetch(`/api/commands?${new URLSearchParams({ token, id })}`)
    .then((r) => (r.ok ? r.json() : []))
    .catch(() => [])
    .then((list) => {
      commandLists.set(id, { at: Date.now(), list: list || [] });
      return list || [];
    });
  commandLists.set(id, { at: Date.now(), list: c ? c.list : [], pending });
  return pending;
}

function findCommand(id, name) {
  return (commandLists.get(id)?.list || []).find((c) => c.name === name || (c.aliases || []).includes(name));
}

// The command being typed: the message's first word, while the cursor is
// in it; not a path.
function commandWord(t) {
  const m = /^\/(\S*)/.exec(t.value);
  if (!m || m[1].includes("/") || t.selectionStart !== t.selectionEnd || t.selectionEnd > m[0].length) return null;
  return { query: m[1].toLowerCase(), end: m[0].length };
}

// rank orders command c for what was typed, q: named so first, then its
// name starting so, an alias, a name with q in it, its description; -1
// if none.
function rank(c, q) {
  if (!q) return 0;
  const names = [c.name, ...(c.aliases || [])].map((n) => n.toLowerCase());
  if (names.includes(q)) return 0;
  if (names[0].startsWith(q)) return 1;
  if (names.some((n) => n.startsWith(q))) return 2;
  if (names.some((n) => n.includes(q))) return 3;
  if (q.length > 2 && (c.description || "").toLowerCase().includes(q)) return 4;
  return -1;
}

async function drawMenu() {
  const t = $("#qinput");
  const id = current;
  const at = (id || nc.open || cv.open) && mentionWord(t);
  if (at) return cv.open ? drawAgentMenu(at) : drawFiles(at);
  const w = id && commandWord(t);
  if (!w) return closeMenu();
  const list = await commandsOf(id);
  const now = commandWord(t);
  if (id !== current || !now || now.query !== w.query) return; // typed on: drawn for that
  // Run here, it takes what it takes headless; /model is the window's.
  const here = windowed(agents.get(id));
  const items = list.filter((c) => !here || c.headless || c.name === "model")
    .map((c) => [rank(c, w.query), c]).filter(([r]) => r >= 0)
    .sort((a, b) => a[0] - b[0] || a[1].name.localeCompare(b[1].name)).map(([, c]) => c);
  if (!items.length) return closeMenu();
  if (menu.query !== w.query || menu.files) menu.sel = 0;
  menu.items = items;
  menu.query = w.query;
  menu.files = false;
  menu.sel = Math.min(menu.sel, items.length - 1);
  const box = $("#qmenu");
  box.replaceChildren(...items.map((c, i) => {
    const li = el("li");
    li.append(el("span", "n", `/${c.name}`));
    if (c.hint) li.append(el("span", "h", c.hint));
    li.append(el("span", "d", c.description || ""));
    li.title = c.description || "";
    li.onmousedown = (e) => {
      e.preventDefault(); // the box keeps the focus
      menu.sel = i;
      pick(false);
    };
    return li;
  }));
  box.hidden = false;
  markSel();
}

function markSel() {
  const box = $("#qmenu");
  [...box.children].forEach((li, i) => li.classList.toggle("on", i === menu.sel));
  box.children[menu.sel]?.scrollIntoView({ block: "nearest" });
}

function closeMenu() {
  $("#qmenu").hidden = true;
  menu.items = [];
  menu.query = null;
  menu.files = false;
}

// ---- files, offered as @ starts a word (/api/files) ----

// mentionWord is the @word being typed, the cursor at its end.
function mentionWord(t) {
  if (t.selectionStart !== t.selectionEnd) return null;
  const m = /(^|\s)@([^\s@]*)$/.exec(t.value.slice(0, t.selectionEnd));
  if (!m) return null;
  return { query: m[2], start: t.selectionEnd - m[2].length - 1, end: t.selectionEnd };
}

let filesSeq = 0;

async function drawFiles(w) {
  const n = ++filesSeq;
  const where = nc.open ? { project: nc.project || "" } : { id: current };
  let list = [];
  try {
    const r = await fetch(`/api/files?${new URLSearchParams({ token, ...where, q: w.query })}`);
    if (r.ok) list = await r.json();
  } catch (_) { /* offered nothing */ }
  const now = mentionWord($("#qinput"));
  if (n !== filesSeq || !now || now.query !== w.query) return; // typed on
  if (!list.length) return closeMenu();
  if (menu.query !== w.query || !menu.files) menu.sel = 0;
  menu.items = list;
  menu.query = w.query;
  menu.files = true;
  menu.sel = Math.min(menu.sel, list.length - 1);
  const box = $("#qmenu");
  box.replaceChildren(...list.map((f, i) => {
    const li = el("li", "file");
    const cut = f.path.replace(/\/$/, "").lastIndexOf("/") + 1;
    li.append(icon(f.dir ? "folder" : "file", "fi"), el("span", "n", f.path.slice(cut)), el("span", "d", f.path.slice(0, cut)));
    li.title = f.path;
    li.onmousedown = (e) => {
      e.preventDefault();
      menu.sel = i;
      pick(false);
    };
    return li;
  }));
  box.hidden = false;
  markSel();
}

// pickFile puts file f in place of the @word being typed: a directory
// to go on into, a file and a space after it.
function pickFile(f) {
  const t = $("#qinput");
  const w = mentionWord(t);
  if (!f || !w) return;
  const put = `@${f.path}${f.dir ? "" : " "}`;
  t.value = t.value.slice(0, w.start) + put + t.value.slice(w.end);
  const at = w.start + put.length;
  t.setSelectionRange(at, at);
  fitInput();
  if (f.dir) drawMenu();
}

// menuKey moves through the menu, and picks: Tab completes the command,
// Enter sends it, as in the agents' own TUIs. It reports whether e was
// the menu's.
function menuKey(e) {
  const n = menu.items.length;
  if (!n) return false;
  const down = e.key === "ArrowDown" || e.ctrlKey && e.key === "n";
  if (down || e.key === "ArrowUp" || e.ctrlKey && e.key === "p") {
    menu.sel = (menu.sel + (down ? 1 : n - 1)) % n;
    markSel();
  } else if (e.key === "Tab" && !e.shiftKey) {
    pick(false);
  } else if (e.key === "Enter" && !e.shiftKey) {
    pick(true);
  } else if (e.key === "Escape") {
    closeMenu();
  } else {
    return false;
  }
  return true;
}

// pick puts the chosen command in place of the one being typed, and with
// run sends the message.
function pick(run) {
  if (menu.files) {
    const f = menu.items[menu.sel];
    closeMenu();
    return pickFile(f);
  }
  const c = menu.items[menu.sel];
  const t = $("#qinput");
  const w = commandWord(t);
  closeMenu();
  if (!c || !w) return;
  const rest = t.value.slice(w.end);
  if (run) {
    t.value = `/${c.name}${rest}`;
    submit();
    return;
  }
  const sep = rest.startsWith(" ") ? "" : " ";
  t.value = `/${c.name}${sep}${rest}`;
  t.setSelectionRange(c.name.length + 2, c.name.length + 2);
  fitInput();
}
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
  await refresh();
  setTimeout(poll, 1000);
}

// setOffline says, over the whole page, that what it draws is old: a
// failed look leaves the last one on screen, and an agent that looks
// working may not be. The banner names when it was last true.
let lastGood = 0;
function setOffline(off) {
  const bar = $("#offline");
  if (!off) lastGood = Date.now();
  bar.hidden = !off;
  document.body.classList.toggle("stale", off);
  if (off) {
    const at = new Date(lastGood).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    bar.textContent = lastGood ? `Offline · as of ${at} · reconnecting` : "Offline · reconnecting";
  }
}

// refresh takes the deck's latest look and draws it.
async function refresh() {
  try {
    const r = await fetch(`/api/deck?token=${encodeURIComponent(token)}`);
    if (!r.ok) throw new Error(r.status);
    deck = await r.json();
    looks = new Map((deck.states || []).flatMap((l) => [[l.state, l], ...(l.also || []).map((raw) => [raw, l])]));
    $("#conn").classList.add("ok");
    $("#conn").title = "connected";
    setOffline(false);
    drawQuota(deck.quota);
    drawSidebar();
    openWanted();
    if (cv.open) drawChanHead();
    drawBoard();
    const o = opened.get(current);
    if (o && agents.get(current)?.chat) {
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
    setOffline(true);
  }
}

// ---- terminals ----

function open(id) {
  const tab = el("div", "tab");
  tab.append(el("span", "dot"), el("span", "t", label(agents.get(id) || { id, kind: "" })));
  const x = el("span", "x");
  x.append(icon("x"));
  x.title = "close (the agent keeps running)";
  x.onclick = (e) => { e.stopPropagation(); close(id); };
  tab.append(x);
  tab.onclick = () => show(id);
  $("#tabs").append(tab);

  // Its conversation. Reading it starts nothing.
  const chat = el("div", "chat");
  const log = el("div", "log");
  const live = el("div", "live"); // the message being written, last in the log
  const asks = el("div", "asks"); // what it asks, run here (window mode)
  log.append(live, asks);
  // While its TUI has it in the terminal deck: what it does is followed
  // here, what it asks answered there.
  const wait = el("div", "wait");
  wait.hidden = true;
  // Scrolled up: a way back to what is being written.
  const down = el("button", "tobottom");
  down.type = "button";
  down.title = "to the latest";
  down.append(icon("down"));
  down.onclick = () => chat.scrollTo({ top: chat.scrollHeight, behavior: "smooth" });
  chat.append(log, wait, down);
  chat.addEventListener("scroll", () => {
    const o = opened.get(id);
    if (!o) return;
    o.stick = nearEnd(o);
    down.classList.toggle("on", !o.stick);
  }, { passive: true });
  $("#terms").append(chat);

  const a = agents.get(id);
  const o = {
    id, tab, chat, log, live, wait,
    asks, hostWS: null, hs: null, err: "", claimAt: 0, // window mode: its host, and what it says
    status: a && a.status,
    file: "", offset: 0, loaded: false, loading: false, entries: 0,
    calls: new Map(), // tool call id → its node, which its result joins
    parts: new Map(), // message id → its parts the transcript has (claude)
    partIDs: new Set(), // parts the transcript has, by their own id (codex)
    msg: null, version: 0, msgAt: 0, follow: null, // the message being written (/api/live)
  };
  opened.set(id, o);
  return o;
}

function show(id) {
  inMain(true);
  if (current && current !== id) {
    const prev = opened.get(current);
    if (prev) {
      prev.chat.classList.remove("on");
      prev.tab.classList.remove("on");
      unfollow(prev);
    }
  }
  current = id;
  closeNew();
  closeChannel();
  closeSkills();
  closeBoard();
  closeMenu();
  closePopup();
  commandsOf(id);
  const o = opened.get(id) || open(id);
  $("#empty").style.display = "none";
  present(id, o);
  o.tab.scrollIntoView({ block: "nearest" });
  // The changes beside it are this agent's.
  if (side.id && side.id !== id) {
    if (agents.get(id)?.chat) openChanges(id);
    else closeChanges();
  }
  drawSidebar();
  seen();
}

// present shows agent id's conversation, run here if it can be (window
// mode), else followed as its TUI runs it.
function present(id, o) {
  o.chat.classList.add("on");
  fitInput();
  const a = agents.get(id);
  if (a && !a.chat) {
    // A shell, or a kind of the user's own: a terminal's alone.
    if (!o.log.querySelector(".tui-only")) {
      const n = el("div", "tui-only");
      const t = el("div", "");
      t.append(el("b", "", `A ${a.kind} is a terminal`), el("span", "muted", "It runs in the terminal deck: open it in your terminal app."));
      n.append(icon("terminal"), t, button("Open in terminal", "primary", () => openTerminal(agents.get(id) || a)));
      o.log.insertBefore(n, o.live);
    }
    return;
  }
  loadChat(id, o);
  if (windowed(a)) {
    hostConnect(id, o);
  } else {
    claim(id, o);
    follow(id, o);
  }
}

function close(id) {
  const o = opened.get(id);
  if (!o) return;
  opened.delete(id);
  unfollow(o);
  hostLeave(o);
  o.chat.remove();
  o.tab.remove();
  if (side.id === id) closeChanges();
  if (current === id) {
    current = null;
    const next = [...opened.keys()].pop();
    if (next) show(next);
    else if (!nc.open) {
      $("#empty").style.display = "";
      inMain(false);
    }
  }
  drawSidebar();
}

// ---- what a notification opens (sw.js) ----

// The service worker shows notifications pushed here; one clicked opens
// its agent, in this page (a message) or a new one (?agent=).
let toOpen = new URLSearchParams(location.search).get("agent");
let toOpenChannel = new URLSearchParams(location.search).get("channel");
if (toOpen || toOpenChannel) history.replaceState(null, "", location.pathname);
if ("serviceWorker" in navigator && isSecureContext) {
  navigator.serviceWorker.register("sw.js").catch(() => {});
  navigator.serviceWorker.addEventListener("message", (e) => {
    if (e.data && (e.data.open || e.data.channel)) {
      toOpen = e.data.open || null;
      toOpenChannel = e.data.channel || null;
      openWanted();
    }
  });
}

// openWanted shows the agent a notification is about, once the deck has it.
function openWanted() {
  if (toOpenChannel && busChannel(toOpenChannel)) {
    openChannel(toOpenChannel);
    toOpenChannel = toOpen = null;
  }
  if (toOpen && agents.has(toOpen)) {
    show(toOpen);
    toOpen = null;
  }
}

// ---- on a phone: the list, or what is open, a screen each ----

const phone = matchMedia("(max-width: 640px)");

// inMain shows what is open (an agent, a new chat) over the list, on a
// phone; the phone's back goes back to the list.
function inMain(on) {
  const was = document.body.classList.contains("in-main");
  document.body.classList.toggle("in-main", on);
  if (on && !was && phone.matches) history.pushState({ main: true }, "");
  drawTitle();
}
window.addEventListener("popstate", () => inMain(false));
$("#back").onclick = () => {
  if (history.state?.main) history.back();
  else inMain(false);
};

// drawTitle says over a phone's screen what is open.
function drawTitle() {
  const t = $("#ptitle");
  const a = current && agents.get(current);
  if (!a) {
    t.replaceChildren(el("span", "pt", bd.open ? "Board" : sk.open ? "Skills" : cv.open ? chanTitle(cv.open) : nc.open ? "New chat" : ""));
    t.className = "";
    return;
  }
  const what = el("span", "pw");
  what.append(el("span", "pt", label(a)), el("span", "ps", `${a.kind} · ${a.project.name}`));
  t.replaceChildren(avatar(a, true), what);
  t.className = `s-${shownStatus(a)}`;
}

// seen tells mad which agent is in front of you, while the window has the
// focus: it neither notifies of nor puts to sleep one you look at.
function seen() {
  if (current && document.hasFocus() && agents.get(current)) {
    fetch(`/api/seen?${new URLSearchParams({ token, id: current })}`, { method: "POST" }).catch(() => {});
  }
}
setInterval(seen, 5000);
window.addEventListener("focus", seen);

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
  for (const pre of div.querySelectorAll("pre")) codeBlock(pre);
  // A table in a frame of its width, scrolled sideways when wider.
  for (const t of div.querySelectorAll("table")) {
    const frame = el("div", "table");
    t.replaceWith(frame);
    frame.append(t);
  }
  return div;
}

// codeBlock frames a block of code, with its language and a copy button.
function codeBlock(pre) {
  const code = pre.querySelector("code") || pre;
  const box = el("div", "codeblock");
  const head = el("div", "ch");
  head.append(el("span", "", (/language-([\w+#.-]+)/.exec(code.className) || [])[1] || "text"));
  const copy = el("button");
  copy.type = "button";
  copy.append(icon("copy"), el("span", "", "Copy"));
  copy.onclick = async () => {
    try {
      await navigator.clipboard.writeText(code.textContent);
      copy.lastChild.textContent = "Copied";
      setTimeout(() => { copy.lastChild.textContent = "Copy"; }, 1500);
    } catch (_) { /* no clipboard here */ }
  };
  head.append(copy);
  pre.replaceWith(box);
  box.append(head, pre);
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
      o.log.replaceChildren(o.live, o.asks);
      o.calls.clear();
      o.lastCall = null;
      o.todo = null;
      if (id === current) drawTodo();
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
      if ((e.text || "").trim()) m.append(md(e.text));
      if (e.images?.length) m.append(imageRow(e.images.map((i) => i.url)));
      put(o, m);
      break;
    }
    case "text": {
      const m = el("div", "msg from-agent");
      m.append(md(e.text), copyButton(e.text));
      localImages(m, e.text, o.id);
      put(o, m);
      break;
    }
    case "command":
      put(o, el("div", "cmd", e.label));
      break;
    case "tool": {
      const d = callCard(e.tool, parseArgs(e.args), ctxOf(o), e.label, e.input);
      put(o, d);
      if (e.id) o.calls.set(e.id, d);
      o.lastCall = d;
      // The last todo list is the one to read; the ones before it fold.
      if (d.view.todos) {
        if (o.todo) o.todo.open = false;
        o.todo = d;
        if (o.id === current) drawTodo();
      }
      break;
    }
    case "result": {
      o.results++;
      const call = e.id && o.calls.get(e.id);
      const imgs = (e.images || []).map((i) => i.url);
      if (call) {
        callResult(call, e.text, e.error, imgs);
      } else {
        if (e.text || !imgs.length) put(o, el("pre", `output${e.error ? " err" : ""}`, e.text || "(no output)"));
        if (imgs.length) put(o, imageRow(imgs));
      }
      break;
    }
  }
}

// ---- images: what the user sent, a call gave back, a reply names ----

// imageRow shows images by their URLs (/api/image, /api/file), each to
// open large.
function imageRow(urls) {
  const row = el("div", "imgs");
  for (const u of urls) row.append(imageNode(u));
  return row;
}

function imageNode(u) {
  const img = el("img", "cimg");
  img.loading = "lazy";
  img.alt = "";
  img.src = `${u}&token=${encodeURIComponent(token)}`;
  img.onclick = () => openLightbox(img.src);
  // Grown as it came: a conversation read at its end stays there.
  img.onload = () => {
    const o = opened.get(current);
    if (o && o.stick !== false && o.chat.contains(img)) o.chat.scrollTop = o.chat.scrollHeight;
  };
  return img;
}

// localImages shows under agent message m the image files it names: in
// markdown (![](path)) or as paths (shot.png, ~/Desktop/a.png, /tmp/b.jpg).
// One that is not there, or not an image, goes away.
const imagePathRe = /(?:^|[\s(`'"“])((?:~\/|\.{1,2}\/|\/)?(?:[\w.@+-]+\/)*[\w.@+-]+\.(?:png|jpe?g|gif|webp))(?=$|[\s)`'"”,;:!?]|\.(?:\s|$))/gi;
function localImages(m, text, id) {
  const file = (p) => `/api/file?${new URLSearchParams({ id, path: p })}`;
  const seen = new Set();
  for (const img of m.querySelectorAll(".md img")) {
    const src = img.getAttribute("src") || "";
    if (/^(https?:|data:|\/api\/)/.test(src)) continue;
    seen.add(src);
    const n = imageNode(file(decodeURI(src)));
    n.onerror = () => n.remove();
    img.replaceWith(n);
  }
  const row = el("div", "imgs");
  for (const [, p] of (text || "").matchAll(imagePathRe)) {
    if (seen.has(p) || seen.size >= 6) continue;
    seen.add(p);
    const n = imageNode(file(p));
    n.onerror = () => {
      n.remove();
      if (!row.children.length) row.remove();
    };
    row.append(n);
  }
  if (row.children.length) m.insertBefore(row, m.querySelector(".mcopy"));
}

function openLightbox(src) {
  const box = el("div", "");
  box.id = "lightbox";
  const img = el("img");
  img.src = src;
  box.append(img);
  const close = () => {
    box.remove();
    window.removeEventListener("keydown", key, true);
  };
  const key = (e) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      close();
    }
  };
  box.onclick = close;
  window.addEventListener("keydown", key, true);
  document.body.append(box);
}

// copyButton copies an agent's message as it wrote it (markdown).
function copyButton(text) {
  const b = el("button", "mcopy");
  b.type = "button";
  b.title = "copy";
  b.append(icon("copy"));
  b.onclick = async () => {
    try {
      await navigator.clipboard.writeText(text);
      b.replaceChildren(icon("check"));
      setTimeout(() => b.replaceChildren(icon("copy")), 1500);
    } catch (_) { /* no clipboard here */ }
  };
  return b;
}

// ctxOf is what the tool views of agent o's calls go by: its project's
// directory, to show paths from.
function ctxOf(o) {
  return { dir: agents.get(o.id)?.project?.path || "" };
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
  const hostCodex = windowed(agents.get(o.id)) && agents.get(o.id)?.kind === "codex";
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
        n.append(el("span", "what shimmer", "Thinking"));
        if (b.text) n.append(el("div", "gist", b.text.slice(-300)));
        box.append(n);
        shown = true;
      } else if (b.kind === "tool") {
        // codex run here: its host's items are not the transcript's, so a
        // call done, or one the transcript has begun, is left to it.
        if (hostCodex && (b.done || o.lastCall && !o.lastCall.matches(".done, .failed"))) continue;
        // Its input is JSON as it comes (claude, pi), a script (codex's
        // exec), or what the host made of the call (codex: its command).
        const text = b.text || "";
        const json = text.trimStart().startsWith("{");
        const ctx = ctxOf(o);
        const args = json ? (b.done ? parseArgs(text) : {}) : b.tool === "exec" ? { input: text } : {};
        const d = callCard(b.tool, args, ctx, json ? hint(text) : shortPath(firstLine(text), ctx.dir));
        d.classList.add(b.done ? "done" : "pending");
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
  const running = windowed(a) && o.hs ? o.hs.status === "running" : a && a.status === "running";
  if (!shown && running && busy) {
    const n = el("div", "busy");
    n.append(el("span", "spin"), el("span", "shimmer", a.tool ? [a.tool, a.target].filter(Boolean).join(" · ") : "Working"));
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

// ---- window mode: agents mad host runs headless, drawn here ----

// windowed: agent a runs headless under mad host, for the window to draw.
function windowed(a) {
  return !!a && a.mode === "window";
}

// claim puts agent id in window mode as the window opens it, or (now) a
// message is sent to it, unless it is busy as its TUI or holds a draft of
// yours there (the server says): it is asked again a while later. It
// reports whether the agent runs here now.
async function claim(id, o, now) {
  const a = agents.get(id);
  if (!a || !o || !a.headless || windowed(a)) return windowed(a);
  if (a.status === "running" || a.status === "waiting") return false;
  if (!now && Date.now() - o.claimAt < 4000) return false;
  o.claimAt = Date.now();
  try {
    const r = await fetch(`/api/mode?${new URLSearchParams({ token, id, mode: "window" })}`, { method: "POST" });
    if (!r.ok) return false;
  } catch (_) {
    return false;
  }
  // The deck's next look says so too; till then, the page knows.
  for (const x of [a, agents.get(id)]) if (x) x.mode = "window";
  unfollow(o);
  hostConnect(id, o);
  if (id === current) drawQueue();
  return true;
}

// hostConnect keeps a websocket to agent id's host while its tab is open.
function hostConnect(id, o) {
  if (o.hostWS || !opened.has(id)) return;
  const ws = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/host?${new URLSearchParams({ token, id })}`);
  o.hostWS = ws;
  ws.onmessage = (ev) => {
    let m;
    try { m = JSON.parse(ev.data); } catch (_) { return; }
    if (m.type === "state" && m.state) hostState(id, o, m.state);
    else if (m.type === "error") {
      o.err = m.error;
      if (id === current) drawQueue();
    }
  };
  ws.onclose = () => {
    if (o.hostWS !== ws) return;
    o.hostWS = null;
    if (opened.has(id) && windowed(agents.get(id))) setTimeout(() => hostConnect(id, o), 1500);
  };
}

// hostLeave stops listening to agent's host: it went back to its TUI, or
// its tab closed.
function hostLeave(o) {
  const ws = o.hostWS;
  o.hostWS = null;
  o.hs = null;
  ws?.close();
  o.asks.replaceChildren();
}

function hostSend(o, cmd) {
  if (!o || !o.hostWS || o.hostWS.readyState !== WebSocket.OPEN) return false;
  o.hostWS.send(JSON.stringify(cmd));
  return true;
}

// hostState takes what the host says: the message being written, what it
// asks, and how it is.
function hostState(id, o, s) {
  o.hs = s;
  const m = s.message;
  if (m && m.id && (!o.msg || o.msg.id !== m.id || o.msg.version !== m.version || o.msg.done !== m.done)) {
    // One ended before it was first seen here is the transcript's.
    const old = !o.msg && m.done && s.status !== "running";
    o.msg = { ...m, blocks: m.blocks || [] };
    o.msgAt = old ? 0 : Date.now();
  }
  try {
    drawLive(o);
  } catch (err) {
    console.error("live", err);
  }
  drawAsks(o);
  if (id === current) drawQueue();
}

// hostSubmit sends text to agent id run here; its host starts if it is
// asleep. /model is the window's menu.
async function hostSubmit(id, text) {
  const t = $("#qinput");
  const o = opened.get(id);
  t.value = "";
  fitInput();
  if (/^\/model\s*$/.test(text.trim())) {
    const chip = document.querySelector("#who .model");
    if (chip && o && o.hs) modelMenu(chip, o);
    return;
  }
  const imgs = takeImages();
  let err = "";
  try {
    await sendTo(id, text, imgs);
  } catch (e) {
    err = e.message || "no connection to mad";
    restoreImages(imgs);
  }
  if (o) {
    o.err = err;
    if (!err) hostConnect(id, o);
  }
  if (err && !t.value) {
    t.value = text;
    fitInput();
  }
  if (id === current) drawQueue();
}

// drawSend makes the send button a stop button while one run here works
// and nothing is typed.
function drawSend() {
  const b = $("#qsend");
  const o = opened.get(current);
  const typed = !!$("#qinput").value.trim();
  const stop = !typed && windowed(agents.get(current)) && !!o && !!o.hs && o.hs.status === "running";
  if (b.classList.contains("stop") !== stop || !b.firstChild) {
    b.classList.toggle("stop", stop);
    b.replaceChildren(icon(stop ? "stop" : "send"));
    b.title = stop ? "stop it (Esc)" : "send (Enter)";
  }
  b.disabled = !stop && !typed;
  // How what is typed goes: run here, at once.
  const h = $("#hint");
  const key = nc.open || cv.open ? "now" : windowed(agents.get(current)) ? (o && o.hs && o.hs.status === "running" ? "stop" : "now") : "queue";
  if (h.dataset.key !== key) {
    h.dataset.key = key;
    h.replaceChildren({ queue: "Sent when its turn is over", now: "Sent now", stop: "Esc stops it" }[key] + " · ",
      el("kbd", "", "⇧"), el("kbd", "", "↵"), " new line");
  }
}

// currentModel is the one of hs.models it runs: named so, or the family
// its full name is of (claude-haiku-5-5 is haiku's).
function currentModel(hs) {
  const ms = hs.models || [];
  return ms.find((x) => x.value === hs.model) ||
    ms.find((x) => x.value !== "default" && (hs.model || "").includes(x.value));
}

function modelName(hs) {
  const m = currentModel(hs);
  return m ? m.name || m.value : hs.model || "";
}

// drawAsks keeps a card for each thing agent asks: added as asked, gone
// once answered; one being filled in is left as it is.
function drawAsks(o) {
  const want = new Map(((o.hs && o.hs.requests) || []).map((r) => [r.id, r]));
  for (const card of [...o.asks.children]) if (!want.has(card.dataset.id)) card.remove();
  const had = new Set([...o.asks.children].map((c) => c.dataset.id));
  let added = false;
  for (const [rid, r] of want) {
    if (had.has(rid)) continue;
    o.asks.append(askCard(o, r));
    added = true;
  }
  if (added) o.chat.scrollTop = o.chat.scrollHeight;
}

function button(text, cls, onclick) {
  const b = el("button", `btn${cls ? " " + cls : ""}`, text);
  b.type = "button";
  if (onclick) b.onclick = onclick;
  return b;
}

// askCard is request r as a card: may it use a tool (Allow, Always allow,
// Deny, or what to do instead), questions with options, or a plan.
function askCard(o, r) {
  const card = el("div", `ask ask-${r.kind}`);
  card.dataset.id = r.id;
  const reply = (answer) => {
    if (!hostSend(o, { type: "answer", answer: { id: r.id, ...answer } })) return;
    card.classList.add("sent");
    for (const c of card.querySelectorAll("button, input")) c.disabled = true;
  };
  const head = el("div", "ah");
  const body = el("div", "abody");
  const acts = el("div", "acts");
  const instead = (place, onsay) => {
    const say = el("input", "say");
    say.placeholder = place;
    say.onkeydown = (e) => {
      if (e.key === "Enter" && !e.isComposing && say.value.trim()) onsay(say.value.trim());
    };
    return say;
  };
  if (r.kind === "question") {
    head.append(icon("chat", "ai"), el("span", "t", "It asks you"));
    const qs = (r.questions || []).map((q) => questionBlock(q, body));
    const answers = () => {
      const out = {};
      for (const [q, get] of qs) {
        const v = get();
        if (!v) return null;
        out[q.question] = v;
      }
      return out;
    };
    const send = button("Answer", "primary", () => {
      const a = answers();
      if (a) reply({ allow: true, answers: a });
    });
    send.disabled = true;
    body.addEventListener("change", () => {
      send.disabled = !answers();
      // One question, one choice: picking is answering.
      if (qs.length === 1 && !qs[0][0].multiSelect && qs[0][2]()) send.click();
    });
    acts.append(send, button("Skip", "", () => reply({ allow: false, message: "The user skipped the question." })));
  } else if (r.kind === "plan") {
    head.append(icon("todo", "ai"), el("span", "t", "Its plan"));
    body.append(md(r.plan || ""));
    acts.append(button("Approve", "primary", () => reply({ allow: true })),
      instead("Or say what to change, and press Enter", (msg) => reply({ allow: false, message: msg })));
  } else {
    // Drawn as the call will be: a change as its diff.
    const v = r.args ? toolView(r.tool, parseArgs(r.args), ctxOf(o)) : r.tool === "apply_patch" && r.input ? patchView(r.input, ctxOf(o)) : null;
    head.append(icon(v?.icon || toolIcon(r.tool), "ai"), el("span", "t", r.title || `Allow ${r.tool}?`));
    const what = v?.sub || (!r.title && r.label);
    if (what) head.append(el("span", "al", what));
    if (v?.body) body.append(v.body);
    else if (r.input) body.append(el("pre", "input", r.input));
    acts.append(button("Allow", "primary", () => reply({ allow: true })));
    if (r.always) acts.append(button("Always allow", "", () => reply({ allow: true, always: true })));
    acts.append(button("Deny", "", () => reply({ allow: false })),
      instead("Or tell it what to do instead, and press Enter", (msg) => reply({ allow: false, message: msg })));
  }
  card.append(head, body, acts);
  return card;
}

// questionBlock draws question q into box: its options to pick (several
// for a multiSelect one), or something else written. It returns q, what
// is picked or written, and whether an option was picked last.
function questionBlock(q, box) {
  const wrap = el("div", "quest");
  if (q.header) wrap.append(el("span", "qh", q.header));
  wrap.append(el("div", "qq", q.question));
  const chosen = new Set();
  let picked = false;
  const list = el("div", "opts");
  const other = el("input", "say");
  other.placeholder = "Something else…";
  const changed = () => box.dispatchEvent(new Event("change"));
  for (const opt of q.options || []) {
    const b = el("button", "opt");
    b.type = "button";
    b.dataset.label = opt.label;
    b.append(el("span", "ol", opt.label));
    if (opt.description) b.append(el("span", "od", opt.description));
    b.onclick = () => {
      if (q.multiSelect) {
        if (chosen.has(opt.label)) chosen.delete(opt.label); else chosen.add(opt.label);
      } else {
        chosen.clear();
        chosen.add(opt.label);
      }
      for (const x of list.children) x.classList.toggle("on", chosen.has(x.dataset.label));
      other.value = "";
      picked = true;
      changed();
    };
    list.append(b);
  }
  other.oninput = () => {
    if (other.value.trim()) {
      chosen.clear();
      for (const x of list.children) x.classList.remove("on");
    }
    picked = false;
    changed();
  };
  wrap.append(list, other);
  box.append(wrap);
  return [q, () => other.value.trim() || [...chosen].join(", "), () => picked];
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
    // The next item of the inbox: what needs you, then what finished.
    e.preventDefault();
    e.stopPropagation();
    nextItem();
    return;
  } else if (e.code === "KeyB") {
    e.preventDefault();
    e.stopPropagation();
    openBoard();
    return;
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
