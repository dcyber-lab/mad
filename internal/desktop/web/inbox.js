// The inbox as the page draws it: deck.inbox (package attention). Every
// item comes with its words, its acts and where it opens, so nothing here
// knows a kind: the page draws title, context and text, and an act posts
// {key, verb, option, text} to /api/inbox. A kind added in Go needs no
// change here. Drawn as a row (the sidebar, the phone's home) and as a
// card (home, the board's first column).
"use strict";

const ib = {
  reply: null,       // {key, verb, label, where}: the item whose reply box is open
  drafts: new Map(), // key → text typed, not sent
  nextKey: null,     // where Next last went
  more: {},          // folded groups, unfolded: name → true
  // keys acted on → {verb, label, at, done}: the item stays where it is,
  // its buttons disabled, until a snapshot no longer has it. Nothing slides
  // under the finger that tapped it.
  pending: new Map(),
  expanded: new Set(), // keys of rows opened in place as cards (Review)
  down: null,        // the act button the pointer last went down on: {id, ok}
  layoutAt: 0,       // when the inbox's layout last changed (ms)
  sig: "",           // what that layout was
  allChans: false,
};

// A tap that follows a layout change by less than this is not an act: the
// row under the finger may be another one than the person aimed at.
const settleMs = 500;
// An act the snapshot still shows this long after it was sent is not
// settling: the item is live again.
const pendingMs = 12000;

// ---- what the snapshot holds ----

function needsYou() {
  const n = deck.inbox?.needs || 0;
  return (deck.inbox?.items || []).slice(0, n);
}
function finishedItems() {
  const n = deck.inbox?.needs || 0;
  return (deck.inbox?.items || []).slice(n);
}
function inboxItem(key) {
  return (deck.inbox?.items || []).find((it) => it.key === key);
}
// pruneGone forgets the acted-on keys the snapshot no longer has (or
// still has long after: that act did not take).
function pruneGone() {
  for (const [k, p] of ib.pending) {
    if (!inboxItem(k) || (p.done && Date.now() - p.done > pendingMs)) ib.pending.delete(k);
  }
  if (ib.reply && !inboxItem(ib.reply.key)) ib.reply = null;
  for (const k of ib.expanded) if (!inboxItem(k)) ib.expanded.delete(k);
}

// settleLayout notes when the inbox's rows changed, so that a tap aimed at
// the old layout is not taken for one at the new.
function settleLayout() {
  const sig = JSON.stringify([...needsYou(), ...finishedItems()].map((it) => it.key)) + JSON.stringify(ib.reply) + [...ib.expanded];
  if (sig !== ib.sig) {
    ib.sig = sig;
    ib.layoutAt = Date.now();
  }
}

// ---- time ----

function short(at) {
  const m = Math.floor((Date.now() - new Date(at)) / 60000);
  if (!at || String(at).startsWith("0001") || isNaN(m)) return ""; // zero: unknown
  if (m < 1) return "now";
  if (m < 60) return `${m}m`;
  if (m < 1440) return `${Math.floor(m / 60)}h`;
  return `${Math.floor(m / 1440)}d`;
}
// tick is an age the page keeps current; suffix goes after it (" ago").
function tick(at, suffix = "") {
  const s = el("span", "t");
  s.dataset.at = at;
  s.dataset.suf = suffix;
  tickText(s);
  return s;
}
function tickText(s) {
  const a = short(s.dataset.at);
  s.textContent = a === "now" ? a : a + s.dataset.suf;
}
setInterval(() => {
  for (const s of document.querySelectorAll(".t[data-at]")) tickText(s);
  fitRows(document.getElementById("projects")); // an age that grew may cut a command
}, 15000);

// ---- the parts ----

// glyph is a state's mark: the page's own CSS draws it by tone.
function glyph(tone) {
  return el("span", `gl ${tone}`);
}

// rowButton is a small labeled button that does not open its row.
function rowButton(text, cls, fn, title, tapId) {
  const b = button(text, `sm${cls ? " " + cls : ""}`, (e) => {
    e.stopPropagation();
    if (tapId && !tapped(e, tapId)) return;
    fn(e);
  });
  if (title) b.title = title;
  if (tapId) {
    b.dataset.tap = tapId;
    b.addEventListener("pointerdown", () => { ib.down = { id: tapId, ok: Date.now() - ib.layoutAt > settleMs }; });
  }
  return b;
}

