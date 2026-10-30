// The bus in the window (package bus): channels and DMs in the sidebar,
// a channel's conversation where an agent's would be, and the person
// posting in it, @mentioning the agents to wake.
"use strict";

// cv is the channel shown: its id, the last message drawn.
const cv = { open: null, last: 0, seq: 0, timer: null, paused: false };

function busChannel(id) {
  return (deck.bus || []).find((c) => c.id === id);
}

// channelsGroup is the sidebar's list of channels and DMs, over the
// projects.
function channelsGroup() {
  const g = el("div", "busgroup");
  const head = el("div", "chead");
  head.append(icon("chat"), el("b", "", "Channels"));
  const plus = el("button", "icon-btn");
  plus.type = "button";
  plus.title = "a new channel";
  plus.append(icon("plus"));
  plus.onclick = (e) => {
    e.stopPropagation();
    createChannel();
  };
  head.append(el("span", "grow"), plus);
  g.append(head);
  const list = deck.bus || [];
  if (!list.length) {
    const hint = el("div", "chan none", "Talk with your agents: + makes a channel.");
    hint.onclick = () => createChannel();
    g.append(hint);
  }
  for (const c of list) {
    const row = el("div", `chan${cv.open === c.id ? " sel" : ""}${c.unread ? " unread" : ""}${c.dm && !c.yours ? " theirs" : ""}`);
    row.title = c.topic || c.title;
    row.append(el("span", "ch", c.dm ? (c.yours ? "@" : "↔") : "#"),
      el("span", "cn", c.dm ? c.title.replace(/^@/, "") : c.id));
    if (c.paused) row.append(el("span", "pill needs", "paused"));
    else if (c.unread) row.append(el("span", "cnt", String(c.unread)));
    row.onclick = () => openChannel(c.id);
    g.append(row);
  }
  return g;
}

// openChannel shows channel id (or a DM of the person's, made with its
// first message) in place of what was shown.
function openChannel(id) {
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
  closeChanges();
  closeMenu();
  closePopup();
  $("#empty").style.display = "none";
  const fresh = cv.open !== id;
  cv.open = id;
  $("#chanview").hidden = false;
  if (fresh) {
    cv.last = 0;
    cv.paused = false;
    $("#chanlog").replaceChildren();
  }
  inMain(true);
  drawChanHead();
  drawSidebar();
  drawQueue();
  loadChannel(fresh);
  clearInterval(cv.timer);
  cv.timer = setInterval(() => loadChannel(false), 1500);
  if (!phone.matches) $("#qinput").focus();
}

function closeChannel() {
  if (!cv.open) return;
  cv.open = null;
  clearInterval(cv.timer);
  $("#chanview").hidden = true;
}

// dmWith is the person's DM with agent id, as the bus names it.
function dmWith(id) {
  return "dm." + [id, "you"].sort().join(".");
}

// chanTitle is how channel id reads: #name, @agent, a ↔ b.
function chanTitle(id) {
  const c = busChannel(id);
  if (c) return c.dm ? c.title : "#" + c.id;
  const other = id.startsWith("dm.") && id.slice(3).split(".").find((x) => x !== "you");
  return other && agents.get(other) ? "@" + label(agents.get(other)) : id;
}

function drawChanHead() {
  const id = cv.open;
  if (!id) return;
  const c = busChannel(id);
  const head = $("#chanhead");
  const what = el("div", "cwhat");
  what.append(el("b", "", chanTitle(id)));
  if (c && c.topic) what.append(el("span", "muted", c.topic));
  const who = el("div", "cwho");
  for (const m of (c && c.members) || []) {
    const a = agents.get(m.id) || { kind: m.kind, id: m.id };
    const v = avatar(a, true);
    v.title = m.label;
    v.onclick = () => show(m.id);
    who.append(v);
  }
  const parts = [what, el("span", "grow"), who];
  if (c && c.paused) {
    const r = button("Resume", "", async () => {
      try {
        await api("POST", "/api/bus/resume", {}, { channel: id });
        loadChannel(false);
      } catch (e) {
        toast(e.message, "err");
      }
    });
    parts.push(el("span", "pill needs", "paused: waiting for you"), r);
  }
  head.replaceChildren(...parts);
  // On a phone the top bar names it: the head shows only what more there is.
  head.classList.toggle("bare", !(c && (c.topic || c.paused)));
}

async function loadChannel(first) {
  const id = cv.open;
  if (!id) return;
  const n = ++cv.seq;
  let r;
  try {
    r = await api("GET", "/api/bus/messages", { channel: id, since: cv.last });
  } catch (_) {
    return; // a DM not made yet, or the next look
  }
  if (n !== cv.seq || id !== cv.open) return;
  const scroll = $("#chanscroll");
  const atEnd = first || scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight < 80;
  for (const m of r.messages) {
    if (m.seq <= cv.last) continue;
    $("#chanlog").append(busMessage(m));
    cv.last = m.seq;
  }
  if (!cv.last && !$("#chanlog").children.length) {
    $("#chanlog").append(el("div", "note", id.startsWith("dm.") ? "Say something: it reaches the agent when its turn is over." : "Nothing said yet. @mention an agent to wake it."));
  }
  if (cv.paused !== r.paused) {
    cv.paused = r.paused;
    drawChanHead();
  }
  if (atEnd) scroll.scrollTop = scroll.scrollHeight;
  if (r.messages.length) api("POST", "/api/bus/read", {}, { channel: id, seq: cv.last }).catch(() => {});
}

