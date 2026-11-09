// The task board in the window (bus.Board, with the snapshot): what
// waits for you first, then what is being done, blocked, in review, to
// do, and done today; a card's menu does what it needs.
"use strict";

// channel: only the cards of that channel (from its panel).
const bd = { open: false, key: "", channel: "" };

const boardColumns = [
  ["waiting-you", T("在等你", "Waiting")], ["doing", T("在做", "Doing")], ["blocked", T("卡住了", "Stuck")],
  ["review", T("待检查", "To check")], ["todo", T("待办", "To do")], ["done", T("今天做完的", "Done today")],
];

// Why a card is blocked; what waits on you is the inbox's (inbox.js).
const boardWhy = {
  waits: T("在等另一个任务", "Waits on another task"), lapsed: T("交接过期了", "Handoff overdue"), stalled: T("停住了", "Stalled"), permission: T("在等许可", "Waits for permission"),
};

function openBoard(channel) {
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
  closeUsage();
  closeChanges();
  closeMenu();
  closePopup();
  $("#empty").style.display = "none";
  bd.open = true;
  bd.channel = channel || "";
  bd.key = "";
  $("#boardview").hidden = false;
  inMain(true);
  drawBoard();
  drawSidebar();
  drawQueue();
}

function closeBoard() {
  closeUsage();
  if (!bd.open) return;
  bd.open = false;
  bd.channel = "";
  $("#boardview").hidden = true;
  $("#boardview").replaceChildren();
  bd.key = "";
  $("#empty").style.display = "";
}

function drawBoard() {
  if (bd.open) keepFocus(drawBoardNow);
}

function drawBoardNow() {
  const cards = (deck.board || []).filter((c) => !bd.channel || c.channel === bd.channel);
  const waiting = bd.channel ? [] : needsYou();
  const key = bd.channel + JSON.stringify(cards.map((c) => [c.kind, c.task && c.task.id, c.column, c.why, c.owner, c.note, c.message && c.message.seq, Math.floor(c.age / 6e10)]))
    + liveKey()
    + JSON.stringify(waiting.map((it) => [it.key, it.context, it.text, it.acts.length])) + JSON.stringify(ib.reply) + ib.nextKey + JSON.stringify(deck.spend || {})
    + JSON.stringify((deck.timers || []).map((t) => [t.id, t.next_at, t.paused, t.held, t.why, t.fired, t.missed]));
  if (key === bd.key) return;
  bd.key = key;
  const root = $("#boardview");
  const head = el("div", "bhead");
  const inColumn = (col) => col === "waiting-you" ? waiting.length : cards.filter((c) => c.column === col).length;
  const counts = boardColumns.map(([col, name]) => [name, inColumn(col)]).filter(([, n]) => n);
  head.append(el("h1", "", bd.channel ? T(`看板 · #${bd.channel}`, `Board · #${bd.channel}`) : T("看板", "Board")), el("p", "muted", counts.length ? counts.map(([n, k]) => T(`${k} 个${n}`, `${k} ${n.toLowerCase()}`)).join(" · ") : T("看板上还没有任务：交接和 agent 的 task_set 会把任务放到这里。", "No tasks on the board yet: handoffs and the agents' task_set put them here.")));
  const spend = spendLine(deck.spend);
  const cols = el("div", "bcols");
  for (const [col, name] of boardColumns) {
    const list = col === "waiting-you" ? waiting : cards.filter((c) => c.column === col);
    const box = el("section", `bcol c-${col}${list.length ? "" : " empty"}`);
    const h = el("h2", "");
    h.append(name, el("span", "muted", list.length ? ` ${list.length}` : ""));
    box.append(h);
    if (!list.length) box.append(el("p", "muted bnone", col === "waiting-you" ? T("没有在等你的", "Nothing waits on you") : T("这里没有", "Nothing here")));
    for (const c of list) box.append(col === "waiting-you" ? itemCard(c, true) : boardCard(c));
    cols.append(box);
  }
  root.replaceChildren(...[head, spend, timerStrip(), cols].filter(Boolean));
}

