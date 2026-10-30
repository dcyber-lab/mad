// The task board in the window (bus.Board, with the snapshot): what
// waits for you first, then what is being done, blocked, in review, to
// do, and done today; a card's menu does what it needs.
"use strict";

const bd = { open: false, key: "" };

const boardColumns = [
  ["waiting-you", "Waiting for you"], ["doing", "Doing"], ["blocked", "Blocked"],
  ["review", "Review"], ["todo", "To do"], ["done", "Done today"],
];

const boardWhy = {
  permission: "waits on a permission", paused: "its channel is paused", asked: "asked you",
  unowned: "has no owner", lapsed: "handoff lapsed", waits: "waits on another task", stalled: "stalled",
  approve: "a skill to approve", budget: "agents wait for you", checkin: "a check-in that could not go out",
};

// boardRow is the sidebar's way to the board, with what waits for you.
function boardRow() {
  const cards = deck.board || [];
  const waiting = cards.filter((c) => c.column === "waiting-you").length;
  const row = el("div", `chan boardrow${bd.open ? " sel" : ""}`);
  row.title = "the task board";
  row.append(icon("todo", "ch"), el("span", "cn", "Board"));
  if (waiting) row.append(el("span", "pill needs", `${waiting} for you`));
  else if (cards.length) row.append(el("span", "muted bcount", String(cards.filter((c) => c.column !== "done").length)));
  row.onclick = () => openBoard();
  return row;
}

function openBoard() {
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
  closeChanges();
  closeMenu();
  closePopup();
  $("#empty").style.display = "none";
  bd.open = true;
  bd.key = "";
  $("#boardview").hidden = false;
  inMain(true);
  drawBoard();
  drawSidebar();
  drawQueue();
}

function closeBoard() {
  if (!bd.open) return;
  bd.open = false;
  $("#boardview").hidden = true;
  $("#empty").style.display = "";
}

function drawBoard() {
  if (!bd.open) return;
  const cards = deck.board || [];
  const key = JSON.stringify(cards.map((c) => [c.kind, c.task && c.task.id, c.column, c.why, c.owner, c.note, c.message && c.message.seq, Math.floor(c.age / 6e10)]))
    + JSON.stringify([...agents.values()].map((a) => a.shown)) + JSON.stringify(deck.spend || {})
    + JSON.stringify((deck.timers || []).map((t) => [t.id, t.next_at, t.paused, t.held, t.why, t.fired, t.missed]));
  if (key === bd.key) return;
  bd.key = key;
  const root = $("#boardview");
  const head = el("div", "bhead");
  const counts = boardColumns.map(([col, name]) => [name, cards.filter((c) => c.column === col).length]).filter(([, n]) => n);
  head.append(el("h1", "", "Board"), el("p", "muted", counts.length ? counts.map(([n, k]) => `${k} ${n.toLowerCase()}`).join(" · ") : "Nothing on the board: handoffs and the agents' task_set put tasks here."));
  const spend = spendLine(deck.spend);
  const cols = el("div", "bcols");
  for (const [col, name] of boardColumns) {
    const list = cards.filter((c) => c.column === col);
    const box = el("section", `bcol c-${col}${list.length ? "" : " empty"}`);
    const h = el("h2", "");
    h.append(name, el("span", "muted", list.length ? ` ${list.length}` : ""));
    box.append(h);
    if (!list.length) box.append(el("p", "muted bnone", col === "waiting-you" ? "Nothing waits for you." : "Nothing here."));
    for (const c of list) box.append(boardCard(c));
    cols.append(box);
  }
  root.replaceChildren(...[head, spend, timerStrip(), cols].filter(Boolean));
}

// spendLine is what mad's own work cost today against its budget, with a
// thin bar; amber from 80% of it, red once it is spent and agents wait.
function spendLine(s) {
  if (!s || !s.day || (!s.usd && !s.turns)) return null;
  const usd = `$${s.usd.toFixed(2)}` + (s.budget_usd ? ` of $${+s.budget_usd.toFixed(2)}` : "");
  const turns = `${s.turns}` + (s.budget_turns ? ` of ${s.budget_turns}` : "") + (s.turns === 1 ? " turn" : " turns");
  const part = Math.max(s.budget_usd ? s.usd / s.budget_usd : 0, s.budget_turns ? s.turns / s.budget_turns : 0);
  const box = el("div", `bspend${s.closed ? " closed" : part >= 0.8 ? " hot" : ""}`);
  const bar = el("div", "bbar");
  const fill = el("div", "bfill");
  fill.style.width = `${Math.min(100, Math.round(part * 100))}%`;
  bar.append(fill);
  const note = s.closed ? " · agents wait" : s.open_today ? " · lifted for today" : "";
  box.append(el("span", "muted", `mad's own work today: ${usd} · ${turns}${note}`), bar);
  return box;
}