// tapped says whether click e on the button tapId is an act the person
// meant: the pointer went down on that very button (not on a row that slid
// under it), and not just after the layout changed. A key press (detail 0)
// has no pointer to mislead.
function tapped(e, tapId) {
  if (e.detail === 0) return true;
  const d = ib.down;
  ib.down = null;
  return !!d && d.id === tapId && d.ok;
}

function meter(part, hot) {
  const m = el("span", "meter");
  const i = el("i", hot ?? (part >= 1 ? "over" : part >= 0.8 ? "hot" : ""));
  i.style.width = `${Math.min(100, Math.round(part * 100))}%`;
  m.append(i);
  return m;
}

// itemLead leads an item: its icon, else its agent's avatar (its live dot
// is the agent's own entry).
function itemLead(it) {
  if (it.icon) {
    const v = el("span", `avatar sys${it.kind === "finished" ? "" : " s-waiting"}`);
    v.append(icon(it.icon), el("span", "dot"));
    return v;
  }
  const a = agents.get(it.agent);
  return avatar(a || { id: it.agent, kind: "?" });
}

// itemLine is what an item says under its name: how long it has waited,
// then its text (code when it is a command). The age is first: a narrow
// row cuts the end, and the end is what matters least.
function itemLine(it) {
  const out = [];
  if (it.part != null) out.push(meter(it.part), " ");
  const aged = short(it.since) !== "";
  if (aged) out.push(tick(it.since));
  if (it.text) out.push(aged ? " · " : "", it.mono ? el("span", "tgt", it.text) : it.text);
  return out;
}

// ---- a row: agents and inbox items alike ----

// row is lead | name, context / line | right, one height per kind of row.
function row(o) {
  const r = el("div", `row ${o.cls || ""}`);
  r.tabIndex = 0;
  if (o.key) r.dataset.key = o.key;
  if (o.title) r.title = o.title;
  const t1 = el("div", "t1");
  t1.append(el("span", "nm", o.name));
  if (o.ctx) t1.append(el("span", "ctx", o.ctx));
  if (o.after) t1.append(o.after);
  r.append(o.lead, t1);
  if (o.line) {
    const t2 = el("div", "t2");
    t2.append(...[].concat(o.line).flat().filter((x) => x != null && x !== ""));
    r.append(t2);
  }
  if (o.right && o.right.length) {
    const rt = el("div", "rt");
    rt.append(...o.right);
    r.append(rt);
  }
  r.onclick = o.onclick;
  r.onkeydown = (e) => {
    if (e.target === r && (e.key === "Enter" || e.key === " ")) {
      e.preventDefault();
      r.click();
    }
  };
  return r;
}

// An act that lets something through (allow, always, approve) is not
// offered in a row beside a line the row had to cut: there it is Review,
// which opens the item as a card with the whole of it. fitRows decides,
// once the row is laid out; until then (and wherever it cannot measure) the
// row says Review.
const letsThrough = new Set(["allow", "always", "approve"]);

function itemRow(it) {
  const acts = [];
  for (const a of it.acts.filter((x) => x.row)) {
    const b = actButton(it, a, "side");
    acts.push(b);
    if (it.text && letsThrough.has(a.verb) && !ib.pending.has(it.key)) {
      b.hidden = true;
      b.classList.add("guard");
      acts.push(rowButton("Review", "primary review", () => { ib.expanded.add(it.key); redraw(); }, "Read all of it before allowing", `${it.key}\nreview`));
    }
  }
  const r = row({
    key: it.key, cls: `${ib.nextKey === it.key ? "sel" : ""}${ib.pending.has(it.key) ? " pending" : ""}`, lead: itemLead(it), name: it.title, ctx: it.context,
    line: itemLine(it), right: acts, onclick: () => openItem(it),
  });
  const a = it.agent && agents.get(it.agent);
  r.title = [it.title, it.context, it.text].filter(Boolean).join(" · ") + (a ? `\n${a.id}` : "");
  return r;
}

