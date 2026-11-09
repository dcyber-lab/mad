"use strict";

// A bot asks to start an agent or a team (inbox item kind "create"): its confirm card, what the card is
// replaced by (mad's own result line and grey lines), and how the lists show it. Everything is worded by
// create.type and the act's verb and kind, never by the server's English (context, text, labels).

const MODEL_KINDS = { claude: "Claude", codex: "Codex", pi: "Pi" };

// modelWords is a kind and model as the person reads them: "Codex", "Claude Sonnet" (an alias is capitalised),
// "Codex gpt-5.5" (a model id is shown as given).
function modelWords(kind, model) {
  const k = MODEL_KINDS[kind] || (kind ? kind[0].toUpperCase() + kind.slice(1) : "");
  if (!model) return k;
  return `${k} ${/^[a-z]+$/.test(model) ? model[0].toUpperCase() + model.slice(1) : model}`;
}

const isTeam = (c) => c && c.type === "team";

// createHead is what the card says it wants; with who, "architect 想创建一个团队".
function createHead(c, who) {
  if (isTeam(c)) return who ? T(`${who} 想创建一个团队`, `${who} wants to create a team`) : T("想创建一个团队", "Wants to create a team");
  return who ? T(`${who} 想创建一个 agent`, `${who} wants to create an agent`) : T("想创建一个 agent", "Wants to create an agent");
}

// createSummary is the one line of a list row: 发布检查 · 3 个角色 · mad.
function createSummary(c) {
  const out = [c.name];
  if (isTeam(c)) out.push(T(`${(c.roles || []).length} 个角色`, `${(c.roles || []).length} roles`));
  else out.push(modelWords(c.kind, c.model));
  if (c.project_name) out.push(c.project_name);
  return out.filter(Boolean).join(" · ");
}

// createView is item it as a list shows it (a row, a card on Today, the home's card): the asker, what it
// wants, a summary, and two acts: 查看, which opens it, and 拒绝. Allowing is for its card.
function createView(it) {
  if (it.kind !== "create" || !it.create) return it;
  const deny = it.acts.find((a) => a.verb === "deny" && !a.input);
  const acts = [{ verb: "view", label: "View", primary: true, row: true }];
  if (deny) acts.push({ ...deny, row: true });
  return { ...it, context: createHead(it.create), text: createSummary(it.create), mono: false, body: "", full: false, acts };
}

function specRow(left, right, tag) {
  const r = el("div", "crow");
  const l = el("span", "cl", left);
  if (tag) l.append(el("span", "ctag", tag));
  r.append(l, el("span", "cr", right));
  return r;
}

// createBody is what would be started: the name and project, its roles (a team) or its model and channel (an
// agent), and the first task or message.
function createBody(c) {
  const box = el("div", "cbody2");
  const name = el("div", "cname");
  name.append(el("b", "", c.name || ""));
  if (c.project_name) name.append(el("span", "cproj", T(`项目 ${c.project_name}`, `Project ${c.project_name}`)));
  const rows = el("div", "cspec");
  if (isTeam(c)) {
    for (const r of c.roles || []) {
      const lead = r.lead && !/^(负责人|lead)$/i.test(r.role) ? T("负责人", "lead") : "";
      rows.append(specRow(r.role, modelWords(r.kind, r.model), lead));
    }
  } else {
    rows.append(specRow(T("模型", "Model"), modelWords(c.kind, c.model)));
    if (c.channel) rows.append(specRow(T("加入群聊", "Joins"), c.channel));
  }
  box.append(name);
  if (rows.childNodes.length) box.append(rows);
  const first = isTeam(c) ? c.goal : c.message;
  if (first) {
    const f = el("div", "cfirst");
    f.append(el("span", "cfl", isTeam(c) ? T("第一件事", "First task") : T("第一句话", "First message")), el("div", "cft", first));
    box.append(f);
  }
  return box;
}

// createAsk is the confirm card of item it, asked by agent a: in the agent's conversation (where "wait") and
// in the channel it was answering in (where "chan", with the asker's name in its head). Its acts are chips;
// 改一下… opens a field under it, which sends the deny-with-words act.
function createAsk(it, a, where) {
  const c = it.create;
  const card = el("div", `ask ask-inbox ask-create${ib.pending.has(it.key) ? " sent" : ""}`);
  card.dataset.key = it.key;
  card.dataset.test = "create.card";
  const head = el("div", "ah");
  head.append(avatar(a || { id: it.agent, kind: "?" }, true), el("span", "t", createHead(c, where === "chan" ? it.title : "")));
  const age = el("span", "al");
  age.append(T("等了 ", "Waiting "), tick(it.since));
  head.append(age);
  const body = el("div", "abody");
  body.append(createBody(c));
  const acts = el("div", "acts chips");
  for (const x of it.acts) {
    if (x.verb === "open") continue;
    const b = actButton(it, x, where, true);
    b.dataset.test = x.verb === "allow" ? "create.allow" : x.input ? "create.change" : "create.deny";
    acts.append(b);
  }
  card.append(head, body, acts);
  if (ib.reply?.key === it.key && ib.reply.where === where) {
    acts.hidden = true;
    card.append(replyBox(it, ib.reply) || "");
  }
  return card;
}