function boardCard(c) {
  const card = el("div", `bcard k-${c.kind}${c.why ? " w-" + c.why : ""}`);
  const top = el("div", "btop");
  top.append(el("span", "btitle", c.title || (c.task && c.task.title) || ""));
  if (c.task) top.append(el("span", "muted bid", c.task.id));
  card.append(top);
  const meta = el("div", "bmeta2");
  const a = c.owner && agents.get(c.owner);
  if (a) {
    meta.append(avatar(a, true), el("span", "", label(a)), statusPill(a));
  } else if (c.owner === "you") {
    meta.append(el("span", "pill quiet", "you"));
  } else if (c.owner_label) {
    meta.append(el("span", "", c.owner_label));
  }
  if (c.channel) meta.append(el("span", "muted", c.channel.startsWith("dm.") ? "DM" : "#" + c.channel));
  meta.append(el("span", "muted", ageText(c.age)));
  card.append(meta);
  if (c.why) card.append(el("div", "bwhy", c.why === "waits" && c.waits && c.waits.length ? `waits on ${c.waits.join(", ")}` : boardWhy[c.why] || c.why));
  // An ask's message is what its title does not already say (bus.Card).
  const text = (c.message && c.message.text) || c.note;
  if (text && text.trim() !== card.querySelector(".btitle").textContent.trim()) card.append(el("div", "bnote", text.length > 220 ? text.slice(0, 219) + "…" : text));
  if (c.task && c.task.nudges && c.task.nudges.length) {
    const n = c.task.nudges[c.task.nudges.length - 1];
    card.append(el("div", "muted bnudged", `nudged ${clock(n.at)}`));
  }
  card.onclick = () => cardMenu(c, card);
  return card;
}

function ageText(ns) {
  const m = Math.floor((ns || 0) / 6e10);
  if (m < 1) return "now";
  if (m < 60) return `${m} min`;
  if (m < 48 * 60) return `${Math.floor(m / 60)} h`;
  return `${Math.floor(m / 1440)} d`;
}

// cardMenu offers what can be done about card c.
function cardMenu(c, anchor) {
  const a = c.owner && agents.get(c.owner);
  const items = [];
  const t = c.task;
  if (c.kind === "budget") {
    items.push({ label: "Raise by $5 today", desc: "agents go on with what waits", icon: "play", onclick: () => boardCall("/api/budget", { op: "raise", usd: 5 }) });
    items.push({ label: "Open for today", desc: "no bound until midnight", icon: "play", onclick: () => boardCall("/api/budget", { op: "open" }) });
  }
  if (c.kind === "checkin") items.push({ label: "Cancel the check-in", desc: "it was dropped or cannot go out", icon: "trash", danger: true, onclick: () => boardCall("/api/timer", { op: "rm", id: c.ref }) });
  if (c.kind === "skill") items.push({ label: "Review it", desc: "approve, edit or discard it under Skills", icon: "skill", onclick: () => openSkills() });
  if (c.why === "permission" && a) items.push({ label: `Answer ${label(a)}`, desc: "it waits on a permission", icon: "shield", onclick: () => show(a.id) });
  if (c.channel && (c.kind === "paused" || c.why === "paused")) {
    items.push({ label: "Resume the channel", desc: "it waits for you", icon: "play", onclick: () => boardCall("/api/bus/resume", { channel: c.channel }) });
  }
  if (c.kind === "ask" && c.channel) items.push({ label: "Answer", desc: c.channel.startsWith("dm.") ? "in the DM" : "in #" + c.channel, icon: "chat", onclick: () => openChannel(c.channel) });
  if (a) items.push({ label: `Open ${label(a)}`, desc: "its conversation", icon: "chat", onclick: () => show(a.id) });
  if (c.channel && c.kind !== "ask") items.push({ label: c.channel.startsWith("dm.") ? "Open the DM" : `Open #${c.channel}`, icon: "chat", onclick: () => openChannel(c.channel) });
  if (t && t.status !== "done") {
    items.push("-");
    if (a) items.push({ label: "Ask how it stands", desc: "a word to its owner, which wakes it", icon: "question", onclick: () => boardCall("/api/board/nudge", { id: t.id }) });
    items.push({ label: "Give it to…", desc: "another agent, told and woken", icon: "agent", onclick: () => reassign(t, anchor) });
    if (t.status !== "review") items.push({ label: "Mark in review", icon: "eye", onclick: () => boardCall("/api/board/task", { id: t.id, status: "review" }) });
    items.push({ label: "Mark done", icon: "check", onclick: () => boardCall("/api/board/task", { id: t.id, status: "done" }) });
    items.push({ label: "Drop it", icon: "trash", danger: true, onclick: () => boardCall("/api/board/task", { id: t.id, status: "dropped" }) });
  }
  if (!items.length) return;
  popup(anchor, items, {});
}

function reassign(t, anchor) {
  const list = [...agents.values()].filter((x) => x.chat && (!x.role || x.team) && x.id !== t.owner);
  setTimeout(() => popup(anchor, list.map((x) => ({
    label: label(x), desc: `${x.kind} · ${x.project.name}`,
    onclick: () => boardCall("/api/board/task", { id: t.id, owner: x.id }),
  })), {}), 0);
}

async function boardCall(path, body) {
  try {
    await api("POST", path, {}, body);
    await refresh();
  } catch (e) {
    toast(e.message, "err");
  }
}