// fitRows shows each guarded act whose row shows its whole line, and Review
// where the line is cut or cannot be measured (a hidden list is 0 wide).
function fitRows(root) {
  for (const b of root.querySelectorAll(".row .guard")) {
    const row = b.closest(".row");
    const rv = row.querySelector(".review");
    const t2 = row.querySelector(".t2");
    b.hidden = false;
    if (rv) rv.hidden = true;
    const fits = !!t2 && t2.clientWidth > 0 && t2.scrollWidth <= t2.clientWidth;
    b.hidden = !fits;
    if (rv) rv.hidden = fits;
  }
}
{
  const list = document.getElementById("projects");
  if (list) new ResizeObserver(() => fitRows(list)).observe(list);
  document.fonts?.ready.then(() => list && fitRows(list));
}

// ---- acts ----

// actButton is act a of item it as a button: an act that takes words
// opens the reply box, open goes to the item, the rest are sent.
function actButton(it, a, where) {
  const cls = a.primary && a.verb !== "open" ? "primary" : a.verb === "open" ? "ghost" : "";
  const b = rowButton(a.label, cls, () => {
    if (a.verb === "open") openItem(it);
    else if (a.input) openReply(it, a, where);
    else act(it, a);
  }, a.option && a.option !== a.label ? `${a.label}: ${a.option}` : "", `${it.key}\n${a.verb}\n${a.label}\n${where}`);
  const p = ib.pending.get(it.key);
  if (p && a.verb !== "open") {
    // sent: it stays here, saying so, until the snapshot settles it
    b.disabled = true;
    if (p.verb === a.verb && p.label === a.label && p.option === (a.option || "")) b.textContent = pendingWord(a);
  }
  return b;
}

// pendingWord is what the button of an act in flight says.
function pendingWord(a) {
  const w = { allow: "Allowing", always: "Allowing", deny: "Denying", answer: "Answering", reply: "Sending", approve: "Approving",
    discard: "Discarding", seen: "Marking", send: "Sending", drop: "Dropping", resume: "Resuming", raise: "Raising", lift: "Lifting" }[a.verb];
  return `${w || a.label.replace(/…$/, "")}…`;
}

function openReply(it, a, where) {
  ib.reply = { key: it.key, verb: a.verb, label: a.label, where };
  redraw();
  const ta = replyArea(it.key, where);
  if (ta) {
    ta.focus();
    ta.setSelectionRange(ta.value.length, ta.value.length);
  }
}

// replyArea is the textarea of the reply box for key drawn where: the one
// on show. A view that is hidden (home under the board) may still hold a
// box of its own from before.
function replyArea(key, where) {
  const all = document.querySelectorAll(`.replybox[data-key="${CSS.escape(key)}"][data-where="${where}"] textarea`);
  return [...all].find((t) => t.getClientRects().length) || null;
}

function closeReply() {
  if (!ib.reply) return;
  ib.reply = null;
  redraw();
}

// replyBox is what an input act opens in place: the item's one-tap
// options (its acts with an option and the same verb), a textarea, Send.
function replyBox(it, r) {
  const a = it.acts.find((x) => x.input && x.verb === r.verb && x.label === r.label);
  if (!a) return null;
  const box = el("div", "replybox");
  box.dataset.key = it.key;
  box.dataset.where = r.where;
  box.onclick = (e) => e.stopPropagation();
  const q = el("div", "rq");
  q.append(el("span", "who", `${it.title}${it.context ? " " + it.context : ""}: `), it.text || it.body || "");
  box.append(q);
  const opts = it.acts.filter((x) => x.option && !x.input && x.verb === a.verb);
  if (opts.length) box.append(el("div", "opts", ""));
  for (const o of opts) box.lastChild.append(rowButton(o.option, "opt", () => act(it, o), "Send this answer", `${it.key}\nopt\n${o.option}`));
  const ta = el("textarea");
  ta.placeholder = `${a.label.replace(/…$/, "")}…`;
  ta.rows = 2;
  ta.value = ib.drafts.get(it.key) || "";
  ta.oninput = () => ib.drafts.set(it.key, ta.value);
  ta.onkeydown = (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      send();
    }
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      closeReply();
    }
  };
  const send = () => {
    if (ta.value.trim()) act(it, a, ta.value.trim());
  };
  const ra = el("div", "ra");
  ra.append(el("span", "muted", "↵ send · Esc"), rowButton("Cancel", "ghost", closeReply), rowButton("Send", "primary", send, "", `${it.key}\nsend`));
  box.append(ta, ra);
  return box;
}