// ---- the channel: the card floats at its foot ----

// drawChanAsk shows the cards of the creates that came from the open channel, over its foot.
function drawChanAsk() {
  const box = $("#chanask");
  if (!box) return;
  const items = cv.open ? needsYou().filter((it) => it.kind === "create" && it.create && it.from === cv.open) : [];
  const sig = JSON.stringify([LANG, items, items.map((it) => ib.pending.get(it.key)), ib.reply, cv.open]);
  if (box.dataset.sig === sig) return;
  box.dataset.sig = sig;
  box.hidden = !items.length;
  box.replaceChildren(...items.map((it) => createAsk(it, agents.get(it.agent), "chan")));
}

// ---- what replaces the card: the result line, and the grey lines ----

// createFor is the request key's entry in the snapshot's creates, the card's own record of how it ended.
function createFor(key) {
  return key && (deck.creates || []).find((x) => x.key === key);
}

const cStateWord = (a) => (a ? { running: T("在工作", "working"), waiting: T("在等你", "waiting for you"), stopped: T("已停止", "stopped"), exited: T("已停止", "stopped") }[a.status] || T("空闲", "idle") : "");

// resultLine is a created agent or team as mad's own line: its tile, what was made, how it stands, and 打开.
// c is the create spec (type, name, kind, model), out the outcome (team or agent, channel).
function resultLine(c, out) {
  const team = isTeam(c);
  if (out.status === "creating") {
    // allowed, and mad is starting it: the line in its pending form, a spinner for the tile and no 打开
    const b = el("div", "cres creating");
    b.dataset.test = "create.result";
    const n = c.name || "";
    const title = team ? T(`正在创建团队「${n}」…`, `Creating team “${n}”…`) : T(`正在创建「${n}」…`, `Creating “${n}”…`);
    const txt = el("div", "ctxt");
    txt.append(el("div", "ct", title));
    b.append(el("span", "spin"), txt);
    return b;
  }
  const id = team ? out.channel : out.agent;
  const here = team ? !!(id && busChannel(id)) : !!(id && agents.get(id));
  const b = el("button", `cres${here ? "" : " gone"}`);
  b.type = "button";
  b.dataset.test = "create.result";
  const title = team ? T(`团队「${c.name}」建好了`, `Team “${c.name}” is ready`) : T(`「${c.name}」建好了`, `“${c.name}” is ready`);
  b.setAttribute("aria-label", here ? T(`${title}，打开`, `${title}, open`) : title);
  const ch = team && here ? busChannel(id) : null;
  const ag = !team && here ? agents.get(id) : null;
  let sub = T("已经不在了", "No longer here");
  if (team && here) {
    const lead = ch.lead && agents.get(ch.lead);
    sub = T(`${(ch.members || []).length} 个成员`, `${(ch.members || []).length} members`) + " · " + (lead ? T(`负责人${cStateWord(lead)}`, `lead ${cStateWord(lead)}`) : c.project_name || "");
  } else if (ag) sub = `${modelWords(c.kind || ag.kind, c.model)} · ${cStateWord(ag)}`;
  const tile = here ? (team ? chanTile(id) : avatar(ag, true)) : avatar({ kind: "?", id: "" }, true);
  const txt = el("div", "ctxt");
  txt.append(el("div", "ct", title), el("div", "cs", sub));
  b.append(tile, txt);
  if (here) {
    const open = el("span", "copen");
    open.dataset.test = "create.open";
    open.append(T("打开", "Open"), icon("right"));
    b.append(open);
    b.onclick = () => (team ? openChannel(id) : show(id));
  } else b.disabled = true;
  return b;
}

// createNote is the grey line of a request that did not make anything.
function createNote(c, status, out) {
  const n = c.name || "";
  const team = isTeam(c);
  const text = {
    declined: team ? T(`你拒绝了，没有创建团队「${n}」`, `You declined, so team “${n}” wasn't created`) : T(`你拒绝了，没有创建「${n}」`, `You declined, so “${n}” wasn't created`),
    changes: T(`你要改一下：${out.reply || ""}`, `You asked for changes: ${out.reply || ""}`),
    expired: team ? T(`没等到你确认，没有创建团队「${n}」`, `No answer in time, so team “${n}” wasn't created`) : T(`没等到你确认，没有创建「${n}」`, `No answer in time, so “${n}” wasn't created`),
    failed: team ? T(`没建成团队「${n}」：${out.reason || ""}`, `Couldn't create team “${n}”: ${out.reason || ""}`) : T(`没建成「${n}」：${out.reason || ""}`, `Couldn't create “${n}”: ${out.reason || ""}`),
  }[status];
  if (!text) return null;
  const d = el("div", "cnote", text);
  d.dataset.test = "create.note";
  return d;
}

// outcomeLine draws status of create c: the result line or a grey one; null for one still going.
function outcomeLine(c, out) {
  const status = out.status;
  return status === "created" || status === "creating" ? resultLine(c, out) : createNote(c, status, out);
}

