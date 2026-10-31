// The bus in the window (package bus): channels and DMs in the sidebar,
// a channel's conversation where an agent's would be, and the person
// posting in it, @mentioning the agents to wake.
"use strict";

// cv is the channel shown: its id, the last message drawn, the message
// being answered.
// lastAt is when the last message drawn was said; run is the sender of the
// messages drawn in a row (they share one name and one character); pics are
// the pictures drawn, for the channel's panel.
const cv = { open: null, last: 0, seq: 0, timer: null, paused: false, reply: null, jump: 0, lastAt: 0, run: null, pics: [] };

function busChannel(id) {
  return (deck.bus || []).find((c) => c.id === id);
}

// channelsGroup is the sidebar's list of channels and DMs: the short list
// (listed: unread, paused or yours) and the one open, the rest behind
// "All N". A search looks in all of them.
function channelsGroup(q) {
  const g = el("div", "busgroup");
  const head = el("div", "chead");
  head.append(icon("chat"), el("b", "", "群聊"));
  const plus = el("button", "icon-btn");
  plus.type = "button";
  plus.title = "a new channel or team";
  plus.append(icon("plus"));
  plus.onclick = (e) => {
    e.stopPropagation();
    popup(plus, [
      { label: "New channel", desc: "agents and you talk in it", icon: "chat", onclick: () => createChannel() },
      { label: "New team", desc: "agents in roles start together, with a lead", icon: "agent", onclick: () => newTeam() },
    ], { right: true });
  };
  const all = deck.bus || [];
  head.append(el("span", "grow"));
  head.append(plus);
  g.append(head);
  // Every channel and team chat (the server leaves out the archived) and the person's DMs, the ones
  // with a red dot first, then the newest message first (c.last_at, as the server gives it).
  const hit = new Map(all.map((c) => [c.id, chanMatch(c, q)]));
  const list = all.filter((c) => (!c.dm || c.yours) && hit.get(c.id));
  const at = (c) => Date.parse(c.last_at) || 0;
  list.sort((x, y) => chanDot(y) - chanDot(x) || at(y) - at(x));
  if (!all.length) {
    const hint = el("div", "chan none", "Talk with your agents: + makes a channel or a team.");
    hint.onclick = () => createChannel();
    g.append(hint);
  }
  for (const c of list) g.append(teamRow(c, q && hit.get(c.id) !== true ? hit.get(c.id) : null));
  return g;
}

// groupChar is a group chat's picture: a tile with up to three of its members' characters, small and
// huddled, each as it stands (a working one moves in its own way).
function groupChar(members, small) {
  const positions = [[3, 6, 24], [21, 3, 22], [12, 22, 26]];
  const mem = [...members].sort((a, b) => ({ running: 0 }[shownStatus(a)] ?? 1) - ({ running: 0 }[shownStatus(b)] ?? 1)).slice(0, 3);
  const working = members.some((a) => shownStatus(a) === "running");
  const v = el("span", `avatar chara group${small ? " small" : ""}${working ? " s-running" : ""}`);
  const minis = mem.map((a, i) => `<g class="mini m${i + 1}">${charSVG(a.id, positions[i], charExpr(charState(shownStatus(a)), a.id), charState(shownStatus(a)) === "stop")}</g>`).join("");
  v.setAttribute("role", "img");
  v.setAttribute("aria-label", working ? "working" : members.some((a) => shownStatus(a) === "waiting") ? "needs you"
    : members.some((a) => shownStatus(a) === "starting") ? "starting" : "idle");
  v.innerHTML = `<svg viewBox="0 0 48 48"><rect class="tile" x="1" y="1" width="46" height="46" rx="15"/>${minis || '<text x="24" y="31" text-anchor="middle" class="hash">#</text>'}</svg>`;
  charWake(v, () => v.querySelectorAll(".mini").forEach((m, i) => charStart(m, mem[i].id, charState(shownStatus(mem[i])), { blink: false, watch: v })));
  return v;
}

// chanDot: 1 when something in channel c waits on the person (an @you, an
// unread DM, a member with an item in the inbox), else 0.
function chanDot(c) {
  const ids = c.team ? (c.team.roles || []).map((r) => r.agent) : (c.members || []).map((m) => m.id);
  return c.mention || (c.dm && c.unread) || ids.some((id) => id && needAgents.has(id)) ? 1 : 0;
}

