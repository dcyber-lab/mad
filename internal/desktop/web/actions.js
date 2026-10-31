// What the window does besides drawing agents: popup menus, dialogs and
// toasts; new chats and past conversations; projects; an agent's
// rename, stop, delete and terminal; its changes; images in a message.
// It uses app.js's globals ($, el, icon, agents, opened, current, show…).
"use strict";

// ---- the parts: menus, dialogs, toasts ----

// popup shows a menu of items by anchor (an element, or a {x, y} point):
// each {label, desc, icon, checked, danger, disabled, onclick}, or "-" for
// a rule. A pick, a click elsewhere or Esc closes it.
function popup(anchor, items, opts = {}) {
  const box = $("#popup");
  box.replaceChildren(...items.map((it) => {
    if (it === "-") return el("li", "rule");
    const li = el("li", [it.danger && "danger", it.disabled && "off", it.checked && "on"].filter(Boolean).join(" "));
    li.append(it.icon ? icon(it.icon) : el("span", "noicon"));
    const t = el("span", "t");
    t.append(el("span", "l", it.label));
    if (it.desc) t.append(el("span", "d", it.desc));
    li.append(t);
    if (it.checked) li.append(icon("check", "ok"));
    li.onmousedown = (e) => e.preventDefault(); // the focus stays where it was
    li.onclick = (e) => {
      e.stopPropagation();
      if (it.disabled) return;
      closePopup();
      it.onclick?.();
    };
    return li;
  }));
  box.hidden = false;
  const r = anchor.getBoundingClientRect ? anchor.getBoundingClientRect()
    : { left: anchor.x, right: anchor.x, top: anchor.y, bottom: anchor.y };
  const w = box.offsetWidth, h = box.offsetHeight;
  let x = opts.right ? r.right - w : r.left;
  let y = r.bottom + 6;
  if (opts.above || y + h > innerHeight - 8) y = Math.max(8, r.top - h - 6);
  box.style.left = `${Math.min(Math.max(8, x), innerWidth - w - 8)}px`;
  box.style.top = `${y}px`;
}

function closePopup() {
  $("#popup").hidden = true;
}
document.addEventListener("mousedown", (e) => {
  if (!$("#popup").hidden && !$("#popup").contains(e.target)) closePopup();
}, true);
window.addEventListener("blur", closePopup);

// dialog asks something over the page: a title, a body, and buttons
// ({label, value, cls}). It resolves to the value of the one pressed, or
// null when let go (Esc, a click beside it). ready gets done(value), to
// end it from the body.
function dialog({ title, body, actions, ready }) {
  return new Promise((resolve) => {
    const d = $("#dialog");
    const card = el("div", "dlg");
    card.append(el("h2", "", title));
    if (body) card.append(body);
    const row = el("div", "dacts");
    const key = (e) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        done(null);
      }
    };
    const done = (v) => {
      d.hidden = true;
      d.replaceChildren();
      document.removeEventListener("keydown", key, true);
      resolve(v);
    };
    // now runs in the click itself: what a browser lets only a click do.
    for (const a of actions) row.append(button(a.label, a.cls, () => { if (a.now) a.now(); done(a.value); }));
    card.append(row);
    d.replaceChildren(card);
    d.hidden = false;
    d.onmousedown = (e) => { if (e.target === d) done(null); };
    document.addEventListener("keydown", key, true);
    ready?.(done);
    (card.querySelector("input") || row.lastChild)?.focus();
  });
}

// toast says how something went, for a few seconds.
function toast(text, kind) {
  const t = el("div", `toast${kind ? " " + kind : ""}`);
  t.append(icon(kind === "err" ? "alert" : "check"), el("span", "", text));
  $("#toasts").append(t);
  setTimeout(() => t.classList.add("out"), 3800);
  setTimeout(() => t.remove(), 4300);
}

// api calls the window's server: JSON in and out, its text as the error.
async function api(method, path, params = {}, body) {
  const opts = { method };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
    opts.headers = { "Content-Type": "application/json" };
  }
  const r = await fetch(`${path}?${new URLSearchParams({ token, ...params })}`, opts);
  const text = await r.text();
  if (!r.ok) throw new Error(text.trim() || r.statusText);
  return text ? JSON.parse(text) : null;
}

function ago(when) {
  const s = (Date.now() - new Date(when)) / 1000;
  if (!when || isNaN(s)) return "";
  if (s < 90) return "just now";
  if (s < 5400) return `${Math.round(s / 60)} min ago`;
  if (s < 129600) return `${Math.round(s / 3600)} h ago`;
  return `${Math.round(s / 86400)} d ago`;
}

