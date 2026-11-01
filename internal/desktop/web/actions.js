// What the window does besides drawing agents: popup menus, dialogs and
// toasts; new chats and past conversations; projects; an agent's
// rename, stop, delete and terminal; its changes; images in a message.
// It uses app.js's globals ($, el, icon, agents, opened, current, show…).
"use strict";

// ---- the parts: menus, dialogs, toasts ----

// popup shows a menu of items by anchor (an element, or a {x, y} point):
// each {label, desc, icon, checked, danger, disabled, onclick}, or "-" for
// a rule. A pick, a click elsewhere or Esc closes it. By keyboard: the first
// item has the focus; Up, Down, Home and End move, Enter or Space picks, Esc
// closes and gives the focus back, and Tab leaves and closes.
let popupBack = null; // what had the focus before the menu: the anchor, if an element
function popup(anchor, items, opts = {}) {
  const box = $("#popup");
  popupBack = anchor.focus ? anchor : document.activeElement;
  box.setAttribute("role", "menu");
  box.replaceChildren(...items.map((it) => {
    if (it === "-") {
      const rule = el("li", "rule");
      rule.setAttribute("role", "separator");
      return rule;
    }
    const li = el("li", [it.danger && "danger", it.disabled && "off", it.checked && "on"].filter(Boolean).join(" "));
    li.setAttribute("role", "menuitem");
    li.tabIndex = -1;
    if (it.disabled) li.setAttribute("aria-disabled", "true");
    li.append(it.icon ? icon(it.icon) : el("span", "noicon"));
    const t = el("span", "t");
    t.append(el("span", "l", it.label));
    if (it.desc) t.append(el("span", "d", it.desc));
    li.append(t);
    if (it.dot) li.append(redDot()); // something there waits on the person
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
  box.querySelector('li[role="menuitem"]:not(.off)')?.focus({ preventScroll: true });
}

// closePopup hides the menu; back gives the focus to what had it before.
function closePopup(back) {
  const box = $("#popup");
  const was = !box.hidden;
  box.hidden = true;
  if (was && back && popupBack?.isConnected) popupBack.focus({ preventScroll: true });
}
$("#popup").addEventListener("keydown", (e) => {
  const items = [...$("#popup").querySelectorAll('li[role="menuitem"]:not(.off)')];
  const at = items.indexOf(document.activeElement);
  const go = (i) => { e.preventDefault(); items[(i + items.length) % items.length]?.focus({ preventScroll: true }); };
  if (e.key === "ArrowDown") go(at + 1);
  else if (e.key === "ArrowUp") go(at < 0 ? -1 : at - 1);
  else if (e.key === "Home") go(0);
  else if (e.key === "End") go(items.length - 1);
  else if (e.key === "Enter" || e.key === " ") {
    e.preventDefault();
    document.activeElement?.click();
  } else if (e.key === "Escape") {
    e.preventDefault();
    e.stopPropagation();
    closePopup(true);
  } else if (e.key === "Tab") {
    closePopup(true); // the focus is back on the anchor before Tab moves it on
    if (typeof cp !== "undefined" && cp.open) trapTab(e); // from a menu of the channel's panel it moves on inside the panel
  }
});
// popupGuard: until then a press outside does not close the menu. A long press
// that opened it can end in the mouse events a browser makes of a touch.
let popupGuard = 0;
document.addEventListener("mousedown", (e) => {
  if (Date.now() < popupGuard) return;
  if (!$("#popup").hidden && !$("#popup").contains(e.target)) closePopup();
}, true);
window.addEventListener("blur", closePopup);

// dialog asks something over the page: a title, a body, and buttons
// ({label, value, cls}). It resolves to the value of the one pressed, or
// null when let go (Esc, a click beside it). ready gets done(value), to
// end it from the body.
function dialog({ title, body, actions, ready }) {
  return new Promise((resolve) => {
    // what had the focus gets it back when it closes: a menu's item is gone by now, its anchor is the one
    const at = document.activeElement;
    const from = at && at.closest && at.closest("#popup") ? popupBack : at;
    const d = $("#dialog");
    const card = el("div", "dlg");
    card.append(el("h2", "", title));
    if (body) card.append(body);
    const row = el("div", "dacts");
    const key = (e) => {
      if (e.key === "Escape" && !busy) {
        e.stopPropagation();
        done(null);
      }
    };
    const done = (v) => {
      d.hidden = true;
      d.replaceChildren();
      document.removeEventListener("keydown", key, true);
      if (from?.isConnected && from !== document.body) from.focus({ preventScroll: true });
      resolve(v);
    };
    // An action's submit runs with the dialog open: the error it returns
    // (or throws) is shown here and what was typed stays.
    const err = el("p", "dtext derr");
    err.hidden = true;
    let busy = false;
    const submit = async (a, b) => {
      if (busy) return;
      busy = true;
      err.hidden = true;
      const label = b.textContent;
      for (const x of row.children) x.disabled = true;
      if (a.busy) b.textContent = a.busy;
      let e = null;
      try {
        e = await a.submit();
      } catch (x) {
        e = x.message || String(x);
      }
      busy = false;
      if (!e) return done(a.value);
      for (const x of row.children) x.disabled = false;
      b.textContent = label;
      err.textContent = e;
      err.hidden = false;
      err.scrollIntoView({ block: "nearest" });
    };
    // now runs in the click itself: what a browser lets only a click do.
    for (const a of actions) {
      const b = button(a.label, a.cls, () => {
        if (a.submit) return submit(a, b);
        if (busy) return;
        if (a.now) a.now();
        done(a.value);
      });
      row.append(b);
    }
    card.append(err, row);
    d.replaceChildren(card);
    d.hidden = false;
    // A click beside a form would drop what was typed: only Cancel or Esc
    // closes one.
    d.onmousedown = (e) => { if (e.target === d && !busy && !card.querySelector("input, textarea")) done(null); };
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
  // An error is read, not glanced at: it stays as long as its text takes.
  const ms = kind === "err" ? Math.min(20000, 4000 + text.length * 80) : 3800;
  setTimeout(() => t.classList.add("out"), ms);
  setTimeout(() => t.remove(), ms + 500);
}

// api calls the window's server: JSON in and out, its text as the error.
async function api(method, path, params = {}, body) {
  const opts = { method };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
    opts.headers = { "Content-Type": "application/json" };
  }
  const r = await authFetch(`${path}?${new URLSearchParams(params)}`, opts);
  const text = await r.text();
  if (!r.ok) throw new Error(text.trim() || r.statusText);
  return text ? JSON.parse(text) : null;
}

function ago(when) {
  const s = (Date.now() - new Date(when)) / 1000;
  if (!when || isNaN(s)) return "";
  if (s < 90) return "刚刚";
  if (s < 5400) return `${Math.round(s / 60)} 分钟前`;
  if (s < 129600) return `${Math.round(s / 3600)} 小时前`;
  return `${Math.round(s / 86400)} 天前`;
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
  $("#newtitle").textContent = p ? "要做点什么？" : "先添加一个项目";
  $("#newsub").textContent = p ? `在 ${p.name}${p.branch ? " · " + p.branch : ""} 里` : "项目就是 agent 工作所在的目录。";
  const past = $("#past");
  if (!p) {
    past.replaceChildren(button("添加项目", "primary", () => addProject()));
    return;
  }
  if (past.dataset.project !== p.path) {
    past.dataset.project = p.path;
    past.replaceChildren(el("div", "muted pastnote", "正在找以前的对话…"));
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
  head.append(icon("history"), el("span", "", "接着以前的对话"));
  const rows = list.slice(0, 12).map((s) => {
    const r = el("button", "pastrow");
    r.type = "button";
    r.append(avatar({ kind: s.kind, icon: kindIcon(s.kind), color: kindColor(s.kind) }, true),
      el("span", "t", s.title || "无标题"), el("span", "w muted", ago(s.updated)));
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
  proj.append(icon("folder"), el("span", "", p ? p.name : "没有项目"), icon("down"));
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
    b.append(el("span", "", m ? m.name || m.value : "默认模型"), icon("down"));
    b.onmousedown = (e) => {
      e.preventDefault();
      popup(b, [{ label: "默认模型", desc: "按它自己的设置", checked: !nc.model, onclick: () => { nc.model = ""; drawQueue(); } },
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
    { label: "添加项目…", icon: "folderplus", onclick: () => addProject() },
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
  modelLists.set(kind, authFetch(`/api/models?${new URLSearchParams({ kind, project: nc.project || "" })}`)
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
  const r = await authFetch(`/api/send?${new URLSearchParams({ id })}`, {
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
  input.placeholder = "一个目录：输入路径，或从下面选一个";
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
      if (it.added) r.append(el("span", "pill quiet", "已添加"));
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
    title: "添加项目", body,
    actions: [{ label: "取消", value: null }, { label: "添加", value: "add", cls: "primary" }],
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
    toast(r.added ? `已添加 ${r.name}` : `${r.name} 早就在了`);
    await refresh();
    openNew(r.path);
  } catch (e) {
    toast(e.message, "err");
  }
}

// projectMenu is what can be done with project p.
function projectMenu(p, anchor) {
  popup(anchor, [
    { label: "新对话", icon: "compose", onclick: () => openNew(p.path) },
    { label: "新终端", desc: "在你的终端 app 里开一个 shell", icon: "terminal", onclick: () => newShell(p) },
    { label: "新小组…", desc: "按角色分工的 agent 一起在一个群里启动", icon: "agent", onclick: () => newTeam(p.path) },
    { label: "以前的对话", icon: "history", onclick: () => openNew(p.path) },
    "-",
    { label: "移除项目", icon: "trash", danger: true, disabled: p.agents.length > 0,
      desc: p.agents.length ? "先删掉它的 agent" : "只从 mad 里移除，磁盘上的东西不动", onclick: () => removeProject(p) },
  ], anchor.getBoundingClientRect ? { right: true } : {});
}

async function newShell(p) {
  try {
    const r = await api("POST", "/api/agents", {}, { project: p.path, kind: "shell" });
    await api("POST", "/api/terminal", { id: r.id });
    toast("已在你的终端 app 里打开");
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
    { label: "改名", icon: "edit", onclick: () => renameAgent(a) },
    { label: "在终端里打开", desc: "在你的终端 app 里打开 deck", icon: "external", onclick: () => openTerminal(a) },
    { label: "改动", desc: "它在自己目录里改了什么", icon: "diff", disabled: !a.chat, onclick: () => { show(a.id); openChanges(a.id); } },
    { label: "在消息总线上发消息", desc: "一条私信，它忙完这一轮后会读到", icon: "chat", disabled: !a.chat || (!!a.role && !a.team), onclick: () => messageAgent(a) },
    { label: "存为技能", desc: "请它把行之有效的做法写下来，由你批准", icon: "skill", disabled: !a.chat || (!!a.role && !a.team), onclick: () => saveAsSkill(a) },
    { label: "打断", desc: interruptWhy(a), icon: "stop", disabled: a.status !== "running" || !a.chat || interruptSent(a), onclick: () => interruptAgent(a) },
    { label: "让它休眠", icon: "sleep", disabled: off, onclick: () => sleepAgent(a),
      desc: off ? "它没有在运行" : waits ? "结束它的进程来释放内存：它现在问你的会丢掉；来一条消息会叫醒它" : "结束它的进程来释放内存；来一条消息会让它接着刚才的地方继续" },
    "-",
    { label: "删除", desc: "从 mad 里移除；它的对话仍留在磁盘上", icon: "trash", danger: true, onclick: () => deleteAgent(a) },
  ], opts || {});
}

async function renameAgent(a) {
  const input = el("input", "say wide");
  input.value = a.name || label(a);
  input.placeholder = "填个名字，留空则用对话的标题";
  const v = await dialog({
    title: "改名", body: input,
    actions: [{ label: "取消", value: null }, { label: "改名", value: "ok", cls: "primary" }],
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
    toast(`${label(a)} 已休眠：来一条消息会叫醒它`);
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
  if (interruptSent(a)) return "已发出：它马上停下";
  if (a.status === "running") return "马上停下它在做的事，和终端里按 Esc 一样；它不会退出";
  if (a.status === "waiting") return windowed(a) ? "它在等你：请在它的卡片上拒绝它的请求" : "它在等你：请在它的卡片上取消它的提问（取消 (Esc)）";
  return "它没有在工作";
}

// interruptButton is agent a's Interrupt on a row or a card; sent, it
// says so.
function interruptButton(a) {
  if (interruptSent(a)) {
    const b = rowButton("打断中…", "", () => {}, "已发出：它马上停下");
    b.disabled = true;
    return b;
  }
  return rowButton("打断", "", () => interruptAgent(a), "马上停下它在做的事，和终端里按 Esc 一样", `${a.id}\ninterrupt`);
}

async function interruptAgent(a) {
  if (interruptSent(a)) return;
  interrupts.set(a.id, { at: 0, status: a.status });
  drawSidebar();
  try {
    await api("POST", "/api/interrupt", { id: a.id });
    interrupts.set(a.id, { at: Date.now(), status: a.status });
    toast(`已打断 ${label(a)}`);
  } catch (e) {
    interrupts.delete(a.id);
    toast(e.message, "err");
  }
  drawSidebar();
  refresh();
}

async function deleteAgent(a) {
  const body = el("p", "muted dtext", `${label(a)} 会停下并离开 mad。它的对话仍留在磁盘上，可以从“以前的对话”接着聊。`);
  const v = await dialog({
    title: "删除这个 agent？", body,
    actions: [{ label: "取消", value: null }, { label: "删除", value: "del", cls: "danger" }],
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
    toast("已在你的终端 app 里打开");
  } catch (e) {
    toast(e.message, "err");
  }
}

// drawActs shows the bar's buttons for the agent shown.
function drawActs() {
  drawTitle();
  const a = current && agents.get(current);
  // a phone's channel has its ⓘ here, not in a band of its own
  const ch = !a && cv.open && phone.matches && !bd.open && !us.open && !sk.open && !nc.open;
  $("#acts").hidden = !a && !ch;
  const more = $("#more");
  more.setAttribute("aria-label", ch ? "群信息" : "更多");
  more.title = ch ? "群信息" : "更多";
  if (more.dataset.icon !== (ch ? "info" : "more")) {
    more.dataset.icon = ch ? "info" : "more";
    more.replaceChildren(icon(ch ? "info" : "more"));
  }
  $("#changes").hidden = !a || !a.chat;
  $("#changes").classList.toggle("on", !!a && side.id === current);
  // 对话 / 终端 / 改动: the conversation, its terminal, its changes
  const diff = !!a && side.id === current;
  $("#seg").hidden = !a;
  for (const b of $("#seg").children) {
    b.hidden = b.dataset.v !== "term" && !(a && a.chat);
    b.classList.toggle("on", (b.dataset.v === "diff") === diff && b.dataset.v !== "term");
  }
}
$("#seg").onclick = (e) => {
  const v = e.target.closest("button")?.dataset.v;
  const a = current && agents.get(current);
  if (!a) return;
  if (v === "term") openTerminal(a);
  else if (v === "diff") { if (side.id !== current) openChanges(current); }
  else if (v === "chat") closeChanges();
};
$("#changes").onclick = () => {
  if (side.id === current) closeChanges();
  else if (current) openChanges(current);
};
$("#more").onclick = () => {
  const a = current && agents.get(current);
  if (a) agentMenu(a, $("#more"), { right: true });
  else if (cv.open) openChanPanel($("#more"));
};

// ---- its changes: the diff of its directory, beside the conversation ----

const side = { id: null, seq: 0 };

async function openChanges(id) {
  side.id = id;
  const n = ++side.seq;
  const box = $("#side");
  box.hidden = false;
  drawActs();
  box.replaceChildren(sideHead(null), el("div", "muted sidenote", "正在读取它的改动…"));
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
  what.append(el("b", "", "改动"));
  if (d && d.branch) {
    const b = el("span", "branch muted");
    b.append(icon("branch"), d.branch);
    what.append(b);
  }
  const files = d && d.files ? d.files : [];
  if (d && !d.error) {
    const add = files.reduce((s, f) => s + (f.added || 0), 0), del = files.reduce((s, f) => s + (f.deleted || 0), 0);
    what.append(el("span", "muted", `${files.length} 个文件`), el("span", "plus", `+${add}`), el("span", "minus", `−${del}`));
  }
  const refreshB = el("button", "icon-btn");
  refreshB.type = "button";
  refreshB.title = "重新读取";
  refreshB.append(icon("refresh"));
  refreshB.onclick = () => side.id && openChanges(side.id);
  const shut = el("button", "icon-btn");
  shut.type = "button";
  shut.title = "关闭";
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
    parts.push(el("div", "muted sidenote", "自上次提交以来没有改动。"));
  } else {
    const body = el("div", "sidebody");
    const chunks = splitPatch(d.patch || "");
    let lines = 0;
    for (const f of d.files) {
      const sec = el("details", "dfile");
      sec.open = true;
      const sum = el("summary");
      const st = { "??": "新增", A: "新增", D: "已删", M: "", R: "已移动" }[(f.status || "").trim()[0] === "?" ? "??" : (f.status || "").trim()[0]] || "";
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
    if (d.cut) body.append(el("div", "muted sidenote", "后面的太长，这里不显示。"));
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

// MAX_IMAGES and MAX_IMAGE_BYTES are the server's limits (bus.MaxImages and
// bus.MaxImageBytes; a test keeps them equal).
const MAX_IMAGES = 6;
const MAX_IMAGE_BYTES = 10 << 20;
const IMAGE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];

// addImages takes the files that are pictures, up to what a message takes
// counting the ones still being read, and says how many it left out.
function addImages(files) {
  let left = 0;
  const bad = [];
  for (const f of files) {
    if (!f.type.startsWith("image/")) continue;
    if (!IMAGE_TYPES.includes(f.type)) {
      bad.push(f.name || "一张图片");
      continue;
    }
    if (f.size > MAX_IMAGE_BYTES) {
      toast(`${f.name || "一张图片"} 超过了 ${MAX_IMAGE_BYTES >> 20} MB`, "err");
      continue;
    }
    if (pendingImgs.length + pendingReads >= MAX_IMAGES) {
      left++;
      continue;
    }
    pendingReads++;
    const reader = new FileReader();
    reader.onload = () => {
      pendingReads--;
      const url = String(reader.result);
      pendingImgs.push({ mime: f.type, data: url.slice(url.indexOf(",") + 1), url });
      drawImages();
    };
    reader.onerror = () => {
      pendingReads--;
      toast(`${f.name || "一张图片"} 读不了`, "err");
      drawImages();
    };
    reader.readAsDataURL(f);
  }
  if (bad.length) toast(`${bad.join("、")}：请发 png、jpeg、gif 或 webp（HEIC 照片请转成 JPEG）`, "err");
  if (left) toast(`最多 ${MAX_IMAGES} 张图片：有 ${left} 张没加进来`, "err");
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
    x.title = "移除";
    x.append(icon("x"));
    x.onclick = () => {
      pendingImgs.splice(i, 1);
      drawImages();
    };
    t.append(img, x);
    return t;
  }));
  fitInput();
  drawSend(); // pictures alone can be sent to a channel
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

// Images go to an agent run here, or one about to be, or a channel.
function takesImages() {
  return nc.open || cv.open || windowed(agents.get(current));
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

// ---- the phone: this window on it, over Tailscale or a Cloudflare Tunnel (/api/phone) ----

async function phoneDialog() {
  let s;
  try {
    s = await api("GET", "/api/phone");
  } catch (e) {
    toast(e.message, "err");
    return;
  }
  const body = el("div", "phonebox");
  const cf = s.via === "cloudflare";
  let actions = [{ label: "好", cls: "primary", value: null }];
  if (s.problem) {
    body.append(el("p", "dtext", s.problem));
  } else if (!s.on) {
    body.append(el("p", "dtext", cf
      ? "通过 Cloudflare Tunnel 把这个窗口送到你的手机上，在哪里都能用（用 mad phone on --cloudflare 选的）：知道地址的人都能连上，但进去要输入它显示的验证码。"
      : `通过 Tailscale 把这个窗口送到你的手机上${s.host ? `，地址是 ${s.host}` : ""}：只有你自己的设备连得上，进去要输入它显示的验证码。`));
    actions = [{ label: "取消", value: null }, { label: "打开", cls: "primary", value: "on" }];
  } else {
    const img = el("img", "qr");
    img.alt = "QR code";
    loadImage(img, `/api/phone/qr?${new URLSearchParams({ t: Date.now() })}`);
    const url = el("div", "purl");
    const copy = el("button", "icon-btn");
    copy.type = "button";
    copy.title = "复制";
    copy.append(icon("copy"));
    copy.onclick = async () => {
      try {
        await navigator.clipboard.writeText(s.url);
        toast("已复制");
      } catch (_) { /* no clipboard here */ }
    };
    url.append(el("span", "", s.url), copy);
    const devs = el("div", "pdevs");
    try {
      for (const d of await api("GET", "/api/phone/devices")) {
        const row = el("div", "pdev");
        const rm = el("button", "icon-btn");
        rm.type = "button";
        rm.title = "移除";
        rm.textContent = "移除";
        rm.onclick = async () => {
          try {
            await api("DELETE", "/api/phone/devices", { id: d.id });
            row.remove();
          } catch (e) {
            toast(e.message, "err");
          }
        };
        row.append(el("span", "", `${d.name} · ${ago(d.last_seen)}在线`), rm);
        devs.append(row);
      }
    } catch (_) { /* the list is a courtesy */ }
    body.append(img, el("p", "dtext muted", `已通过 ${cf ? "Cloudflare Tunnel" : "Tailscale"} 提供。用手机相机扫它${cf ? "" : "（手机要在你的 tailnet 里）"}：里面的验证码只能用一次，10 分钟内有效，手机会得到一把自己的钥匙。在那里“添加到主屏幕”，下次就方便了。`), url, el("p", "dtext muted", "已配对的手机"), devs);
    actions = [{ label: "关闭", value: "off" }, { label: "完成", cls: "primary", value: null }];
  }
  const v = await dialog({ title: "在你的手机上", body, actions });
  try {
    if (v === "on") {
      const r = await api("POST", "/api/phone");
      if (r.problem) toast(r.problem, "err");
      else phoneDialog();
    } else if (v === "off") {
      await api("DELETE", "/api/phone");
      toast("手机的入口已关闭");
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
      ? "iPhone 上的通知要通过主屏幕上的 mad 来收：点分享 → 添加到主屏幕，从那里打开 mad，再在这里打开通知。"
      : "这个浏览器收不到通知。请在手机上打开 mad（点手机按钮），在那里打开通知。"));
  } else if (Notification.permission === "denied") {
    body.append(el("p", "dtext", "这个浏览器的设置里屏蔽了 mad 的通知：请先在那里允许。"));
  } else if (sub && st.on) {
    body.append(el("p", "dtext", "agent 在等你或做完时，这台设备会收到通知，不管 mad 开没开着。"));
    actions = [{ label: "关闭", value: "off" }, { label: "发一条测试", value: "test" }, { label: "完成", cls: "primary", value: null }];
  } else {
    body.append(el("p", "dtext", "agent 在等你或做完时，在这台设备上收到通知，不管 mad 开没开着。"));
    actions = [{ label: "取消", value: null },
      { label: "打开", cls: "primary", value: "on", now: () => { asked = Notification.requestPermission(); } }];
  }
  const v = await dialog({ title: "通知", body, actions });
  try {
    if (v === "on") {
      if ((await asked) !== "granted") {
        toast("没有允许通知", "err");
        return;
      }
      const key = Uint8Array.from(atob(st.key.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));
      // One made for another key of mad's goes first.
      if (sub) await sub.unsubscribe();
      sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
      await api("POST", "/api/push", {}, sub.toJSON());
      await api("POST", "/api/push/test", { endpoint: sub.endpoint });
      toast("通知已打开：测试通知马上到");
    } else if (v === "off") {
      await api("DELETE", "/api/push", { endpoint: sub.endpoint });
      await sub.unsubscribe();
      toast("这台设备的通知已关闭");
    } else if (v === "test") {
      await api("POST", "/api/push/test", { endpoint: sub.endpoint });
      toast("已发送");
    }
  } catch (e) {
    toast(e.message, "err");
  }
}

// ---- wiring ----

$("#newchat").onclick = () => openNew();
$("#emptynew").onclick = () => openNew();
$("#addproject").onclick = () => addProject();

// In the app the footer's icon row is not drawn: what was there that suits
// it (adding a project, Skills, the theme, how many agents) is in this menu.
// The header's menu: the pages, and what the footer's icons were.
$("#embed-menu").onclick = () => {
  popup($("#embed-menu"), [
    { label: "用量", dot: !!(deck.spend && deck.spend.closed), onclick: () => openUsage() },
    { label: "看板", onclick: () => openBoard() },
    { label: "技能", onclick: () => $("#skills").click() },
    { label: "手机和已配对设备", onclick: () => phoneDialog() },
    ...(embed ? [{ label: "切换 Mac", onclick: () => switchMac() }] : [{ label: "通知", onclick: () => $("#bell").click() }]),
    "-",
    { label: "添加项目", onclick: () => $("#addproject").click() },
    { label: `主题：${themeWord[theme]}`, onclick: () => $("#theme").click() },
    // #embed=app from a link in a browser: the app's own page is not there to leave it
    ...(embed && !window.webkit?.messageHandlers?.mad ? [{ label: "显示完整页面", onclick: () => leaveEmbed() }] : []),
  ], { right: true });
};
$("#phone").onclick = () => phoneDialog();
$("#bell").onclick = () => notifyDialog();
window.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !$("#popup").hidden) {
    closePopup(true);
    e.stopImmediatePropagation(); // handlers after this one (the channel panel's) must not see it: one Esc, one layer
    return;
  }
  if (e.metaKey && !e.altKey && !e.ctrlKey && e.key.toLowerCase() === "n") {
    e.preventDefault();
    openNew();
  }
}, true);