// chanAgents are the agents in channel c, as the page knows them.
function chanAgents(c) {
  const ids = c.team ? (c.team.roles || []).map((r) => r.agent) : (c.members || []).map((m) => m.id);
  return ids.map((id) => id && agents.get(id)).filter(Boolean);
}

// chanMatch: does channel c answer search q? A channel's members are not rows of
// their own, so one matches when a member's label, title or kind does; that
// member is returned, to be named on the row. true for a match of the channel's own.
function chanMatch(c, q) {
  if (!q || `${c.title} ${c.topic || ""}`.toLowerCase().includes(q)) return true;
  return chanAgents(c).find((a) => [label(a), a.title, a.kind].some((s) => (s || "").toLowerCase().includes(q))) || false;
}

// teamRow is a channel with its members as one row, on a phone: the group's
// picture, its name, and its last message (else how its members stand). A
// red dot when it @you or a member waits on you.
function teamRow(c, found) {
  const mem = chanAgents(c);
  const n = (s) => mem.filter((a) => shownStatus(a) === s).length;
  const parts = [n("waiting") ? `${n("waiting")} needs you` : "", n("running") ? `${n("running")} working` : ""].filter(Boolean);
  // a DM is its agent's own character; a channel, its members' tile
  const face = c.dm && mem.length ? avatar(mem[0]) : groupChar(mem);
  const lead = chanDot(c) ? withDot(face) : face;
  return row({
    key: `t:${c.id}`, cls: `teamrow${cv.open === c.id ? " sel" : ""}`,
    lead, name: c.team ? c.team.name : c.dm ? c.title.replace(/^@/, "") : c.id, line: found ? `${label(found)}: ${lastLine(found, shownStatus(found))}`
      : String(c.last_text || "").replace(/\s+/g, " ").trim() || parts.join(" · ") || c.topic || "idle",
    onclick: () => openChannel(c.id), title: c.topic || c.title,
  });
}

// openChannel shows channel id (or a DM of the person's, made with its
// first message) in place of what was shown.
// seq, if given, is a message to scroll to and mark once it is drawn.
function openChannel(id, seq) {
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
  closeSkills();
  closeBoard();
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
    cv.lastAt = 0;
    cv.run = null;
    cv.pics = [];
    replyTo(null);
    closeChanPanel(true);
    $("#chanlog").replaceChildren();
  }
  inMain(true);
  drawChanHead();
  drawSidebar();
  drawQueue();
  cv.jump = seq || 0;
  loadChannel(fresh);
  clearInterval(cv.timer);
  cv.timer = setInterval(() => loadChannel(false), 1500);
  if (!phone.matches) $("#qinput").focus();
}

function closeChannel() {
  if (!cv.open) return;
  cv.open = null;
  closeChanPanel(true);
  clearInterval(cv.timer);
  $("#chanview").hidden = true;
  replyTo(null);
}

// replyTo makes what is typed next an answer to message m (null: to none).
function replyTo(m) {
  cv.reply = m;
  const bar = $("#qreply");
  bar.hidden = !m;
  if (!m) return bar.replaceChildren();
  const x = el("button", "icon-btn");
  x.type = "button";
  x.title = "not a reply (Esc)";
  x.append(icon("x"));
  x.onclick = () => replyTo(null);
  const what = el("span", "rq");
  what.append(el("b", "", "Replying to " + (m.from.id === "you" ? "yourself" : m.from.label)), " ", snippet(m.text));
  bar.replaceChildren(icon("chat"), what, x);
  if (!phone.matches) $("#qinput").focus();
}

function snippet(text) {
  const line = (text || "").split("\n").find((l) => l.trim()) || "";
  return line.length > 120 ? line.slice(0, 119) + "…" : line;
}

