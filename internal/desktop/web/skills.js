// Skills in the window (package skill): a page of what agents saved for
// you to approve and of the skills approved, the project's skills as chips
// on the new chat page, and "Save as skill" in an agent's menu.
"use strict";

const sk = { open: false, data: null, editing: new Set(), timer: null };

// The text "Save as skill" queues to the agent.
const SAVE_AS_SKILL = "Distill what worked in this conversation into a reusable skill with skill_save: name, when to use it, the steps and commands that worked, what to avoid. Scope project unless it is general.";

async function loadSkills(project) {
  return api("GET", "/api/skills", project ? { project } : {});
}

// The sidebar button shows a dot while something waits.
async function drawSkillsDot() {
  try {
    const d = await loadSkills();
    const n = d.pending.length;
    $("#skills").classList.toggle("dot", n > 0);
    $("#skills").title = n ? `${n} skill${n > 1 ? "s" : ""} waiting for you` : "skills";
    if (sk.open) {
      sk.data = d;
      drawSkills();
    }
  } catch (_) {}
}
setInterval(drawSkillsDot, 15000);
drawSkillsDot();

function openSkills() {
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
  closeBoard();
  closeChanges();
  closeMenu();
  closePopup();
  sk.open = true;
  $("#empty").style.display = "none";
  $("#skillview").hidden = false;
  inMain(true);
  drawQueue();
  drawSidebar();
  drawSkillsDot();
  drawSkills();
}

function closeSkills() {
  if (!sk.open) return;
  sk.open = false;
  $("#skillview").hidden = true;
  $("#empty").style.display = "";
}

$("#skills").onclick = () => (sk.open ? closeSkills() || drawTitle() : openSkills());


function drawSkills() {
  const root = $("#skillview");
  const d = sk.data;
  const page = el("div", "skpage");
  page.append(el("h1", "", "技能"),
    el("p", "muted", "agent 存下来的做事方法。批准的技能会复制给 Claude、Codex 和 Pi，各自在它的 / 菜单里找到。"));
  if (!d) {
    page.append(el("p", "muted", "加载中…"));
    root.replaceChildren(page);
    return;
  }
  page.append(el("h2", "", `在等你${d.pending.length ? " · " + d.pending.length : ""}`));
  if (!d.pending.length) page.append(el("p", "muted", "没有在等你的"));
  for (const p of d.pending) page.append(pendingCard(p));
  page.append(el("h2", "", "已批准"));
  if (!d.approved.length) page.append(el("p", "muted", "还没有：agent 用 skill_save 存一个，或者在它的菜单里选“存为技能”。"));
  const groups = new Map();
  for (const s of d.approved) {
    const g = s.scope === "user" ? "你的，所有项目通用" : s.project_name || s.project;
    if (!groups.has(g)) groups.set(g, []);
    groups.get(g).push(s);
  }
  for (const [g, list] of groups) {
    page.append(el("h3", "", g));
    for (const s of list) page.append(approvedRow(s));
  }
  const keep = root.scrollTop;
  root.replaceChildren(page);
  root.scrollTop = keep;
}

function pendingCard(p) {
  const card = el("div", "skcard");
  const head = el("div", "skhead");
  head.append(el("b", "", "/" + p.name),
    el("span", "pill", p.scope === "user" ? "所有项目" : p.project_name || "项目"),
    el("span", "muted skby", `${p.by || "某个 agent"} · ${ago(p.at)}`));
  card.append(head, el("p", "skdesc", p.description));
  const actions = el("div", "skacts");
  if (sk.editing.has(p.name)) {
    const ta = el("textarea", "skedit");
    ta.value = p.text;
    ta.spellcheck = false;
    ta.rows = Math.min(24, p.text.split("\n").length + 2);
    card.append(ta);
    actions.append(button("批准", "primary", () => approveSkill(p, ta.value)),
      button("取消", "", () => { sk.editing.delete(p.name); drawSkills(); }));
  } else {
    card.append(md(p.body));
    actions.append(button("批准", "primary", () => approveSkill(p)),
      button("编辑", "", () => { sk.editing.add(p.name); drawSkills(); }),
      button("丢弃", "danger", () => discardSkill(p)));
  }
  card.append(actions);
  return card;
}

function approvedRow(s) {
  const d = el("details", "skrow");
  const sum = el("summary");
  sum.append(el("b", "", "/" + s.name), el("span", "muted skd", s.description));
  d.append(sum, md(s.body));
  return d;
}

async function approveSkill(p, body) {
  try {
    await api("POST", "/api/skills/approve", {}, body === undefined ? { name: p.name } : { name: p.name, body });
    sk.editing.delete(p.name);
    toast(`已批准 /${p.name}`);
    skillCache.clear();
    drawSkillsDot();
  } catch (e) {
    toast(e.message, "err");
  }
}

async function discardSkill(p) {
  const v = await dialog({
    title: `丢弃 /${p.name}？`, body: el("p", "muted dtext", "它会被丢掉；agent 可以再存一次。"),
    actions: [{ label: "取消", value: null }, { label: "丢弃", value: "d", cls: "danger" }],
  });
  if (v !== "d") return;
  try {
    await api("POST", "/api/skills/discard", {}, { name: p.name });
    drawSkillsDot();
  } catch (e) {
    toast(e.message, "err");
  }
}

// ---- the new chat page: the project's skills as chips ----

const skillCache = new Map(); // project → { at, list }

function drawNewSkills() {
  let box = $("#newskills");
  if (!box) {
    box = el("div", "skchips");
    box.id = "newskills";
    $("#new").append(box);
  }
  const path = nc.project;
  if (!path) {
    box.replaceChildren();
    return;
  }
  const c = skillCache.get(path);
  if (c && Date.now() - c.at < 30000) {
    paintChips(box, c.list);
    return;
  }
  if (box.dataset.project !== path) box.replaceChildren();
  box.dataset.project = path;
  loadSkills(path).then((d) => {
    skillCache.set(path, { at: Date.now(), list: d.approved });
    if (nc.open && nc.project === path) paintChips($("#newskills"), d.approved);
  }).catch(() => {});
}

function paintChips(box, list) {
  box.dataset.project = nc.project;
  box.replaceChildren(...list.map((s) => {
    const b = el("button", "chip skchip", "/" + s.name);
    b.type = "button";
    b.title = s.description;
    b.onmousedown = (e) => {
      e.preventDefault();
      const t = $("#qinput");
      t.value = `/${s.name} ` + t.value;
      fitInput();
      t.focus();
    };
    return b;
  }));
}

// ---- an agent's menu: Save as skill ----

async function saveAsSkill(a) {
  try {
    if (windowed(a)) await sendTo(a.id, SAVE_AS_SKILL, []);
    else {
      const r = await authFetch(`/api/queue?${new URLSearchParams({ id: a.id })}`, { method: "POST", body: SAVE_AS_SKILL });
      if (!r.ok) throw new Error((await r.text()).trim() || r.statusText);
    }
    toast(`已请 ${label(a)} 存一个技能：到“技能”里批准它`);
  } catch (e) {
    toast(e.message, "err");
  }
}