// createEventLine is mad's event line m of a channel (created, create_declined, ...) as the result line or a
// grey line, from the request the line is about (m.ref) in the snapshot's creates.
function createEventLine(m) {
  const d = m.data || {};
  const x = createFor(m.ref);
  const c = x?.create || { type: d.type, name: d.name };
  const status = { created: "created", create_declined: "declined", create_changes: "changes", create_expired: "expired", create_failed: "failed" }[m.event];
  return outcomeLine(c, { status, team: d.team, agent: d.agent, channel: d.channel, reason: d.reason, reply: d.words });
}

// ---- in an agent's conversation: in place of its agent_new / team_start step ----

const createTool = (tool) => /(^|__)(agent_new|team_start)$/.test(tool || "") ? (/team_start$/.test(tool) ? "team" : "agent") : "";

// A tool step's result is {status: "asked", request, item}: the outcome is on that item in the snapshot's
// creates, later; with create-without-asking on it is {status: "created", team | agent, channel} at once. Any
// other result (an error) leaves the step as it was.
function readResult(text) {
  let j = null;
  try { j = JSON.parse(text); } catch (_) { return null; }
  if (!j || typeof j !== "object") return null;
  if (j.status === "asked" && j.item) return { item: j.item };
  if (j.status === "created") return { out: { status: "created", team: j.team, agent: j.agent && typeof j.agent === "object" ? j.agent.id : j.agent, channel: j.channel } };
  return null;
}

// createSlot is the place of an agent_new / team_start step: nothing while the card is the place, then mad's
// line, from the item's outcome as the snapshot has it (drawCreateSlots keeps it current).
function createSlot(type, args) {
  const s = el("div", "cslot");
  s.hidden = true;
  s.createCall = { tool: type === "team" ? "mcp__mad__team_start" : "mcp__mad__agent_new", args, c: { type, name: args.name, kind: args.kind, model: args.model, project_name: args.project && String(args.project).split("/").filter(Boolean).pop() } };
  return s;
}

const createSlots = new Set();

// fillCreateSlot reads the result of the step in s: false when it is not one of mad's (the step is drawn as
// any other).
function fillCreateSlot(s, text) {
  const r = readResult(text);
  if (!r) return false;
  Object.assign(s.createCall, r);
  createSlots.add(s);
  drawCreateSlots();
  return true;
}

// drawCreateSlots draws each slot from its item's outcome; with none, nothing.
function drawCreateSlots() {
  for (const s of createSlots) {
    if (!s.isConnected) { createSlots.delete(s); continue; }
    const call = s.createCall;
    const x = call.item ? createFor(call.item) : null;
    const out = call.out || (x && x.outcome);
    const c = x?.create ? { ...call.c, ...x.create } : call.c;
    const id = out && (out.team ? out.channel : out.agent);
    const sig = JSON.stringify([LANG, out, c.name, !!(id && (agents.get(id) || busChannel(id))), out && agents.get(out.agent || "")?.status]);
    if (s.dataset.sig === sig) continue;
    s.dataset.sig = sig;
    const line = out && outcomeLine(c, out);
    s.hidden = !line;
    s.replaceChildren(...(line ? [line] : []));
  }
}

// What mad tells the asking agent when a request ended is its own plumbing: the line says it.
const createTold = (text) => /^\[mad\] your request c-[0-9a-f]{8} to create /.test(text || "");

// ---- the lists: what is new comes first ----

// freshCreates are the agents and channels a create made lately (30 minutes), newest first, from the snapshot.
function freshCreates() {
  const out = { agents: [], channels: [] };
  const now = Date.now();
  const made = (deck.creates || []).filter((x) => x.outcome && x.outcome.status === "created" && now - Date.parse(x.outcome.at) < 30 * 60e3);
  made.sort((a, b) => Date.parse(b.outcome.at) - Date.parse(a.outcome.at));
  for (const x of made) {
    if (x.create?.type === "team" && x.outcome.channel) out.channels.push(x.outcome.channel);
    else if (x.outcome.agent) out.agents.push(x.outcome.agent);
  }
  return out;
}

// What the person allowed on this device and has not yet seen arrive: its row gets one soft highlight.
const allowedHere = new Set();
const freshUntil = new Map();
function freshRow(id) {
  if ((freshUntil.get(id) || 0) > Date.now()) return true;
  for (const x of deck.creates || []) {
    if (!allowedHere.has(x.key) || !x.outcome || x.outcome.status !== "created") continue;
    if (x.outcome.agent === id || x.outcome.channel === id) {
      allowedHere.delete(x.key);
      freshUntil.set(id, Date.now() + 1500);
      return true;
    }
  }
  return false;
}

// byFresh puts the ids of fresh first, in its order, and keeps the rest as they were.
function byFresh(list, fresh, idOf) {
  if (!fresh.length) return list;
  const rank = (x) => { const i = fresh.indexOf(idOf(x)); return i < 0 ? fresh.length : i; };
  return list.map((x, i) => [x, i]).sort((p, q) => rank(p[0]) - rank(q[0]) || p[1] - q[1]).map(([x]) => x);
}