// spendLine is what mad's own work cost today against its budget, with a
// thin bar; amber from 80% of it, red once it is spent and agents wait.
function spendLine(s) {
  if (!s || !s.day || (!s.usd && !s.turns)) return null;
  const usd = `$${s.usd.toFixed(2)}` + (s.budget_usd ? ` / $${+s.budget_usd.toFixed(2)}` : "");
  const turns = `${s.turns}` + (s.budget_turns ? ` / ${s.budget_turns}` : "") + T(" 轮", " turns");
  const part = s.part || 0;
  const box = el("div", `bspend${s.closed ? " closed" : part >= 0.8 ? " hot" : ""}`);
  const bar = el("div", "bbar");
  const fill = el("div", "bfill");
  fill.style.width = `${Math.min(100, Math.round(part * 100))}%`;
  bar.append(fill);
  const note = s.closed ? T(" · agent 在等", " · agents wait") : s.open_today ? T(" · 今天不限", " · no limit today") : "";
  box.append(el("span", "muted", T(`mad 自己今天的花费：${usd} · ${turns}${note}`, `mad's own spend today: ${usd} · ${turns}${note}`)), bar);
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
    meta.append(el("span", "pill quiet", T("你", "You")));
  } else if (c.owner_label) {
    meta.append(el("span", "", c.owner_label));
  }
  if (c.channel) meta.append(el("span", "muted", c.channel.startsWith("dm.") ? "DM" : "#" + c.channel));
  meta.append(el("span", "muted", ageText(c.age)));
  card.append(meta);
  if (c.why) card.append(el("div", "bwhy", c.why === "waits" && c.waits && c.waits.length ? T(`在等 ${c.waits.join("、")}`, `Waits on ${c.waits.join(", ")}`) : boardWhy[c.why] || c.why));
  // An ask's message is what its title does not already say (bus.Card).
  const text = (c.message && c.message.text) || c.note;
  if (text && text.trim() !== card.querySelector(".btitle").textContent.trim()) card.append(el("div", "bnote", text.length > 220 ? text.slice(0, 219) + "…" : text));
  if (c.task && c.task.nudges && c.task.nudges.length) {
    const n = c.task.nudges[c.task.nudges.length - 1];
    card.append(el("div", "muted bnudged", T(`${clock(n.at)} 催过`, `nudged at ${clock(n.at)}`)));
  }
  card.onclick = () => cardMenu(c, card);
  return card;
}

function ageText(ns) {
  const m = Math.floor((ns || 0) / 6e10);
  if (m < 1) return T("刚刚", "just now");
  if (m < 60) return T(`${m} 分钟`, `${m} min`);
  if (m < 48 * 60) return T(`${Math.floor(m / 60)} 小时`, `${Math.floor(m / 60)} h`);
  return T(`${Math.floor(m / 1440)} 天`, `${Math.floor(m / 1440)} d`);
}

// cardMenu offers what can be done about card c.
function cardMenu(c, anchor) {
  const a = c.owner && agents.get(c.owner);
  const items = [];
  const t = c.task;
  if (a) items.push({ label: T(`打开 ${label(a)}`, `Open ${label(a)}`), desc: T("它的对话", "Its chat"), icon: "chat", onclick: () => show(a.id) });
  if (c.channel) items.push({ label: c.channel.startsWith("dm.") ? T("打开私信", "Open DM") : T(`打开 #${c.channel}`, `Open #${c.channel}`), icon: "chat", onclick: () => openChannel(c.channel) });
  if (t && t.status !== "done") {
    items.push("-");
    if (a) items.push({ label: T("问问进度", "Ask how it goes"), desc: T("给负责人说一声，会叫醒它", "Tells its owner, which wakes it"), icon: "question", onclick: () => boardCall("/api/board/nudge", { id: t.id }) });
    items.push({ label: T("交给别人…", "Hand to someone else…"), desc: T("另一个 agent，会通知并叫醒它", "Another agent, told and woken"), icon: "agent", onclick: () => reassign(t, anchor) });
    if (t.status !== "review") items.push({ label: T("标为待检查", "Mark to check"), icon: "eye", onclick: () => boardCall("/api/board/task", { id: t.id, status: "review" }) });
    items.push({ label: T("标为做完", "Mark done"), icon: "check", onclick: () => boardCall("/api/board/task", { id: t.id, status: "done" }) });
    items.push({ label: T("放弃它", "Drop it"), icon: "trash", danger: true, onclick: () => boardCall("/api/board/task", { id: t.id, status: "dropped" }) });
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