// jumpTo scrolls to message seq in the channel shown, and marks it a moment.
function jumpTo(seq) {
  const t = $(`#chanlog [data-seq="${seq}"]`);
  if (!t) return toast(`#${seq} is further back`);
  t.scrollIntoView({ block: "center", behavior: "smooth" });
  t.classList.remove("flash");
  void t.offsetWidth;
  t.classList.add("flash");
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

// chanTile is the channel's avatar: avatar() draws it (a team's members
// huddled in one tile, where it does).
function chanTile(id) {
  const c = busChannel(id);
  const other = c && c.dm && c.members.length === 1 && agents.get(c.members[0].id);
  if (other) return avatar(other, true);
  return avatar({ kind: "channel", icon: "#", id, members: ((c && c.members) || []).map((m) => m.id) }, true);
}

// ---- the channel's head, and its side panel (群信息) ----

// chanName is the channel's name as the head says it, without its # or @.
function chanName(id) {
  return chanTitle(id).replace(/^[#@]/, "");
}

// chanSub is the head's one small line: how many work, or that it is quiet.
function chanSub(c) {
  if (c && c.paused) return "已暂停";
  const n = c ? chanAgents(c).filter((a) => shownStatus(a) === "running").length : 0;
  if (c && c.dm) return n ? "在工作" : "空闲";
  return n ? `${n} 个在工作` : "空闲";
}

// tileSig says how the channel's tile stands (its members' states), so a tile is
// drawn again only when that changed.
function tileSig(id) {
  const c = busChannel(id);
  return ((c && c.members) || []).map((m) => `${m.id}:${agents.has(m.id) ? shownStatus(agents.get(m.id)) : ""}`).join();
}

// chanHeadWho is the channel's tile, its name and that line, as one
// button: it opens the panel. Given the one drawn before for the same
// channel (reuse), it is updated in place and returned: the poll that draws
// this each second must not take the focus from it.
function chanHeadWho(id, reuse) {
  let who = reuse && reuse.dataset.id === id ? reuse : null;
  const c = busChannel(id);
  if (!who) {
    who = el("button", "chwho");
    who.type = "button";
    who.dataset.id = id;
    who.setAttribute("aria-haspopup", "dialog");
    who.title = "群信息";
    const what = el("span", "chwhat");
    what.append(el("b"), el("small"));
    who.append(chanTile(id), what);
    who.dataset.sig = tileSig(id);
    who.onclick = () => openChanPanel(who);
  }
  const [name, sub] = [chanName(id), chanSub(c)];
  const b = who.querySelector(".chwhat b"), small = who.querySelector(".chwhat small");
  if (b.textContent !== name) b.textContent = name;
  if (small.textContent !== sub) small.textContent = sub;
  who.setAttribute("aria-label", `${name}，${sub}`); // its name for a screen reader: the channel and how it stands
  if (who.dataset.sig !== tileSig(id)) {
    who.firstChild.replaceWith(chanTile(id));
    who.dataset.sig = tileSig(id);
  }
  return who;
}

// chanInfoButton is the ⓘ that opens the panel.
function chanInfoButton() {
  const b = el("button", "icon-btn cinfo");
  b.type = "button";
  b.title = "群信息";
  b.setAttribute("aria-label", "群信息");
  b.setAttribute("aria-haspopup", "dialog");
  b.append(icon("info"));
  b.onclick = () => openChanPanel(b);
  return b;
}

// drawChanHead draws the head of a wide window: one line. A phone's top
// bar says the same (drawTitle, drawActs).
function drawChanHead() {
  const id = cv.open;
  if (!id) return;
  const head = $("#chanhead");
  const old = head.querySelector(".chwho");
  const who = chanHeadWho(id, old);
  if (who !== old) head.replaceChildren(who, el("span", "grow"), chanInfoButton()); // another channel
  drawActs();
  if (cp.open) drawChanPanel();
}

// cp is the panel: whether it is open, which page of it, what had the focus.
const cp = { open: false, page: "", from: null, timer: 0 };

function openChanPanel(from) {
  if (!cv.open || !busChannel(cv.open) && !cv.open.startsWith("dm.")) return;
  const box = $("#chpanel");
  clearTimeout(cp.timer);
  cp.open = true;
  cp.page = "";
  cp.from = from || document.activeElement;
  box.hidden = false;
  drawChanPanel();
  void box.offsetWidth; // so the slide starts from where it was
  box.classList.add("open");
  $("#chpanel .cpclose")?.focus({ preventScroll: true });
}

// closeChanPanel slides it away; quick leaves the focus where it is.
function closeChanPanel(quick) {
  const box = $("#chpanel");
  if (!cp.open) return;
  cp.open = false;
  box.classList.remove("open");
  cp.timer = setTimeout(() => { box.hidden = true; $("#chpanel .chside").replaceChildren(); delete $("#chpanel .chside").dataset.sig; }, quick ? 0 : 300);
  if (!quick && cp.from?.isConnected) cp.from.focus({ preventScroll: true });
}

// panelRow is a row of the panel's lists: a button, its label and a small
// line beside it (key says which, for patching and for the focus).
function panelRow(key, label, small, onclick) {
  const r = el("button", "cpact");
  r.type = "button";
  r.dataset.key = key;
  const chev = el("span", "chev", "›");
  chev.setAttribute("aria-hidden", "true");
  r.append(el("span", "", label), el("small", "", small || ""), chev);
  r.onclick = onclick;
  return r;
}

// panelSig is what the panel's rows are made of: when it is the same as at the
// last draw the panel is only patched (each second the deck is drawn again),
// so what has the focus keeps it and nothing is fetched again.
function panelSig(c) {
  if (cp.page === "pics") return JSON.stringify(["pics", cv.pics.map((p) => p.url)]);
  return JSON.stringify([c.id, c.topic || "", !!c.paused, !!c.dm, c.lead || "", (c.members || []).map((m) => [m.id, m.label])]);
}

function bigTile(id) {
  const t = chanTile(id);
  t.classList.remove("small");
  t.classList.add("big");
  return t;
}

// drawChanPanel draws what the panel says of the channel: its tile, name
// and topic, its members and how they stand, then what is of the group.
function drawChanPanel() {
  const id = cv.open;
  const c = id && busChannel(id);
  const side = $("#chpanel .chside");
  if (!c) return closeChanPanel(true);
  const sig = panelSig(c);
  if (side.dataset.sig === sig) {
    if (cp.page !== "pics") patchChanPanel(side, c);
    return;
  }
  const had = side.contains(document.activeElement) ? document.activeElement.dataset.key : "";
  side.dataset.sig = sig;
  if (cp.page === "pics") drawPanelPics(side);
  else drawPanelMain(side, c);
  if (had) side.querySelector(`[data-key="${CSS.escape(had)}"]`)?.focus({ preventScroll: true });
}

function panelClose() {
  const close = el("button", "icon-btn cpclose");
  close.type = "button";
  close.dataset.key = "close";
  close.title = "关闭";
  close.setAttribute("aria-label", "关闭");
  close.append(icon("x"));
  close.onclick = () => closeChanPanel();
  return close;
}

// boardLine is the 任务板 row's small line: this channel's tasks, counted.
function boardLine(id) {
  const cards = (deck.board || []).filter((x) => x.channel === id);
  const doing = cards.filter((x) => x.column === "doing" || x.column === "review").length;
  const done = cards.filter((x) => x.column === "done").length;
  return cards.length ? `${doing} 进行中 · ${done} 完成` : "没有任务";
}

function drawPanelMain(side, c) {
  const id = c.id;
  const top = el("div", "cptop");
  top.dataset.sig = tileSig(id);
  top.append(bigTile(id), el("b", "", chanName(id)));
  if (c.topic) top.append(el("p", "cptopic", c.topic));
  const parts = [panelClose(), top];
  // the members
  const mem = c.members || [];
  parts.push(el("h3", "cpsec", `成员 · ${mem.length}`));
  const list = el("div", "cplist");
  for (const m of mem) {
    const a = agents.get(m.id);
    const r = el("button", "cprow");
    r.type = "button";
    r.dataset.key = `m:${m.id}`;
    r.disabled = !a;
    r.dataset.st = a ? shownStatus(a) : "";
    const t = el("span", "t");
    const n = el("span", "n", m.label);
    if (m.id === c.lead) n.append(" ", el("span", "tag", "负责人"));
    t.append(n, el("span", "s", a ? lastLine(a, shownStatus(a)) : "不在列表里"));
    r.append(avatar(a || { kind: m.kind, id: m.id }, false), t);
    r.onclick = () => { closeChanPanel(true); show(m.id); };
    list.append(r);
  }
  parts.push(list);
  // the group
  parts.push(el("h3", "cpsec", "这个群"));
  const acts = el("div", "cplist");
  acts.append(panelRow("board", "任务板", boardLine(id), () => { closeChanPanel(true); openBoard(id); }));
  acts.append(panelRow("pics", "图片和文件", String(cv.pics.length), () => { cp.page = "pics"; drawChanPanel(); $("#chpanel .cpclose")?.focus({ preventScroll: true }); }));
  if (c.paused) {
    acts.append(panelRow("resume", "继续", "这个群在等你", async () => {
      try {
        await api("POST", "/api/bus/resume", {}, { channel: id });
        loadChannel(false);
      } catch (e) {
        toast(e.message, "err");
      }
    }));
  }
  if (!c.dm) acts.append(panelRow("more", "更多…", "", (e) => chanMenu(c, e.currentTarget)));
  parts.push(acts);
  side.replaceChildren(...parts);
}

// patchChanPanel brings a panel of the same rows up to date: each member's state
// and line, the tile, the counts; nothing else is touched.
function patchChanPanel(side, c) {
  const top = side.querySelector(".cptop");
  if (top && top.dataset.sig !== tileSig(c.id)) {
    top.querySelector(".avatar").replaceWith(bigTile(c.id));
    top.dataset.sig = tileSig(c.id);
  }
  const rows = [...side.querySelectorAll(".cprow")];
  (c.members || []).forEach((m, i) => {
    const r = rows[i];
    if (!r) return;
    const a = agents.get(m.id);
    const st = a ? shownStatus(a) : "";
    r.disabled = !a;
    if (r.dataset.st !== st) {
      r.firstChild.replaceWith(avatar(a || { kind: m.kind, id: m.id }, false));
      r.dataset.st = st;
    }
    const line = r.querySelector(".s"), text = a ? lastLine(a, st) : "不在列表里";
    if (line.textContent !== text) line.textContent = text;
  });
  const set = (key, text) => {
    const small = side.querySelector(`.cpact[data-key="${key}"] small`);
    if (small && small.textContent !== text) small.textContent = text;
  };
  set("board", boardLine(c.id));
  set("pics", String(cv.pics.length));
}

// drawPanelPics is the panel's page of the channel's pictures.
function drawPanelPics(side) {
  const back = el("button", "cpback");
  back.type = "button";
  back.dataset.key = "back";
  back.append(el("span", "", "‹"), "图片和文件");
  back.onclick = () => { cp.page = ""; drawChanPanel(); $("#chpanel .cpclose")?.focus({ preventScroll: true }); };
  const grid = el("div", "cpgrid");
  for (const p of cv.pics) grid.append(imageButton(p.url, `图片，来自 ${p.who}`));
  if (!cv.pics.length) grid.append(el("p", "muted", "这个群里还没有图片。"));
  side.replaceChildren(panelClose(), back, grid);
}

// the panel: a tap on its scrim, Esc, or a swipe to the right closes it; a swipe
// to the left from the right edge of a phone opens it.
$("#chpanel .chscrim").onclick = () => closeChanPanel();
// trapTab keeps Tab inside the open panel: from its last button to its first, and back.
function trapTab(e) {
  const f = [...$("#chpanel .chside").querySelectorAll("button:not([disabled])")];
  if (!f.length) return;
  const at = f.indexOf(document.activeElement);
  const next = e.shiftKey ? (at <= 0 ? f.length - 1 : at - 1) : (at < 0 || at === f.length - 1 ? 0 : at + 1);
  e.preventDefault();
  f[next].focus();
}

// layerAbove: a menu, a dialog or a picture open over the panel has the keys: they close
// first, one Esc each, and the panel's own handling stands down.
const layerAbove = () => !$("#popup").hidden || !$("#dialog").hidden || !!$("#lightbox");
window.addEventListener("keydown", (e) => {
  if (!cp.open || layerAbove()) return;
  if (e.key === "Escape") {
    e.stopPropagation();
    closeChanPanel();
  } else if (e.key === "Tab") {
    trapTab(e);
  }
}, true);
{
  let from = null; // where a touch began: {x, y, edge, panel}
  document.addEventListener("touchstart", (e) => {
    const t = e.touches[0];
    from = e.touches.length === 1 ? { x: t.clientX, y: t.clientY, edge: t.clientX > innerWidth - 24, panel: !!e.target.closest?.("#chpanel") } : null;
  }, { passive: true });
  document.addEventListener("touchend", (e) => {
    if (!from) return;
    const t = e.changedTouches[0];
    const dx = t.clientX - from.x, dy = t.clientY - from.y;
    if (Math.abs(dx) > 60 && Math.abs(dx) > 2 * Math.abs(dy)) {
      if (cp.open && from.panel && dx > 0) closeChanPanel();
      else if (!cp.open && cv.open && from.edge && dx < 0 && !$("#chanview").hidden) openChanPanel($(".chwho"));
    }
    from = null;
  }, { passive: true });
}

// chanMenu offers what the person does about channel c's work: its
// progress in a line, a summary from its lead, another lead.
function chanMenu(c, anchor) {
  const lead = c.lead && agents.get(c.lead);
  const items = [{
    label: "Post the progress report", desc: "the board's line for it, from mad: it wakes no one", icon: "todo",
    onclick: async () => {
      try {
        const r = await api("POST", "/api/bus/report", {}, { channel: c.id });
        if (!r.line) toast(`Nothing open in #${c.id}`);
        loadChannel(false);
      } catch (e) {
        toast(e.message, "err");
      }
    },
  }];
  if (lead) {
    items.push({
      label: `Ask ${label(lead)} for a summary`, desc: "what is done, in progress, blocked, and waits for you", icon: "question",
      onclick: async () => {
        try {
          await api("POST", "/api/bus/post", {}, { channel: c.id, text: `@${label(lead)} please post a short progress summary of #${c.id}: what is done, what is in progress and who has it, what is blocked, and what waits for me. Keep the board current with task_set.` });
          loadChannel(false);
        } catch (e) {
          toast(e.message, "err");
        }
      },
    });
  }
  items.push({ label: "Routine here…", desc: "post in it on a schedule", icon: "play", onclick: () => routineDialog(c.id) });
  const others = (c.members || []).filter((m) => m.id !== c.lead && agents.has(m.id));
  if (others.length) {
    items.push({
      label: lead ? "Another lead…" : "Make one the lead…", desc: "the agent that answers for its work", icon: "agent",
      onclick: () => setTimeout(() => popup(anchor, others.map((m) => ({
        label: m.label, desc: "the lead",
        onclick: async () => {
          try {
            await api("POST", "/api/bus/lead", {}, { channel: c.id, agent: m.id });
            await refresh();
          } catch (e) {
            toast(e.message, "err");
          }
        },
      })), {}), 0),
    });
  }
  if (c.team) items.push(...teamItems(c));
  popup(anchor, items, {});
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
    drawMessage(m);
    cv.last = m.seq;
  }
  if (!cv.last && !$("#chanlog").children.length) {
    $("#chanlog").append(el("div", "note", id.startsWith("dm.") ? "Say something: it reaches the agent when its turn is over." : "Nothing said yet. @mention an agent to wake it."));
  }
  if (cv.paused !== r.paused) {
    cv.paused = r.paused;
    drawChanHead();
  }
  if (cv.jump) {
    const s = cv.jump;
    cv.jump = 0;
    jumpTo(s);
  } else if (atEnd) scroll.scrollTop = scroll.scrollHeight;
  if (r.messages.length) api("POST", "/api/bus/read", {}, { channel: id, seq: cv.last }).catch(() => {});
}

// ---- the messages ----

// timeLine is the centred time between messages that are far apart: today's
// as "今天 21:05", yesterday's, or the date.
function timeLine(at) {
  const d = new Date(at);
  const hm = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
  const day = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((day(new Date()) - day(d)) / 864e5);
  const when = days === 0 ? "今天" : days === 1 ? "昨天" : `${d.getMonth() + 1}月${d.getDate()}日`;
  const t = el("div", "ctime", `${when} ${hm}`);
  t.dataset.at = String(at);
  return t;
}

// drawMessage draws message m at the end of the log: the person's on the
// right, the bus's own lines in the middle, an agent's in a run with its
// other messages in a row, which share the name and the character (beside
// the last of them). A line of time comes first when ten minutes or more
// have passed.
function drawMessage(m) {
  const log = $("#chanlog");
  log.querySelector(".note")?.remove();
  if (m.kind === "system") {
    const s = el("div", "csys", m.text);
    s.dataset.seq = m.seq;
    log.append(s);
    cv.run = null;
    return;
  }
  const at = Date.parse(m.at);
  if (!isNaN(at)) {
    if (!cv.lastAt || at - cv.lastAt >= 10 * 60e3) {
      log.append(timeLine(at));
      cv.run = null;
    }
    cv.lastAt = at;
  }
  const row = messageNode(m);
  if (m.from.id === "you") {
    log.append(row);
    cv.run = null;
    return;
  }
  if (!cv.run || cv.run.from !== m.from.id) {
    // mad itself (a nudge) is m, not its kind's s.
    const a = agents.get(m.from.id) || { kind: m.from.kind || "?", id: m.from.id, icon: m.from.id === "mad" ? "m" : "" };
    const v = avatar(a, true);
    if (agents.has(m.from.id)) {
      v.title = "open it";
      v.onclick = () => show(m.from.id);
    }
    const stack = el("div", "cstack");
    stack.append(el("div", "cname", m.from.label));
    const grp = el("div", "cgrp");
    grp.append(v, stack);
    log.append(grp);
    cv.run = { from: m.from.id, stack };
  }
  cv.run.stack.append(row);
}

// messageNode is one message: a bubble; or, for what an agent did or reports,
// one thin line, opened in place by a tap; its pictures; the message it answers,
// quoted. Reply is on hover, and in a menu on a long press.
function messageNode(m) {
  const mine = m.from.id === "you";
  const row = el("div", `cmsg${mine ? " mine" : ""}`);
  row.dataset.seq = m.seq;
  row.dataset.who = mine ? "你" : m.from.label;
  const text = el("div", "ctext");
  text.append(md(mentionsBold(m.text || "")));
  const quote = m.reply_to ? quoteOf(m.reply_to) : null;
  if (m.reply_to) row.dataset.reply = m.reply_to;
  let imgs = null;
  if (m.images && m.images.length) {
    const who = mine ? "你" : m.from.label;
    const urls = m.images.map((im) => `/api/bus/image?${new URLSearchParams({ id: im.id })}`);
    urls.forEach((url) => cv.pics.push({ url, who, seq: m.seq }));
    imgs = imageRow(urls, who);
  }
  const folded = foldSummary(m);
  if (folded) {
    row.classList.add("fold");
    const line = el("button", "cline");
    line.type = "button";
    line.setAttribute("aria-expanded", "false");
    const sum = el("span", "csum");
    const head = foldHead(m);
    if (head) sum.append(el("b", "", head), "：");
    sum.append(folded);
    const chev = el("span", "chev", "›");
    chev.setAttribute("aria-hidden", "true");
    line.append(sum, chev);
    line.onclick = () => line.setAttribute("aria-expanded", String(row.classList.toggle("open")));
    const full = el("div", "cfull");
    if (quote) full.append(quote);
    if (m.text) full.append(text);
    if (imgs) full.append(imgs);
    const h = m.handoff;
    if (h) full.append(el("div", "cnote", `${h.id} · ${h.status || ""}`));
    row.append(line, full);
  } else {
    if (quote || m.text) {
      const bub = el("div", "cbub");
      if (quote) bub.append(quote);
      if (m.text) bub.append(text);
      row.append(bub);
    }
    if (imgs) row.append(imgs);
  }
  const re = el("button", "creply");
  re.type = "button";
  re.title = "回复";
  re.setAttribute("aria-label", "回复");
  re.append(icon("chat"));
  re.onclick = () => replyTo(m);
  row.append(re);
  pressMenu(row, m);
  return row;
}

// pressMenu gives a message its menu, on a long press (a phone) or a right
// click: reply, or copy what it says.
function pressMenu(row, m) {
  const open = (x, y) => popup({ x, y }, [
    { label: "回复", icon: "chat", onclick: () => replyTo(m) },
    { label: "复制", icon: "copy", disabled: !m.text, onclick: () => copyText(m.text) },
  ], {});
  let timer = 0, sx = 0, sy = 0;
  const cancel = () => { clearTimeout(timer); timer = 0; };
  row.addEventListener("touchstart", (e) => {
    if (e.touches.length !== 1) return;
    sx = e.touches[0].clientX;
    sy = e.touches[0].clientY;
    timer = setTimeout(() => { timer = 0; popupGuard = Date.now() + 600; open(sx, sy); }, 450);
  }, { passive: true });
  row.addEventListener("touchmove", (e) => {
    const t = e.touches[0];
    if (Math.hypot(t.clientX - sx, t.clientY - sy) > 10) cancel();
  }, { passive: true });
  row.addEventListener("touchend", cancel);
  row.addEventListener("touchcancel", cancel);
  row.addEventListener("contextmenu", (e) => {
    e.preventDefault();
    open(e.clientX, e.clientY);
  });
}

// copyText puts text on the clipboard, and says so.
async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    toast("已复制");
  } catch (_) {
    toast("不能复制", "err");
  }
}

