// The bus in the window (package bus): channels and DMs in the sidebar,
// a channel's conversation where an agent's would be, and the person
// posting in it, @mentioning the agents to wake.
"use strict";

// cv is the channel shown: its id, the last message drawn, the message
// being answered.
const cv = { open: null, last: 0, seq: 0, timer: null, paused: false, reply: null };

function busChannel(id) {
  return (deck.bus || []).find((c) => c.id === id);
}

// channelsGroup is the sidebar's list of channels and DMs: the short list
// (listed: unread, paused or yours) and the one open, the rest behind
// "All N". A search looks in all of them.
function channelsGroup(q) {
  const g = el("div", "busgroup");
  const head = el("div", "chead");
  head.append(icon("chat"), el("b", "", "Channels"));
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
  if (all.length) {
    const more = button(ib.allChans ? "Fewer" : `All ${all.length}`, "ghost ga", () => { ib.allChans = !ib.allChans; drawSidebar(); });
    more.title = "every channel and DM";
    head.append(more);
  }
  head.append(plus);
  g.append(head);
  const list = all.filter((c) => q ? `${c.title} ${c.topic || ""}`.toLowerCase().includes(q) : ib.allChans || c.listed || c.id === cv.open);
  if (!all.length) {
    const hint = el("div", "chan none", "Talk with your agents: + makes a channel or a team.");
    hint.onclick = () => createChannel();
    g.append(hint);
  }
  for (const c of list) {
    const row = el("div", `chan${cv.open === c.id ? " sel" : ""}${c.unread ? " unread" : ""}${c.dm && !c.yours ? " theirs" : ""}`);
    row.title = c.topic || c.title;
    row.append(el("span", "ch", c.dm ? (c.yours ? "@" : "↔") : "#"),
      el("span", "cn", c.dm ? c.title.replace(/^@/, "") : c.id));
    if (c.paused) row.append(el("span", "pill needs", "paused"));
    else if (c.unread) row.append(el("span", `cnt${c.mention ? " at" : ""}`, c.mention ? `@${c.unread}` : String(c.unread)));
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
    replyTo(null);
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

function drawChanHead() {
  const id = cv.open;
  if (!id) return;
  const c = busChannel(id);
  const head = $("#chanhead");
  const what = el("div", "cwhat");
  what.append(el("b", "", chanTitle(id)));
  if (c && c.topic) what.append(el("span", "muted", c.topic));
  const lead = c && c.lead && agents.get(c.lead);
  if (c && c.team) what.append(teamLine(c.team));
  else if (lead) what.append(el("span", "muted", "lead " + label(lead)));
  const who = el("div", "cwho");
  for (const m of (c && c.members) || []) {
    const a = agents.get(m.id) || { kind: m.kind, id: m.id };
    const v = avatar(a, true);
    v.title = m.id === (c && c.lead) ? m.label + ", the lead" : m.label;
    if (m.id === (c && c.lead)) v.classList.add("lead");
    v.onclick = () => show(m.id);
    who.append(v);
  }
  const parts = [what, el("span", "grow"), who];
  if (c && !c.dm) {
    const more = el("button", "icon-btn chmore"); // on a phone the top bar has it
    more.type = "button";
    more.title = "the channel's work";
    more.append(icon("more"));
    more.onclick = () => chanMenu(c, more);
    parts.push(more);
  }
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
  // On a phone the top bar names it and holds the menu: the head shows
  // only what more there is (a topic, a pause, a team or a lead).
  head.classList.toggle("bare", !(c && (c.topic || c.paused || c.team || lead)));
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
    $("#chanlog").append(busMessage(m));
    cv.last = m.seq;
  }
  if (!cv.last && !$("#chanlog").children.length) {
    $("#chanlog").append(el("div", "note", id.startsWith("dm.") ? "Say something: it reaches the agent when its turn is over." : "Nothing said yet. @mention an agent to wake it."));
  }
  drawReceipts(r.receipts || [], r.paused);
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
  if (m.kind === "system") {
    const s = el("div", "bsys", m.text);
    s.dataset.seq = m.seq;
    return s;
  }
  $("#chanlog .note")?.remove();
  const mine = m.from.id === "you";
  const card = m.kind === "handoff" || m.kind === "done";
  const row = el("div", `bmsg${mine ? " mine" : ""}${card ? " card" : ""}`);
  row.dataset.seq = m.seq;
  if (!mine) {
    // mad itself (a nudge) is m, not its kind's s.
    const a = agents.get(m.from.id) || { kind: m.from.kind || "?", id: m.from.id, icon: m.from.id === "mad" ? "m" : "" };
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
  const re = el("button", "breply");
  re.type = "button";
  re.title = "reply to this";
  re.append(icon("chat"), "Reply");
  re.onclick = () => replyTo(m);
  meta.append(re);
  const h = m.handoff;
  if (m.kind === "handoff") meta.append(el("span", "pill working", `hands ${h ? h.id + " " : ""}to ${agentLabel(h && h.to)}`));
  if (m.kind === "done") meta.append(el("span", `pill ${h && h.status === "declined" ? "needs" : "done"}`, `${h ? h.id + " " : ""}${(h && h.status) || "done"}`));
  const text = el("div", "btext");
  text.append(md(mentionsBold(m.text || "")));
  body.append(meta);
  if (m.reply_to) {
    row.dataset.reply = m.reply_to;
    body.append(quoteOf(m.reply_to));
  }
  body.append(text);
  row.append(body);
  return row;
}

// drawReceipts puts under each of your messages who it woke and how far
// it got: ◷ waiting (not yet in the agent's digest), ✓ delivered, ✓✓ read,
// ↩ replied; or that it reached no one.
function drawReceipts(list, paused) {
  const mark = { waiting: "◷", delivered: "✓", read: "✓✓", replied: "↩" };
  for (const rc of list) {
    const row = $(`#chanlog .bmsg.mine[data-seq="${rc.seq}"]`);
    if (!row) continue;
    let line = row.querySelector(".brcpt");
    if (!line) {
      line = el("div", "brcpt");
      row.querySelector(".bbody").append(line);
    }
    const parts = [];
    for (const st of ["replied", "read", "delivered", "waiting"]) {
      const names = rc.targets.filter((t) => t.state === st).map((t) => t.label);
      if (names.length) parts.push(`${mark[st]} ${names.join(", ")} ${st}`);
    }
    const none = !rc.targets.length;
    let text = none ? "reached no agent — @ someone to wake them" : parts.join(" · ");
    if (paused && rc.targets.some((t) => t.state === "waiting")) text += " · held: the channel is paused";
    line.classList.toggle("none", none);
    if (line.textContent !== text) line.textContent = text;
    line.title = none
      ? "A line with no @mention wakes no one; agents see it only when they read the channel."
      : "◷ waiting · ✓ delivered to the agent · ✓✓ read · ↩ replied";
  }
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

// quoteOf is what a reply answers: the message, quoted, which a click
// shows; and the message counts its replies.
function quoteOf(seq) {
  const q = el("button", "bquote");
  q.type = "button";
  const parent = $(`#chanlog [data-seq="${seq}"]`);
  if (parent) {
    const who = parent.querySelector(".bmeta b")?.textContent || "";
    const said = parent.querySelector(".btext")?.textContent || parent.textContent;
    q.append(el("b", "", who || `#${seq}`), " ", snippet(said));
    let n = parent.querySelector(".bcount");
    if (!n) {
      n = el("button", "bcount");
      n.type = "button";
      n.dataset.n = "0";
      n.onclick = () => {
        const replies = $("#chanlog").querySelectorAll(`[data-reply="${seq}"]`);
        if (replies.length) jumpTo(replies[replies.length - 1].dataset.seq);
      };
      (parent.querySelector(".bbody") || parent).append(n);
    }
    n.dataset.n = String(+n.dataset.n + 1);
    n.textContent = `${n.dataset.n} repl${n.dataset.n === "1" ? "y" : "ies"}`;
  } else {
    q.append(`↩ #${seq}`);
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
  t.value = "";
  fitInput();
  drawNoWake();
  try {
    const r = await api("POST", "/api/bus/post", {}, cv.reply ? { channel: id, text, reply_to: cv.reply.seq } : { channel: id, text });
    replyTo(null);
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