function projectOf(path) {
  return deck.projects.find((p) => p.path === path);
}

// ---- a new chat: the message box, a project and a kind to start ----

const KINDS = [["claude", "Claude"], ["codex", "Codex"], ["pi", "Pi"]];
const nc = { open: false, project: "", kind: localStorageGet("mad.kind") || "claude", model: "" };

// openNew shows the new chat page, in project if given.
function openNew(project) {
  const was = current && agents.get(current)?.project?.path;
  if (current) {
    const o = opened.get(current);
    if (o) {
      o.chat.classList.remove("on");
      o.tab.classList.remove("on");
      unfollow(o);
    }
    current = null;
  }
  nc.open = true;
  closeChannel();
  closeSkills();
  closeBoard();
  inMain(true);
  for (const [k] of KINDS) knownModels(k); // asked now: there by the time it is picked
  nc.project = project || was || nc.project || (deck.projects[0] && deck.projects[0].path) || "";
  if (nc.project && !projectOf(nc.project)) nc.project = deck.projects[0] ? deck.projects[0].path : "";
  $("#empty").style.display = "none";
  $("#new").hidden = false;
  $("#past").hidden = false;
  $("#center").classList.add("fresh");
  closeChanges();
  closeMenu();
  drawNew();
  drawSidebar();
  $("#qinput").focus();
}

function closeNew() {
  if (!nc.open) return;
  nc.open = false;
  $("#new").hidden = true;
  $("#past").hidden = true;
  $("#center").classList.remove("fresh");
}

// drawNew draws the page: what to start, and the project's past
// conversations to go on with.
function drawNew() {
  const p = projectOf(nc.project);
  $("#newtitle").textContent = p ? "What should we work on?" : "Add a project to start";
  $("#newsub").textContent = p ? `in ${p.name}${p.branch ? " · " + p.branch : ""}` : "A project is a directory the agents work in.";
  const past = $("#past");
  if (!p) {
    past.replaceChildren(button("Add a project", "primary", () => addProject()));
    return;
  }
  if (past.dataset.project !== p.path) {
    past.dataset.project = p.path;
    past.replaceChildren(el("div", "muted pastnote", "Looking for past conversations…"));
    loadPast(p.path);
  }
  drawNewSkills();
}

const pastCache = new Map(); // project → { at, list }

async function loadPast(path) {
  let list;
  const c = pastCache.get(path);
  if (c && Date.now() - c.at < 30000) list = c.list;
  else {
    try {
      list = (await api("GET", "/api/history", { project: path })) || [];
    } catch (_) {
      list = [];
    }
    pastCache.set(path, { at: Date.now(), list });
  }
  const past = $("#past");
  if (past.dataset.project !== path) return;
  if (!list.length) {
    past.replaceChildren();
    return;
  }
  const head = el("div", "pasthead");
  head.append(icon("history"), el("span", "", "Continue a past conversation"));
  const rows = list.slice(0, 12).map((s) => {
    const r = el("button", "pastrow");
    r.type = "button";
    r.append(avatar({ kind: s.kind, icon: kindIcon(s.kind), color: kindColor(s.kind) }, true),
      el("span", "t", s.title || "untitled"), el("span", "w muted", ago(s.updated)));
    r.title = `${s.kind} · ${s.id}`;
    r.onclick = () => continuePast(path, s);
    return r;
  });
  past.replaceChildren(head, ...rows);
}

// kindIcon and kindColor are a kind's as the deck draws it.
function kindIcon(kind) {
  for (const a of agents.values()) if (a.kind === kind && a.icon) return a.icon;
  return { claude: "✻", codex: ">_", pi: "π", shell: "$" }[kind] || kind[0];
}

function kindColor(kind) {
  for (const a of agents.values()) if (a.kind === kind && a.color) return a.color;
  return { claude: "173", codex: "252", pi: "111", shell: "114" }[kind] || "";
}

async function continuePast(project, s) {
  try {
    const r = await api("POST", "/api/agents", {}, { project, kind: s.kind, session: s.id, cwd: s.cwd || "" });
    pastCache.delete(project);
    await refresh();
    show(r.id);
  } catch (e) {
    toast(e.message, "err");
  }
}