// busMessage draws one message: the person's on the right, an agent's
// with its avatar, a handoff or its result as a card, the bus's own
// lines in the middle.
function busMessage(m) {
  if (m.kind === "system") return el("div", "bsys", m.text);
  $("#chanlog .note")?.remove();
  const mine = m.from.id === "you";
  const card = m.kind === "handoff" || m.kind === "done";
  const row = el("div", `bmsg${mine ? " mine" : ""}${card ? " card" : ""}`);
  if (!mine) {
    const a = agents.get(m.from.id) || { kind: m.from.kind || "?", id: m.from.id };
    const v = avatar(a, true);
    if (agents.has(m.from.id)) {
      v.title = "open it";
      v.onclick = () => show(m.from.id);
    }
    row.append(v);
  }
  const body = el("div", "bbody");
  const meta = el("div", "bmeta");
  meta.append(el("b", "", mine ? "You" : m.from.label), el("span", "muted", `#${m.seq} · ${clock(m.at)}`));
  if (m.reply_to) meta.append(el("span", "muted", `↩ #${m.reply_to}`));
  const h = m.handoff;
  if (m.kind === "handoff") meta.append(el("span", "pill working", `hands ${h ? h.id + " " : ""}to ${agentLabel(h && h.to)}`));
  if (m.kind === "done") meta.append(el("span", `pill ${h && h.status === "declined" ? "needs" : "done"}`, `${h ? h.id + " " : ""}${(h && h.status) || "done"}`));
  const text = el("div", "btext");
  text.append(md(mentionsBold(m.text || "")));
  body.append(meta, text);
  row.append(body);
  return row;
}

function agentLabel(id) {
  const a = id && agents.get(id);
  return a ? label(a) : id ? id.slice(0, 8) : "?";
}

function clock(at) {
  const d = new Date(at);
  return isNaN(d) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

// mentionsBold marks @label in markdown text, the agents' labels first
// (some have a space in them).
function mentionsBold(text) {
  const names = [...agents.values()].map((a) => label(a)).concat(["all", "you"]).sort((x, y) => y.length - x.length);
  const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  if (!names.length) return text;
  const re = new RegExp("(^|[\\s(])@(" + names.map(esc).join("|") + ")(?![\\w-])", "gi");
  return text.replace(re, (_, pre, name) => `${pre}**@${name}**`);
}

// postChannel posts what is typed, as the person.
async function postChannel(text) {
  const t = $("#qinput");
  const id = cv.open;
  t.value = "";
  fitInput();
  try {
    const r = await api("POST", "/api/bus/post", {}, { channel: id, text });
    if (r && r.warning) toast(r.warning, "err");
    $("#qerr").textContent = "";
  } catch (e) {
    if (!t.value) t.value = text;
    fitInput();
    $("#qerr").textContent = e.message;
  }
  loadChannel(false);
}

// drawChanWho says, under the message box, where it goes.
function drawChanWho() {
  const c = busChannel(cv.open);
  const who = $("#who");
  const what = el("span", "what");
  const n = c ? c.members.length : 0;
  what.append(el("b", "", chanTitle(cv.open)), c && !c.dm ? ` · ${n} agent${n === 1 ? "" : "s"} · @ to wake one` : " · reaches it when its turn is over");
  who.replaceChildren(what);
  who.className = "";
}

// drawAgentMenu offers the agents as @ is typed in a channel: its
// members first.
function drawAgentMenu(w) {
  const c = busChannel(cv.open);
  const members = new Set(((c && c.members) || []).map((m) => m.id));
  const q = w.query.toLowerCase();
  const all = [...agents.values()].filter((a) => a.chat && !a.role)
    .filter((a) => !q || label(a).toLowerCase().includes(q) || a.id.startsWith(q))
    .sort((x, y) => (members.has(y.id) - members.has(x.id)) || label(x).localeCompare(label(y)));
  if (!all.length) return closeMenu();
  if (menu.query !== w.query || !menu.files) menu.sel = 0;
  menu.items = all.map((a) => ({ path: label(a), dir: false }));
  menu.query = w.query;
  menu.files = true;
  menu.sel = Math.min(menu.sel, all.length - 1);
  const box = $("#qmenu");
  box.replaceChildren(...all.map((a, i) => {
    const li = el("li", "file");
    li.append(avatar(a, true), el("span", "n", label(a)), el("span", "d", members.has(a.id) ? "in the channel" : `${a.kind} · ${a.project.name}`));
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

// createChannel asks for a name and the agents to put in it.
async function createChannel() {
  const body = el("div", "");
  const name = el("input", "say wide");
  name.placeholder = "name, e.g. api-redesign";
  const topic = el("input", "say wide");
  topic.placeholder = "what it is for (optional)";
  const pick = el("div", "suggest");
  const chosen = new Set();
  for (const a of [...agents.values()].filter((x) => x.chat && !x.role)) {
    const row = el("label", "srow pickrow");
    const box = el("input");
    box.type = "checkbox";
    box.onchange = () => (box.checked ? chosen.add(a.id) : chosen.delete(a.id));
    row.append(box, avatar(a, true), el("span", "t", label(a)), el("span", "muted", a.project.name));
    pick.append(row);
  }
  body.append(name, topic, el("p", "dtext muted", "Agents in it read it when they want; an @mention wakes one."), pick);
  const v = await dialog({ title: "A new channel", body, actions: [{ label: "Cancel", value: null }, { label: "Create", cls: "primary", value: "ok" }], ready: () => name.focus() });
  if (v !== "ok" || !name.value.trim()) return;
  try {
    const c = await api("POST", "/api/bus/channel", {}, { name: name.value.trim(), topic: topic.value.trim(), members: [...chosen] });
    await refresh();
    openChannel(c.id);
  } catch (e) {
    toast(e.message, "err");
  }
}

// messageAgent opens the person's DM with agent a.
function messageAgent(a) {
  openChannel(dmWith(a.id));
}