// foldHead is the bold start of a folded line: whom a handoff is to, or that
// a result is done (or refused); none for other long messages.
function foldHead(m) {
  const h = m.handoff;
  if (m.kind === "handoff") return `→ ${agentLabel(h && h.to)}`;
  if (m.kind === "done") return h && h.status === "declined" ? "已拒绝" : "完成";
  return "";
}

// foldSummary is the line a message folds to in a channel, "" when it is
// shown whole: an agent's handoff, its result, and what runs past three
// lines. The person's own, mad's, a message that @mentions the person, and
// whatever an agent says in its DM with the person (all of it addressed to
// them) stay whole.
function foldSummary(m) {
  if (m.from.id === "you" || m.from.id === "mad" || m.kind === "system") return "";
  const c = busChannel(cv.open);
  if (c && c.dm && c.yours) return "";
  const text = (m.text || "").trim();
  if (/(^|[\s(（])@you(?![\w-])/i.test(text)) return "";
  const lines = text.split("\n").filter((l) => l.trim());
  const card = m.kind === "handoff" || m.kind === "done";
  if (!card && lines.length <= 3 && text.length <= 280) return "";
  // The line's bold start says whom a handoff is to, or how it ended (foldHead).
  return firstSentence(lines[0] || "");
}

// firstSentence is the start of a line with its markdown taken off, cut at
// the end of its first sentence, or at 120 characters.
function firstSentence(line) {
  const plain = line.replace(/^[#>\-*\s]+/, "").replace(/[*_`]/g, "")
    // a link to a pull request is its name; one after the word PR is not "PR PR #88"
    .replace(/(\bPR\s+)?https?:\/\/\S+?\/pull\/(\d+)\S*/gi, (_, pr, n) => `PR #${n}`)
    .replace(/https?:\/\/([^\s/,;)]+)\S*/g, "$1").trim();
  const end = plain.search(/[.!?。！？](\s|$)/);
  const one = end > 0 ? plain.slice(0, end + 1) : plain;
  return one.length > 120 ? one.slice(0, 119) + "…" : one;
}

// noWake tells whether text, posted in the open channel, would wake no
// one: no @all and no @agent, as the bus counts them (a DM always wakes
// the other side).
function noWake(text) {
  if (!text.trim() || !cv.open || cv.open.startsWith("dm.")) return false;
  const names = [...agents.values()].map((a) => label(a)).concat(["all"]).sort((x, y) => y.length - x.length);
  const esc = (x) => x.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const re = new RegExp("(^|[\\s(\\[{<\"'*,;])@(" + names.map(esc).join("|") + ")(?![\\w-])", "i");
  return !re.test(text);
}

// drawNoWake says beside the send button when the draft would wake no one.
function drawNoWake() {
  const who = $("#who");
  if (!who) return;
  let h = who.querySelector(".nowake");
  if (!noWake($("#qinput").value)) return h && h.remove();
  if (!h) {
    h = el("span", "nowake", "no one will be woken");
    h.title = "No @mention in it: agents see it only when they read the channel.";
    who.append(h);
  }
}
$("#qinput").addEventListener("input", drawNoWake);

// quoteOf is what a reply answers: the message, quoted in one line, which a
// click shows.
function quoteOf(seq) {
  const q = el("button", "cquote");
  q.type = "button";
  const parent = $(`#chanlog [data-seq="${seq}"]`);
  if (parent) {
    q.append(el("b", "", parent.dataset.who || ""), " ", snippet(parent.querySelector(".ctext")?.textContent || parent.textContent));
  } else {
    q.append("↩ 更早的消息");
  }
  q.title = "show what it answers";
  q.onclick = () => jumpTo(seq);
  return q;
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
  const imgs = takeImages();
  t.value = "";
  fitInput();
  drawNoWake();
  try {
    const body = { channel: id, text, images: imgs.map(({ mime, data }) => ({ mime, data })) };
    if (cv.reply) body.reply_to = cv.reply.seq;
    const r = await api("POST", "/api/bus/post", {}, body);
    replyTo(null);
    if (r && r.warning) toast(r.warning, "err");
    $("#qerr").textContent = "";
  } catch (e) {
    if (!t.value) t.value = text;
    restoreImages(imgs);
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
  drawNoWake();
}

// drawAgentMenu offers the agents as @ is typed in a channel: its
// members first.
function drawAgentMenu(w) {
  const c = busChannel(cv.open);
  const members = new Set(((c && c.members) || []).map((m) => m.id));
  const q = w.query.toLowerCase();
  const all = [...agents.values()].filter((a) => a.chat && (!a.role || a.team))
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
  for (const a of [...agents.values()].filter((x) => x.chat && (!x.role || x.team))) {
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