// drawNewWho fills the bar under the message box for a new chat: where,
// what, and which model.
function drawNewWho() {
  const who = $("#who");
  const p = projectOf(nc.project);
  const proj = el("button", "chip");
  proj.type = "button";
  proj.append(icon("folder"), el("span", "", p ? p.name : "No project"), icon("down"));
  proj.onmousedown = (e) => { e.preventDefault(); projectPicker(proj); };
  const kinds = el("span", "seg");
  for (const [k, name] of KINDS) {
    const b = el("button", k === nc.kind ? "on" : "");
    b.type = "button";
    b.title = name;
    b.append(el("span", "kg", kindIcon(k)), el("span", "kn", name));
    b.onmousedown = (e) => {
      e.preventDefault();
      nc.kind = k;
      nc.model = "";
      localStorageSet("mad.kind", k);
      drawQueue();
    };
    kinds.append(b);
  }
  const parts = [proj, kinds];
  const models = knownModels(nc.kind);
  if (models.length) {
    const m = models.find((x) => x.value === nc.model);
    const b = el("button", "chip");
    b.type = "button";
    b.append(el("span", "", m ? m.name || m.value : "Default model"), icon("down"));
    b.onmousedown = (e) => {
      e.preventDefault();
      popup(b, [{ label: "Default model", desc: "as its own settings say", checked: !nc.model, onclick: () => { nc.model = ""; drawQueue(); } },
        ...models.map((x) => ({ label: x.name || x.value, desc: x.description, checked: x.value === nc.model,
          onclick: () => { nc.model = x.value; drawQueue(); } }))], { above: true });
    };
    parts.push(b);
  }
  who.replaceChildren(...parts);
  who.className = "";
}

function projectPicker(anchor) {
  popup(anchor, [
    ...deck.projects.map((p) => ({ label: p.name, desc: p.branch ? `${p.branch} · ${p.path}` : p.path, icon: "folder",
      checked: p.path === nc.project, onclick: () => { nc.project = p.path; drawNew(); drawQueue(); } })),
    "-",
    { label: "Add a project…", icon: "folderplus", onclick: () => addProject() },
  ], { above: true });
}

// The models a kind offers, as one of its agents run here said last.
// knownModels is what a new chat of kind can start on: asked of mad once
// a page (/api/models), what an agent of the kind said last shown till
// it answers.
const modelLists = new Map(); // kind → its models, once asked
function knownModels(kind) {
  const got = modelLists.get(kind);
  if (got) return Array.isArray(got) ? got : savedModels(kind);
  modelLists.set(kind, fetch(`/api/models?${new URLSearchParams({ token, kind, project: nc.project || "" })}`)
    .then((r) => (r.ok ? r.json() : []))
    .catch(() => [])
    .then((list) => {
      if (list && list.length) keepModels(kind, list);
      modelLists.set(kind, list && list.length ? list : savedModels(kind));
      if (nc.open && nc.kind === kind) drawQueue();
    }));
  return savedModels(kind);
}

function savedModels(kind) {
  try {
    return JSON.parse(localStorageGet(`mad.models.${kind}`) || "[]");
  } catch (_) {
    return [];
  }
}

function keepModels(kind, models) {
  if (models && models.length) localStorageSet(`mad.models.${kind}`, JSON.stringify(models));
}

// newSubmit starts the chat: an agent of the kind in the project, run
// here, the message its first.
async function newSubmit(text) {
  if (!projectOf(nc.project)) {
    addProject();
    return;
  }
  const t = $("#qinput");
  const imgs = takeImages();
  t.value = "";
  fitInput();
  $("#qsend").disabled = true;
  try {
    const body = { project: nc.project, kind: nc.kind, model: nc.model, text: imgs.length ? "" : text };
    const r = await api("POST", "/api/agents", {}, body);
    if (imgs.length) await sendTo(r.id, text, imgs);
    else if (r.error) toast(r.error, "err");
    await refresh();
    show(r.id);
  } catch (e) {
    $("#qerr").textContent = e.message;
    if (!t.value) t.value = text;
    restoreImages(imgs);
    fitInput();
  }
}