// act sends the person's choice: no hold, the agent waits on it. 409: it
// was handled already (elsewhere, or it stopped waiting); 400: its error.
async function act(it, a, text) {
  if (ib.pending.has(it.key)) return; // one act per item: it is already sent
  ib.pending.set(it.key, { verb: a.verb, label: a.label, option: a.option || "", at: Date.now(), done: 0 });
  if (ib.reply?.key === it.key) ib.reply = null;
  redraw();
  const what = text ? `${a.label}: “${text.length > 40 ? text.slice(0, 39) + "…" : text}”` : a.option && a.option !== a.label ? `${a.label}: ${a.option}` : a.label;
  try {
    const r = await fetch(`/api/inbox?${new URLSearchParams({ token })}`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ key: it.key, verb: a.verb, option: a.option || "", text: text || "" }),
    });
    if (r.status === 409) {
      toast("Already handled: it is not waiting on you any more");
    } else if (!r.ok) {
      ib.pending.delete(it.key);
      toast((await r.text()).trim() || r.statusText, "err");
    } else {
      ib.drafts.delete(it.key);
      toast(`${what} · ${it.title}`);
    }
  } catch (e) {
    ib.pending.delete(it.key);
    toast(e.message, "err");
  }
  const p = ib.pending.get(it.key);
  if (p) p.done = Date.now();
  redraw();
  refresh();
}

// markAllSeen marks every finished agent that offers it as looked at.
function seenActs() {
  return finishedItems().map((it) => [it, it.acts.find((a) => a.verb === "seen")]).filter(([, a]) => a);
}
function markAllSeen() {
  for (const [it, a] of seenActs()) act(it, a);
}

// ---- where an item is settled, and Next ----

function openItem(it) {
  const o = it.open || {};
  if (o.agent && agents.has(o.agent)) show(o.agent);
  else if (o.channel) openChannel(o.channel);
  else if (o.page === "skills") openSkills();
  else if (o.page === "board") openBoard();
}

// nextItem is Alt-N and the phone's Next: the item after the one Next
// went to last, wrapping, in the snapshot's order (what needs you, then
// what finished): the deck's d walks the same list.
function nextItem() {
  const list = [...needsYou(), ...finishedItems()];
  if (!list.length) return toast("Nothing needs you");
  const i = list.findIndex((x) => x.key === ib.nextKey);
  const it = list[(i + 1) % list.length];
  ib.nextKey = it.key;
  ib.more[needsYou().includes(it) ? "needs" : "finished"] = true; // the row it goes to shows
  openItem(it);
  redraw();
  setTimeout(() => {
    for (const n of document.querySelectorAll(`[data-key="${CSS.escape(it.key)}"]`)) {
      n.classList.remove("flash");
      void n.offsetWidth;
      n.classList.add("flash");
    }
    document.querySelector(`#projects [data-key="${CSS.escape(it.key)}"]`)?.scrollIntoView({ block: "nearest" });
  }, 0);
}

// ---- the same item, wider: home and the board ----

