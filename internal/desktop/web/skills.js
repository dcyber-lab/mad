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
  page.append(el("h1", "", "Skills"),
    el("p", "muted", "Ways of working your agents saved. Approved skills are copied to Claude, Codex and Pi, so each finds them in its / menu."));
  if (!d) {
    page.append(el("p", "muted", "Loading…"));
    root.replaceChildren(page);
    return;
  }
  page.append(el("h2", "", `Waiting for you${d.pending.length ? " · " + d.pending.length : ""}`));
  if (!d.pending.length) page.append(el("p", "muted", "Nothing waits for you."));
  for (const p of d.pending) page.append(pendingCard(p));
  page.append(el("h2", "", "Approved"));
  if (!d.approved.length) page.append(el("p", "muted", "None yet: an agent saves one with skill_save, or from its menu: Save as skill."));
  const groups = new Map();
  for (const s of d.approved) {
    const g = s.scope === "user" ? "Yours, for every project" : s.project_name || s.project;
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
    el("span", "pill", p.scope === "user" ? "all projects" : p.project_name || "project"),
    el("span", "muted skby", `${p.by || "an agent"} · ${ago(p.at)}`));
  card.append(head, el("p", "skdesc", p.description));
  const actions = el("div", "skacts");
  if (sk.editing.has(p.name)) {
    const ta = el("textarea", "skedit");
    ta.value = p.text;
    ta.spellcheck = false;
    ta.rows = Math.min(24, p.text.split("\n").length + 2);
    card.append(ta);
    actions.append(button("Approve", "primary", () => approveSkill(p, ta.value)),
      button("Cancel", "", () => { sk.editing.delete(p.name); drawSkills(); }));
  } else {
    card.append(md(p.body));
    actions.append(button("Approve", "primary", () => approveSkill(p)),
      button("Edit", "", () => { sk.editing.add(p.name); drawSkills(); }),
      button("Discard", "danger", () => discardSkill(p)));
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
    toast(`Approved /${p.name}`);
    skillCache.clear();
    drawSkillsDot();
  } catch (e) {
    toast(e.message, "err");
  }
}

async function discardSkill(p) {
  const v = await dialog({
    title: `Discard /${p.name}?`, body: el("p", "muted dtext", "It is dropped; the agent can save it again."),
    actions: [{ label: "Cancel", value: null }, { label: "Discard", value: "d", cls: "danger" }],
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
      const r = await fetch(`/api/queue?${new URLSearchParams({ token, id: a.id })}`, { method: "POST", body: SAVE_AS_SKILL });
      if (!r.ok) throw new Error((await r.text()).trim() || r.statusText);
    }
    toast(`Asked ${label(a)} to save a skill: you approve it under Skills`);
  } catch (e) {
    toast(e.message, "err");
  }
}
