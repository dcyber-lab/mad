// The bus in the window (package bus): channels and DMs in the sidebar,
// a channel's conversation where an agent's would be, and the person
// posting in it, @mentioning the agents to wake.
"use strict";

// cv is the channel shown: its id, the last message drawn, the message
// being answered.
const cv = { open: null, last: 0, seq: 0, timer: null, paused: false, reply: null, jump: 0 };

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

// groupChar is a group chat's picture, on a phone: a tile with up to three
// of its members' characters, small and huddled. They hop in turn while any
// member works.
function groupChar(members, small) {
  const positions = [[3, 6, 24], [21, 3, 22], [12, 22, 26]];
  const mem = [...members].sort((a, b) => ({ running: 0 }[shownStatus(a)] ?? 1) - ({ running: 0 }[shownStatus(b)] ?? 1)).slice(0, 3);
  const working = members.some((a) => shownStatus(a) === "running");
  const v = el("span", `avatar chara group${small ? " small" : ""}${working ? " s-running" : ""}`);
  const minis = mem.map((a, i) => `<g class="mini m${i + 1}">${charSVG(a.id, positions[i])}</g>`).join("");
  v.setAttribute("role", "img");
  v.setAttribute("aria-label", working ? "working" : members.some((a) => shownStatus(a) === "waiting") ? "needs you" : "idle");
  v.innerHTML = `<svg viewBox="0 0 48 48"><rect class="tile" x="1" y="1" width="46" height="46" rx="15"/>${minis || '<text x="24" y="31" text-anchor="middle" class="hash">#</text>'}</svg>`;
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
  const lead = c.dm && mem.length ? avatar(mem[0]) : groupChar(mem);
  if (chanDot(c)) lead.append(redDot());
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
    replyTo(null);
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
  $("#bar .cwho")?.remove();
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
  return avatar({ kind: "channel", icon: "#", id, members: ((c && c.members) || []).map((m) => m.id) }, true);
}

function drawChanHead() {
  const id = cv.open;
  if (!id) return;
  const c = busChannel(id);
  const head = $("#chanhead");
  // The channel's tile, its name and its topic small beside them (the
  // lead is in the members' list); a team's progress under them.
  const what = el("div", "cwhat");
  const ttl = el("div", "cttl");
  ttl.append(chanTile(id), el("b", "", chanTitle(id).replace(/^[#@]/, "")));
  if (c && c.topic) ttl.append(el("span", "muted", c.topic));
  what.append(ttl);
  if (c && c.team) what.append(teamLine(c.team));
  // The members, in one place: a strip of their avatars (which show who
  // works), and a tap on it lists them with their state, to open one.
  const members = (c && c.members) || [];
  const parts = [what, el("span", "grow")];
  const ptype = document.documentElement.classList.contains("ptype");
  $("#bar .cwho")?.remove();
  if (members.length) {
    const who = el("button", "cwho");
    who.type = "button";
    who.title = `${members.length} agent${members.length === 1 ? "" : "s"}: see who is doing what`;
    for (const m of members.slice(0, STRIP_MAX)) {
      const a = agents.get(m.id) || { kind: m.kind, id: m.id };
      const v = avatar(a, true);
      if (m.id === c.lead) v.classList.add("lead");
      who.append(v);
    }
    if (members.length > STRIP_MAX) who.append(el("span", "cmore", `+${members.length - STRIP_MAX}`));
    who.onclick = () => memberMenu(c, who);
    // On a phone and in the app the strip is in the top bar, at its right
    // end (back, the channel's tile and name, the members); elsewhere in
    // the channel's own head.
    if (ptype) $("#bar").insertBefore(who, $("#acts"));
    else parts.push(who);
  }
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
  // On a phone and in the app the head has no more than the top bar has,
  // unless there is a topic or a pause to say.
  head.classList.toggle("bare", !(c && (c.topic || c.paused || (!ptype && (c.team || members.length)))));
}

// STRIP_MAX is how many avatars the members' strip shows before "+n".
const STRIP_MAX = 6;

// memberMenu lists channel c's members with how each stands, and opens the
// one picked.
function memberMenu(c, anchor) {
  popup(anchor, c.members.map((m) => {
    const a = agents.get(m.id);
    const state = a ? look(shownStatus(a)).word : "not in the deck";
    const parts = [state];
    if (a && a.role) parts.push(a.role);
    if (m.id === c.lead) parts.push("the lead");
    return { label: m.label, desc: parts.join(" · "), disabled: !a, onclick: () => show(m.id) };
  }), { right: true });
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
  if (cv.jump) {
    const s = cv.jump;
    cv.jump = 0;
    jumpTo(s);
  } else if (atEnd) scroll.scrollTop = scroll.scrollHeight;
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
  if (m.reply_to) {
    row.dataset.reply = m.reply_to;
    body.append(quoteOf(m.reply_to));
  }
  // What an agent did or reports is a line, opened where it is by a tap;
  // what the person wrote, or what is for the person, is shown whole.
  const folded = foldSummary(m);
  let less = null;
  if (folded) {
    row.classList.add("fold");
    const toggle = () => {
      const open = row.classList.toggle("open");
      more.setAttribute("aria-expanded", open);
    };
    const sum = el("div", "bsum");
    const more = el("button", "bmore", "Show more");
    more.type = "button";
    more.onclick = toggle;
    sum.append(el("span", "bsumt", folded), more);
    sum.onclick = (e) => e.target === more || toggle();
    body.append(sum);
    less = el("button", "bless", "Show less");
    less.type = "button";
    less.onclick = toggle;
  }
  if (m.text) body.append(text);
  // Pictures, fetched with the token, each to open large.
  if (m.images && m.images.length) {
    body.append(imageRow(m.images.map((im) => `/api/bus/image?${new URLSearchParams({ id: im.id })}`)));
  }
  if (less) body.append(less);
  // A bubble under its sender's name: the name and time are outside the
  // bubble, the text in it.
  const col = el("div", "bcol");
  col.append(meta, body);
  row.append(col);
  return row;
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
  // The card's pill says which handoff and to whom, or how it ended: the
  // line is what it is about (a declined one says so, the pill being small).
  const first = firstSentence(lines[0] || "");
  const h = m.handoff;
  if (m.kind === "done" && h && h.status === "declined") return `declined: ${first}`;
  return first;
}

// firstSentence is the start of a line with its markdown taken off, cut at
// the end of its first sentence, or at 120 characters.
function firstSentence(line) {
  const plain = line.replace(/^[#>\-*\s]+/, "").replace(/[*_`]/g, "")
    .replace(/https?:\/\/\S+?\/pull\/(\d+)\S*/g, "PR #$1") // a link is its name
    .replace(/https?:\/\/([^\s/,;)]+)\S*/g, "$1").trim();
  const end = plain.search(/[.!?。！？](\s|$)/);
  const one = end > 0 ? plain.slice(0, end + 1) : plain;
  return one.length > 120 ? one.slice(0, 119) + "…" : one;
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
      // Under its bubble where there is one (the phone's chat), else in it.
      (row.querySelector(".bcol") || row.querySelector(".bbody")).append(line);
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