// itemCard is an item as a card: its words in full, every act. compact
// (the board's column) keeps the text to two lines.
function itemCard(it, compact, where = "main") {
  const done = it.kind === "finished";
  const c = el("div", `icard ${done ? "k-finished" : "k-needs"}${compact ? " compact" : ""}${ib.pending.has(it.key) ? " pending" : ""}${ib.nextKey === it.key ? " sel" : ""}`);
  c.dataset.key = it.key;
  const a = it.agent && agents.get(it.agent);
  const ch = el("div", "ch");
  const who = el("div", "who");
  who.append(el("span", "nm", it.title), el("span", "sub", [a && a.project.name, !done && it.context].filter(Boolean).join(" · ")));
  ch.append(itemLead(it), who, el("span", "age", ""));
  ch.lastChild.append(tick(it.since, done ? " ago" : ""));
  c.append(ch);
  if (it.text) {
    if (it.mono) {
      const cmd = el("div", "icmd");
      cmd.append(el("span", "pr", "$ "), it.text);
      if (it.where) cmd.append(el("span", "pr", `   in ${it.where}`));
      c.append(cmd);
    } else {
      // what an allow sits beside is shown whole
      c.append(el("div", it.acts.some((a) => letsThrough.has(a.verb)) ? "body full" : "body", it.text));
    }
  }
  if (it.part != null) {
    const t = el("div", "todo");
    t.append(meter(it.part));
    c.append(t);
  }
  if (it.body && !compact) c.append(el("div", "body muted", it.body));
  if (it.tasks?.length && !compact) c.append(el("div", "tasks muted", it.tasks.map((t) => t.title || t.id).join(" · ")));
  if (!done) {
    // an answer's options are one tap each; "Reply" and the like follow
    const acts = el("div", "acts");
    for (const x of it.acts) acts.append(actButton(it, x, where));
    if (where === "side") acts.append(rowButton("Less", "ghost", () => { ib.expanded.delete(it.key); redraw(); }, "Back to the row"));
    c.append(acts);
    if (ib.reply?.key === it.key && ib.reply.where === where) c.append(replyBox(it, ib.reply) || "");
  }
  c.onclick = () => openItem(it);
  return c;
}

// ---- an agent's activity line ----

// activityLine is agent a's line from its entry: every part that is set,
// in order. The snapshot leaves out the parts that do not apply in its
// state, so there is no rule here about which shows when.
//   working   Bash · go test ./... · 6m · 2/5
//   finished  Finished 3m ago · Tests pass
//   quiet     Idle 12m
function activityLine(a) {
  const todo = a.todo ? `${a.todo.done}/${a.todo.total}` : "";
  const aged = short(a.since) !== "";
  if (a.tool) {
    // age and todo first: a narrow row cuts the end, which says least
    const out = [];
    const put = (x) => { if (x) out.push(out.length ? " · " : "", x); };
    put(aged && tick(a.since));
    put(todo);
    put(el("span", "tool", a.tool));
    put(a.target && el("span", "tgt", a.target));
    return out;
  }
  const st = shownStatus(a);
  const word = look(st).word;
  const out = [el("span", st === "finished" ? "fin" : "word", `${word}${aged ? " " : ""}`)];
  if (aged) out[0].append(tick(a.since, st === "finished" ? " ago" : ""));
  if (todo) out.push(" · ", todo);
  if (a.reply) out.push(" · ", a.reply);
  return out;
}

// rollupNode is a group's roll-up: a mark and a count per state that
// needs you, works or finished, then how many are quiet. The tone comes
// from the state table.
function rollupNode(rollup) {
  const r = el("span", "rollup");
  let quiet = 0;
  for (const c of rollup || []) {
    const tone = look(c.state).tone;
    if (tone === "quiet") quiet += c.n;
    else {
      const n = el("span", `r-${tone}`);
      n.append(glyph(tone), String(c.n));
      r.append(n);
    }
  }
  if (quiet) r.append(el("span", "r-quiet", `${quiet} quiet`));
  return r;
}

// keepFocus draws, and gives back the focus and cursor of a reply box the
// draw replaced: a poll comes while one types.
function keepFocus(draw) {
  const ae = document.activeElement;
  const ta = ae && ae.matches && ae.matches(".replybox textarea") ? ae : null;
  const box = ta && ta.closest(".replybox");
  const at = ta && [box.dataset.key, box.dataset.where, ta.selectionStart, ta.selectionEnd];
  draw();
  if (!at) return;
  const n = replyArea(at[0], at[1]);
  if (n) {
    n.focus({ preventScroll: true });
    n.setSelectionRange(at[2], at[3]);
  }
}

// redraw draws what shows the inbox again, after the page changed it.
function redraw() {
  drawSidebar();
  bd.key = "";
  drawBoard();
}