// sendTo gives agent id run here a message, images and all.
async function sendTo(id, text, imgs) {
  const r = await fetch(`/api/send?${new URLSearchParams({ token, id })}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text, images: (imgs || []).map(({ mime, data }) => ({ mime, data })) }),
  });
  if (!r.ok) throw new Error((await r.text()).trim());
}

// ---- projects ----

// addProject asks for a directory, offering the ones agents ran in.
async function addProject() {
  const input = el("input", "say wide");
  input.placeholder = "A directory: type a path, or pick one below";
  input.spellcheck = false;
  const list = el("div", "suggest");
  const body = el("div", "");
  body.append(input, list);
  let picked = "";
  let seq = 0;
  const fill = async () => {
    const n = ++seq;
    let items = [];
    try {
      items = (await api("GET", "/api/projects/suggest", { q: input.value.trim() })) || [];
    } catch (_) { /* none */ }
    if (n !== seq) return;
    list.replaceChildren(...items.map((it, i) => {
      const r = el("button", `srow${i === 0 ? " on" : ""}`);
      r.type = "button";
      r.append(icon("folder"), el("span", "t", it.name), el("span", "p muted", it.short || it.path));
      if (it.added) r.append(el("span", "pill quiet", "added"));
      r.dataset.path = it.path;
      r.onclick = () => {
        picked = it.path;
        input.value = it.short || it.path;
        list.querySelectorAll(".srow").forEach((x) => x.classList.toggle("on", x === r));
        input.focus();
      };
      return r;
    }));
    picked = items[0] ? items[0].path : "";
  };
  input.oninput = () => fill();
  const v = await dialog({
    title: "Add a project", body,
    actions: [{ label: "Cancel", value: null }, { label: "Add", value: "add", cls: "primary" }],
    ready: (done) => {
      input.onkeydown = (e) => { if (e.key === "Enter" && !e.isComposing) done("add"); };
      fill();
    },
  });
  if (v !== "add") return;
  const typed = input.value.trim();
  const path = /^[~/.]/.test(typed) && !picked.endsWith(typed.replace(/^~/, "")) ? typed : picked || typed;
  if (!path) return;
  try {
    const r = await api("POST", "/api/projects", {}, { path });
    toast(r.added ? `Added ${r.name}` : `${r.name} was there already`);
    await refresh();
    openNew(r.path);
  } catch (e) {
    toast(e.message, "err");
  }
}

// projectMenu is what can be done with project p.
function projectMenu(p, anchor) {
  popup(anchor, [
    { label: "New chat", icon: "compose", onclick: () => openNew(p.path) },
    { label: "New terminal", desc: "a shell, in your terminal app", icon: "terminal", onclick: () => newShell(p) },
    { label: "New team…", desc: "agents in roles start together in a channel", icon: "agent", onclick: () => newTeam(p.path) },
    { label: "Past conversations", icon: "history", onclick: () => openNew(p.path) },
    "-",
    { label: "Remove project", icon: "trash", danger: true, disabled: p.agents.length > 0,
      desc: p.agents.length ? "delete its agents first" : "from mad; nothing on disk", onclick: () => removeProject(p) },
  ], anchor.getBoundingClientRect ? { right: true } : {});
}

async function newShell(p) {
  try {
    const r = await api("POST", "/api/agents", {}, { project: p.path, kind: "shell" });
    await api("POST", "/api/terminal", { id: r.id });
    toast("Opened in your terminal app");
    refresh();
  } catch (e) {
    toast(e.message, "err");
  }
}

async function removeProject(p) {
  try {
    await api("DELETE", "/api/projects", { path: p.path });
    if (nc.project === p.path) nc.project = "";
    await refresh();
    if (nc.open) openNew();
  } catch (e) {
    toast(e.message, "err");
  }
}

// ---- an agent: rename, stop, delete, its terminal ----

// agentMenu is what can be done with agent a.
function agentMenu(a, anchor, opts) {
  const off = a.status === "stopped" || a.status === "asleep" || a.status === "exited";
  const waits = a.status === "waiting";
  popup(anchor, [
    { label: "Rename", icon: "edit", onclick: () => renameAgent(a) },
    { label: "Open in terminal", desc: "the terminal deck, in your terminal app", icon: "external", onclick: () => openTerminal(a) },
    { label: "Changes", desc: "what it changed in its directory", icon: "diff", disabled: !a.chat, onclick: () => { show(a.id); openChanges(a.id); } },
    { label: "Message on the bus", desc: "a DM it reads when its turn is over", icon: "chat", disabled: !a.chat || (!!a.role && !a.team), onclick: () => messageAgent(a) },
    { label: "Save as skill", desc: "asks it to write down what worked; you approve it", icon: "skill", disabled: !a.chat || (!!a.role && !a.team), onclick: () => saveAsSkill(a) },
    { label: "Interrupt", desc: interruptWhy(a), icon: "stop", disabled: a.status !== "running" || !a.chat || interruptSent(a), onclick: () => interruptAgent(a) },
    { label: "Put to sleep", icon: "sleep", disabled: off, onclick: () => sleepAgent(a),
      desc: off ? "it is not running" : waits ? "ends its process to free memory: what it asks you now is lost; a message wakes it" : "ends its process to free memory; a message wakes it where it left off" },
    "-",
    { label: "Delete", desc: "out of mad; its conversation stays on disk", icon: "trash", danger: true, onclick: () => deleteAgent(a) },
  ], opts || {});
}

async function renameAgent(a) {
  const input = el("input", "say wide");
  input.value = a.name || label(a);
  input.placeholder = "A name, or nothing for its conversation's title";
  const v = await dialog({
    title: "Rename", body: input,
    actions: [{ label: "Cancel", value: null }, { label: "Rename", value: "ok", cls: "primary" }],
    ready: (done) => {
      input.onkeydown = (e) => { if (e.key === "Enter" && !e.isComposing) done("ok"); };
      setTimeout(() => input.select(), 0);
    },
  });
  if (v !== "ok") return;
  try {
    await api("PATCH", "/api/agents", { id: a.id }, { name: input.value.trim() });
    refresh();
  } catch (e) {
    toast(e.message, "err");
  }
}

// sleepAgent ends agent a's process (the server refuses while a command
// runs under it, and says why); interruptAgent only stops its turn.
async function sleepAgent(a) {
  try {
    await api("POST", "/api/agents/stop", { id: a.id });
    toast(`${label(a)} is asleep: a message wakes it`);
    refresh();
  } catch (e) {
    toast(e.message, "err");
  }
}

// interrupts are the agents an Interrupt went to → {at, status}: each
// says so where it is drawn, with no second Interrupt, until the deck's
// look of it changes, or interruptMs pass: claude says nothing on Esc, so
// it reads working until its screen has stood still a while (longer than
// the server's interruptGap, when its status line ticks meanwhile).
const interrupts = new Map();
const interruptMs = 20000;

// interruptSent says whether an Interrupt to agent a is in flight or
// settling.
function interruptSent(a) {
  const p = interrupts.get(a.id);
  if (p && (p.at === 0 || (Date.now() - p.at < interruptMs && a.status === p.status))) return true;
  interrupts.delete(a.id);
  return false;
}

// interruptWhy is what the menu says of Interrupt for agent a.
function interruptWhy(a) {
  if (interruptSent(a)) return "sent: it stops in a moment";
  if (a.status === "running") return "stops what it does now, as Esc in its TUI; it stays up";
  if (a.status === "waiting") return windowed(a) ? "it waits on you: deny what it asks instead, on its card" : "it waits on you: cancel its prompt instead, with Cancel (Esc) on its card";
  return "it is not working";
}

// interruptButton is agent a's Interrupt on a row or a card; sent, it
// says so.
function interruptButton(a) {
  if (interruptSent(a)) {
    const b = rowButton("Interrupting…", "", () => {}, "sent: it stops in a moment");
    b.disabled = true;
    return b;
  }
  return rowButton("Interrupt", "", () => interruptAgent(a), "stop what it does now, as Esc in its TUI", `${a.id}\ninterrupt`);
}

async function interruptAgent(a) {
  if (interruptSent(a)) return;
  interrupts.set(a.id, { at: 0, status: a.status });
  drawSidebar();
  try {
    await api("POST", "/api/interrupt", { id: a.id });
    interrupts.set(a.id, { at: Date.now(), status: a.status });
    toast(`Interrupted ${label(a)}`);
  } catch (e) {
    interrupts.delete(a.id);
    toast(e.message, "err");
  }
  drawSidebar();
  refresh();
}

async function deleteAgent(a) {
  const body = el("p", "muted dtext", `${label(a)} stops and leaves mad. Its conversation stays on disk: you can go on with it from Past conversations.`);
  const v = await dialog({
    title: "Delete this agent?", body,
    actions: [{ label: "Cancel", value: null }, { label: "Delete", value: "del", cls: "danger" }],
  });
  if (v !== "del") return;
  try {
    await api("DELETE", "/api/agents", { id: a.id });
    if (opened.has(a.id)) close(a.id);
    pastCache.delete(a.project?.path);
    refresh();
  } catch (e) {
    toast(e.message, "err");
  }
}

async function openTerminal(a) {
  try {
    await api("POST", "/api/terminal", { id: a.id });
    toast("Opened in your terminal app");
  } catch (e) {
    toast(e.message, "err");
  }
}

// drawActs shows the bar's buttons for the agent shown.
function drawActs() {
  drawTitle();
  const a = current && agents.get(current);
  // a phone's channel has its menu here, not in a band of its own
  const ch = cv.open && phone.matches ? busChannel(cv.open) : null;
  $("#acts").hidden = !a && !(ch && !ch.dm);
  $("#changes").hidden = !a || !a.chat;
  $("#changes").classList.toggle("on", !!a && side.id === current);
}
$("#changes").onclick = () => {
  if (side.id === current) closeChanges();
  else if (current) openChanges(current);
};
$("#more").onclick = () => {
  const a = current && agents.get(current);
  const ch = cv.open && busChannel(cv.open);
  if (a) agentMenu(a, $("#more"), { right: true });
  else if (ch && !ch.dm) chanMenu(ch, $("#more"));
};

// ---- its changes: the diff of its directory, beside the conversation ----

const side = { id: null, seq: 0 };

async function openChanges(id) {
  side.id = id;
  const n = ++side.seq;
  const box = $("#side");
  box.hidden = false;
  drawActs();
  box.replaceChildren(sideHead(null), el("div", "muted sidenote", "Reading its changes…"));
  let d;
  try {
    d = await api("GET", "/api/diff", { id });
  } catch (e) {
    d = { error: e.message };
  }
  if (n !== side.seq || side.id !== id) return;
  drawChanges(d);
}

function closeChanges() {
  side.id = null;
  side.seq++;
  $("#side").hidden = true;
  drawActs();
}

function sideHead(d) {
  const head = el("div", "sidehead");
  const what = el("div", "what");
  what.append(el("b", "", "Changes"));
  if (d && d.branch) {
    const b = el("span", "branch muted");
    b.append(icon("branch"), d.branch);
    what.append(b);
  }
  const files = d && d.files ? d.files : [];
  if (d && !d.error) {
    const add = files.reduce((s, f) => s + (f.added || 0), 0), del = files.reduce((s, f) => s + (f.deleted || 0), 0);
    what.append(el("span", "muted", `${files.length} file${files.length === 1 ? "" : "s"}`), el("span", "plus", `+${add}`), el("span", "minus", `−${del}`));
  }
  const refreshB = el("button", "icon-btn");
  refreshB.type = "button";
  refreshB.title = "read again";
  refreshB.append(icon("refresh"));
  refreshB.onclick = () => side.id && openChanges(side.id);
  const shut = el("button", "icon-btn");
  shut.type = "button";
  shut.title = "close";
  shut.append(icon("panelclose"));
  shut.onclick = () => closeChanges();
  head.append(what, el("span", "grow"), refreshB, shut);
  return head;
}

// drawChanges draws the diff: a section a file, its lines marked.
function drawChanges(d) {
  const box = $("#side");
  const parts = [sideHead(d)];
  if (d.error) {
    parts.push(el("div", "muted sidenote", d.error));
  } else if (!d.files || !d.files.length) {
    parts.push(el("div", "muted sidenote", "Nothing changed since its last commit."));
  } else {
    const body = el("div", "sidebody");
    const chunks = splitPatch(d.patch || "");
    let lines = 0;
    for (const f of d.files) {
      const sec = el("details", "dfile");
      sec.open = true;
      const sum = el("summary");
      const st = { "??": "new", A: "new", D: "gone", M: "", R: "moved" }[(f.status || "").trim()[0] === "?" ? "??" : (f.status || "").trim()[0]] || "";
      sum.append(icon("right", "chev"), el("span", "p", f.path));
      if (st) sum.append(el("span", "pill quiet", st));
      sum.append(el("span", "plus", f.added ? `+${f.added}` : ""), el("span", "minus", f.deleted ? `−${f.deleted}` : ""));
      sec.append(sum);
      const chunk = chunks.get(f.path);
      if (chunk && lines < 6000) {
        const pre = el("div", "dlines");
        for (const line of chunk) {
          if (lines++ > 6000) break;
          const cls = line.startsWith("@@") ? "h" : line.startsWith("+") ? "a" : line.startsWith("-") ? "d" : "c";
          pre.append(el("div", `dl ${cls}`, line || " "));
        }
        sec.append(pre);
      }
      body.append(sec);
    }
    if (d.cut) body.append(el("div", "muted sidenote", "The rest is too long to show here."));
    parts.push(body);
  }
  box.replaceChildren(...parts);
}

// splitPatch cuts a unified diff into each file's lines from its first
// hunk on.
function splitPatch(patch) {
  const out = new Map();
  let file = null, lines = null, inHunk = false;
  for (const line of patch.split("\n")) {
    const m = /^diff --git a\/(.*) b\/(.*)$/.exec(line);
    if (m) {
      file = m[2] === "dev/null" ? m[1] : m[2];
      lines = [];
      out.set(file, lines);
      inHunk = false;
      continue;
    }
    if (!lines) continue;
    if (line.startsWith("+++ ")) {
      // a new file's name is in its +++ line
      const name = line.slice(4).replace(/^b\//, "");
      if (name !== "/dev/null" && name !== file) {
        out.delete(file);
        file = name;
        out.set(file, lines);
      }
      continue;
    }
    if (line.startsWith("@@")) inHunk = true;
    if (inHunk) lines.push(line);
  }
  return out;
}

// ---- images in a message ----

const pendingImgs = []; // { mime, data (base64), url }

function addImages(files) {
  for (const f of files) {
    if (!f.type.startsWith("image/")) continue;
    if (f.size > 10 << 20) {
      toast(`${f.name || "An image"} is over 10 MB`, "err");
      continue;
    }
    if (pendingImgs.length >= 8) {
      toast("8 images at most", "err");
      break;
    }
    const reader = new FileReader();
    reader.onload = () => {
      const url = String(reader.result);
      pendingImgs.push({ mime: f.type, data: url.slice(url.indexOf(",") + 1), url });
      drawImages();
    };
    reader.readAsDataURL(f);
  }
}

function drawImages() {
  const box = $("#qimgs");
  box.replaceChildren(...pendingImgs.map((im, i) => {
    const t = el("span", "thumb");
    const img = el("img");
    img.src = im.url;
    img.alt = "";
    const x = el("button", "x");
    x.type = "button";
    x.title = "remove it";
    x.append(icon("x"));
    x.onclick = () => {
      pendingImgs.splice(i, 1);
      drawImages();
    };
    t.append(img, x);
    return t;
  }));
  fitInput();
}

function takeImages() {
  const imgs = pendingImgs.splice(0);
  drawImages();
  return imgs;
}

function restoreImages(imgs) {
  pendingImgs.push(...imgs);
  drawImages();
}

// Images go to an agent run here, or one about to be.
function takesImages() {
  return nc.open || windowed(agents.get(current));
}

$("#attach").onclick = () => $("#imgpick").click();
$("#imgpick").onchange = (e) => {
  addImages([...e.target.files]);
  e.target.value = "";
};
$("#qinput").addEventListener("paste", (e) => {
  const files = [...(e.clipboardData?.files || [])].filter((f) => f.type.startsWith("image/"));
  if (files.length && takesImages()) {
    e.preventDefault();
    addImages(files);
  }
});
$(".composer").addEventListener("dragover", (e) => {
  if (takesImages() && [...(e.dataTransfer?.types || [])].includes("Files")) {
    e.preventDefault();
    $(".composer").classList.add("drop");
  }
});
$(".composer").addEventListener("dragleave", () => $(".composer").classList.remove("drop"));
$(".composer").addEventListener("drop", (e) => {
  $(".composer").classList.remove("drop");
  if (!takesImages()) return;
  e.preventDefault();
  addImages([...(e.dataTransfer?.files || [])]);
});

// ---- its model and how much it may do, run here ----

function modelMenu(anchor, o) {
  const cur = currentModel(o.hs);
  popup(anchor, (o.hs.models || []).map((m) => ({
    label: m.name || m.value, desc: m.description, checked: m === cur,
    onclick: () => hostSend(o, { type: "model", model: m.value }),
  })), { above: true });
}

function permissionMenu(anchor, o) {
  popup(anchor, (o.hs.permissions || []).map((m) => ({
    label: m.name || m.value, desc: m.description, checked: m.value === o.hs.permission,
    onclick: () => hostSend(o, { type: "permission", mode: m.value }),
  })), { above: true });
}

function permissionName(hs) {
  const m = (hs.permissions || []).find((x) => x.value === hs.permission);
  // A mode the menu does not list (claude's own report) shows as it is.
  return m ? m.name || m.value : hs.permission || "";
}

// ---- the phone: this window on it, over Tailscale (/api/phone) ----

async function phoneDialog() {
  let s;
  try {
    s = await api("GET", "/api/phone");
  } catch (e) {
    toast(e.message, "err");
    return;
  }
  const body = el("div", "phonebox");
  let actions = [{ label: "OK", cls: "primary", value: null }];
  if (s.problem) {
    body.append(el("p", "dtext", s.problem));
  } else if (!s.on) {
    body.append(el("p", "dtext", `Serve this window to your phone over Tailscale, at ${s.host}: only your own devices reach it, and they need the code it shows to get in.`));
    actions = [{ label: "Cancel", value: null }, { label: "Turn on", cls: "primary", value: "on" }];
  } else {
    const img = el("img", "qr");
    img.alt = "QR code";
    img.src = `/api/phone/qr?${new URLSearchParams({ token, t: Date.now() })}`;
    const url = el("div", "purl");
    const copy = el("button", "icon-btn");
    copy.type = "button";
    copy.title = "copy";
    copy.append(icon("copy"));
    copy.onclick = async () => {
      try {
        await navigator.clipboard.writeText(s.url);
        toast("Copied");
      } catch (_) { /* no clipboard here */ }
    };
    url.append(el("span", "", s.url), copy);
    body.append(img, el("p", "dtext muted", "Scan it with the phone's camera, the phone on your tailnet. There, Add to Home Screen keeps it at hand."), url);
    actions = [{ label: "Turn off", value: "off" }, { label: "Done", cls: "primary", value: null }];
  }
  const v = await dialog({ title: "On your phone", body, actions });
  try {
    if (v === "on") {
      const r = await api("POST", "/api/phone");
      if (r.problem) toast(r.problem, "err");
      else phoneDialog();
    } else if (v === "off") {
      await api("DELETE", "/api/phone");
      toast("The phone's way in is closed");
    }
  } catch (e) {
    toast(e.message, "err");
  }
}

// ---- notifications pushed to this device (/api/push, sw.js) ----

async function notifyDialog() {
  const can = "serviceWorker" in navigator && "PushManager" in window && isSecureContext;
  let reg = null, sub = null, st;
  try {
    if (can) {
      reg = await navigator.serviceWorker.ready;
      sub = await reg.pushManager.getSubscription();
    }
    st = await api("GET", "/api/push", sub ? { endpoint: sub.endpoint } : {});
  } catch (e) {
    toast(e.message, "err");
    return;
  }
  const body = el("div", "");
  let actions = [{ label: "OK", cls: "primary", value: null }];
  let asked = null;
  const ios = /iPhone|iPad|iPod/.test(navigator.userAgent);
  if (!can) {
    body.append(el("p", "dtext", ios && !navigator.standalone
      ? "On an iPhone, notifications come to mad on the Home Screen: Share → Add to Home Screen, open mad from there, and turn them on here."
      : "This browser can't be sent notifications here. Open mad on your phone (the phone button), and turn them on there."));
  } else if (Notification.permission === "denied") {
    body.append(el("p", "dtext", "Notifications are blocked for mad in this browser's settings: allow them there first."));
  } else if (sub && st.on) {
    body.append(el("p", "dtext", "This device is told when an agent needs you or is done, mad open or not."));
    actions = [{ label: "Turn off", value: "off" }, { label: "Send a test", value: "test" }, { label: "Done", cls: "primary", value: null }];
  } else {
    body.append(el("p", "dtext", "Be told on this device when an agent needs you or is done, mad open or not."));
    actions = [{ label: "Cancel", value: null },
      { label: "Turn on", cls: "primary", value: "on", now: () => { asked = Notification.requestPermission(); } }];
  }
  const v = await dialog({ title: "Notifications", body, actions });
  try {
    if (v === "on") {
      if ((await asked) !== "granted") {
        toast("Notifications were not allowed", "err");
        return;
      }
      const key = Uint8Array.from(atob(st.key.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));
      // One made for another key of mad's goes first.
      if (sub) await sub.unsubscribe();
      sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
      await api("POST", "/api/push", {}, sub.toJSON());
      await api("POST", "/api/push/test", { endpoint: sub.endpoint });
      toast("Notifications on: a test is on its way");
    } else if (v === "off") {
      await api("DELETE", "/api/push", { endpoint: sub.endpoint });
      await sub.unsubscribe();
      toast("Notifications off on this device");
    } else if (v === "test") {
      await api("POST", "/api/push/test", { endpoint: sub.endpoint });
      toast("Sent");
    }
  } catch (e) {
    toast(e.message, "err");
  }
}

// ---- wiring ----

$("#newchat").onclick = () => openNew();
$("#emptynew").onclick = () => openNew();
$("#addproject").onclick = () => addProject();
$("#phone").onclick = () => phoneDialog();
$("#bell").onclick = () => notifyDialog();
window.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !$("#popup").hidden) {
    closePopup();
    e.stopPropagation();
    return;
  }
  if (e.metaKey && !e.altKey && !e.ctrlKey && e.key.toLowerCase() === "n") {
    e.preventDefault();
    openNew();
  }
}, true);
