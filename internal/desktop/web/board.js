// The task board in the window (bus.Board, with the snapshot): what
// waits for you first, then what is being done, blocked, in review, to
// do, and done today; a card's menu does what it needs.
"use strict";

// channel: only the cards of that channel (from its panel).
const bd = { open: false, key: "", channel: "" };

const boardColumns = [
  ["waiting-you", "在等你"], ["doing", "在做"], ["blocked", "卡住了"],
  ["review", "待检查"], ["todo", "待办"], ["done", "今天做完的"],
];

// Why a card is blocked; what waits on you is the inbox's (inbox.js).
const boardWhy = {
  waits: "在等另一个任务", lapsed: "交接过期了", stalled: "停住了", permission: "在等许可",
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
    + JSON.stringify([...agents.values()].map((a) => a.shown))
    + JSON.stringify(waiting.map((it) => [it.key, it.context, it.text, it.acts.length])) + JSON.stringify(ib.reply) + ib.nextKey + JSON.stringify(deck.spend || {})
    + JSON.stringify((deck.timers || []).map((t) => [t.id, t.next_at, t.paused, t.held, t.why, t.fired, t.missed]));
  if (key === bd.key) return;
  bd.key = key;
  const root = $("#boardview");
  const head = el("div", "bhead");
  const inColumn = (col) => col === "waiting-you" ? waiting.length : cards.filter((c) => c.column === col).length;
  const counts = boardColumns.map(([col, name]) => [name, inColumn(col)]).filter(([, n]) => n);
  head.append(el("h1", "", bd.channel ? `看板 · #${bd.channel}` : "看板"), el("p", "muted", counts.length ? counts.map(([n, k]) => `${k} 个${n}`).join(" · ") : "看板上还没有任务：交接和 agent 的 task_set 会把任务放到这里。"));
  const spend = spendLine(deck.spend);
  const cols = el("div", "bcols");
  for (const [col, name] of boardColumns) {
    const list = col === "waiting-you" ? waiting : cards.filter((c) => c.column === col);
    const box = el("section", `bcol c-${col}${list.length ? "" : " empty"}`);
    const h = el("h2", "");
    h.append(name, el("span", "muted", list.length ? ` ${list.length}` : ""));
    box.append(h);
    if (!list.length) box.append(el("p", "muted bnone", col === "waiting-you" ? "没有在等你的" : "这里没有"));
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
  const turns = `${s.turns}` + (s.budget_turns ? ` / ${s.budget_turns}` : "") + " 轮";
  const part = s.part || 0;
  const box = el("div", `bspend${s.closed ? " closed" : part >= 0.8 ? " hot" : ""}`);
  const bar = el("div", "bbar");
  const fill = el("div", "bfill");
  fill.style.width = `${Math.min(100, Math.round(part * 100))}%`;
  bar.append(fill);
  const note = s.closed ? " · agent 在等" : s.open_today ? " · 今天不限" : "";
  box.append(el("span", "muted", `mad 自己今天的花费：${usd} · ${turns}${note}`), bar);
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
    meta.append(el("span", "pill quiet", "你"));
  } else if (c.owner_label) {
    meta.append(el("span", "", c.owner_label));
  }
  if (c.channel) meta.append(el("span", "muted", c.channel.startsWith("dm.") ? "DM" : "#" + c.channel));
  meta.append(el("span", "muted", ageText(c.age)));
  card.append(meta);
  if (c.why) card.append(el("div", "bwhy", c.why === "waits" && c.waits && c.waits.length ? `在等 ${c.waits.join("、")}` : boardWhy[c.why] || c.why));
  // An ask's message is what its title does not already say (bus.Card).
  const text = (c.message && c.message.text) || c.note;
  if (text && text.trim() !== card.querySelector(".btitle").textContent.trim()) card.append(el("div", "bnote", text.length > 220 ? text.slice(0, 219) + "…" : text));
  if (c.task && c.task.nudges && c.task.nudges.length) {
    const n = c.task.nudges[c.task.nudges.length - 1];
    card.append(el("div", "muted bnudged", `${clock(n.at)} 催过`));
  }
  card.onclick = () => cardMenu(c, card);
  return card;
}

function ageText(ns) {
  const m = Math.floor((ns || 0) / 6e10);
  if (m < 1) return "刚刚";
  if (m < 60) return `${m} 分钟`;
  if (m < 48 * 60) return `${Math.floor(m / 60)} 小时`;
  return `${Math.floor(m / 1440)} 天`;
}

// cardMenu offers what can be done about card c.
function cardMenu(c, anchor) {
  const a = c.owner && agents.get(c.owner);
  const items = [];
  const t = c.task;
  if (a) items.push({ label: `打开 ${label(a)}`, desc: "它的对话", icon: "chat", onclick: () => show(a.id) });
  if (c.channel) items.push({ label: c.channel.startsWith("dm.") ? "打开私信" : `打开 #${c.channel}`, icon: "chat", onclick: () => openChannel(c.channel) });
  if (t && t.status !== "done") {
    items.push("-");
    if (a) items.push({ label: "问问进度", desc: "给负责人说一声，会叫醒它", icon: "question", onclick: () => boardCall("/api/board/nudge", { id: t.id }) });
    items.push({ label: "交给别人…", desc: "另一个 agent，会通知并叫醒它", icon: "agent", onclick: () => reassign(t, anchor) });
    if (t.status !== "review") items.push({ label: "标为待检查", icon: "eye", onclick: () => boardCall("/api/board/task", { id: t.id, status: "review" }) });
    items.push({ label: "标为做完", icon: "check", onclick: () => boardCall("/api/board/task", { id: t.id, status: "done" }) });
    items.push({ label: "放弃它", icon: "trash", danger: true, onclick: () => boardCall("/api/board/task", { id: t.id, status: "dropped" }) });
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
