// Teams in the window (package team): a template's roles started
// together in a channel with a lead, the team's roles in the channel's
// header with their live status, and putting a team to sleep or ending
// it.
"use strict";

// teamLine is the roles of team t in a channel's header, each with its
// agent's live status.
function teamLine(t) {
  const line = el("div", "cteam");
  line.append(el("span", "muted", `team ${t.template} ·`));
  for (const r of t.roles) {
    const a = r.agent && agents.get(r.agent);
    const chip = el("span", `trole${a ? " s-" + shownStatus(a) : " s-stopped"}`);
    if (a) {
      const v = avatar(a, true);
      v.onclick = () => show(a.id);
      v.title = `${label(a)}: ${look(shownStatus(a)).word.toLowerCase()}`;
      chip.append(v);
    }
    chip.append(el("span", "", r.lead ? `${r.role} (lead)` : r.role));
    line.append(chip);
  }
  return line;
}

// teamItems are what a team channel's menu adds to the channel's own.
function teamItems(c) {
  const t = c.team;
  return [
    "-",
    {
      label: "Sleep team", desc: "every agent of it stops; a message wakes one", icon: "sleep",
      onclick: async () => {
        try {
          const r = await api("POST", "/api/team", {}, { op: "sleep", team: t.name });
          toast(`${r.slept} asleep`);
          refresh();
        } catch (e) {
          toast(e.message, "err");
          refresh();
        }
      },
    },
    {
      label: "Remove team", desc: "removes its agents (their conversations stay on disk) and archives the channel", icon: "trash", danger: true,
      onclick: async () => {
        const v = await dialog({
          title: `Remove team ${t.name}?`,
          body: el("p", "dtext", `Its ${t.roles.length} agents are stopped and removed from mad, and #${c.id} is archived.`),
          actions: [{ label: "Cancel", value: null }, { label: "Remove", cls: "danger", value: "ok" }],
        });
        if (v !== "ok") return;
        try {
          await api("POST", "/api/team", {}, { op: "rm", team: t.name });
          if (cv.open === c.id) closeChannel();
          await refresh();
        } catch (e) {
          toast(e.message, "err");
          refresh();
        }
      },
    },
  ];
}

// select is a <select> over options [value, text], value chosen.
function select(options, value, onchange) {
  const s = el("select", "say tsel");
  for (const [v, text] of options) {
    const o = el("option", "", text);
    o.value = v;
    s.append(o);
  }
  s.value = value;
  if (onchange) s.onchange = () => onchange(s.value);
  return s;
}

// agentParts splits "claude:opus@high" into its agent and effort.
function agentParts(s) {
  const [agent, effort] = (s || "").split("@");
  return [agent, effort || ""];
}

// newTeam asks for a team to start: the template with its roles' agents
// and efforts as the run form offers them, the project, name, goal,
// permission and, if wanted, one worktree for all.
async function newTeam(project) {
  if (!deck.projects.length) {
    toast("Add a project first", "err");
    return;
  }
  let info;
  try {
    info = await api("GET", "/api/team/templates", { project: project || deck.projects[0].path });
  } catch (e) {
    toast(e.message, "err");
    return;
  }
  if (info.error) toast(info.error, "err");
  const choice = (agent) => info.choices.find((c) => c.agent === agent);
  const body = el("div", "tform");
  const proj = select(deck.projects.map((p) => [p.path, p.name]), project || nc.project || deck.projects[0].path);
  const tpl = select(info.templates.map((t) => [t.name, t.description ? `${t.name}: ${t.description}` : t.name]), info.templates[0].name, () => drawRoles());
  const roles = el("div", "troles");
  const picked = {}; // role → { agent, effort }
  const drawRoles = () => {
    const t = info.templates.find((x) => x.name === tpl.value);
    const rows = [];
    for (const k of Object.keys(picked)) delete picked[k];
    for (const r of t.roles) {
      const [agent0, effort0] = agentParts(r.agent);
      picked[r.name] = { agent: agent0, effort: effort0 };
      const opts = info.choices.map((c) => [c.agent, c.agent === "codex" ? "codex (its own model)" : c.agent]);
      if (!choice(agent0)) opts.unshift([agent0, agent0]);
      const eff = el("span", "");
      const drawEffort = () => {
        const levels = (choice(picked[r.name].agent) || { efforts: [] }).efforts;
        const e = select([["", "default effort"], ...levels.map((l) => [l, l])], levels.includes(picked[r.name].effort) ? picked[r.name].effort : "", (v) => (picked[r.name].effort = v));
        picked[r.name].effort = e.value;
        eff.replaceChildren(e);
      };
      const ag = select(opts, agent0, (v) => {
        picked[r.name].agent = v;
        drawEffort();
      });
      drawEffort();
      const name = el("div", "tname");
      name.append(el("b", "", r.name), r.lead ? el("span", "pill working", "lead") : "", r.read_only ? el("span", "pill quiet", "read only") : "");
      const row = el("div", "trole-row");
      row.title = r.brief || "";
      row.append(name, ag, eff);
      rows.push(row);
    }
    roles.replaceChildren(...rows);
  };
  drawRoles();
  const name = el("input", "say wide");
  name.placeholder = "name (from the goal when empty)";
  name.autocapitalize = "none";
  name.autocomplete = "off";
  name.spellcheck = false;
  const goal = el("textarea", "say wide tgoal");
  goal.placeholder = "the goal: what the team is to get done";
  goal.rows = 3;
  const perm = select(info.permissions.map((p) => [p, p === "auto" ? "auto: claude judges each action (recommended)" : p === "allowlist" ? "allowlist: edits, tests, builds, commits" : "bypass: asks nothing (risky)"]), info.permissions[0]);
  const shared = el("input");
  shared.type = "checkbox";
  const branch = el("input", "say wide");
  branch.placeholder = "branch, e.g. team/api";
  branch.hidden = true;
  shared.onchange = () => {
    branch.hidden = !shared.checked;
    if (shared.checked && !branch.value) branch.focus();
  };
  const wt = el("label", "tcheck");
  wt.append(shared, el("span", "", "One shared worktree for all (otherwise they work in the project's directory)"));
  const field = (text, node) => {
    const f = el("label", "tfield");
    f.append(el("span", "muted", text), node);
    return f;
  };
  body.append(field("Project", proj), field("Template", tpl), roles, field("Name", name), field("Goal", goal), field("Permission", perm), wt, branch,
    el("p", "dtext muted", `Lead and teammates start together in a new channel; the lead gets the goal. At most ${info.max_teams} teams run at once.`));
  const v = await dialog({
    title: "A new team", body,
    actions: [{ label: "Cancel", value: null }, { label: "Start", cls: "primary", value: "ok" }],
    ready: () => goal.focus(),
  });
  if (v !== "ok") return;
  if (!goal.value.trim()) {
    toast("The team needs a goal", "err");
    return;
  }
  const agentsChosen = {};
  for (const [r, p] of Object.entries(picked)) agentsChosen[r] = p.agent + (p.effort ? "@" + p.effort : "");
  toast("Starting the team: its agents come up one by one");
  try {
    const r = await api("POST", "/api/team", {}, {
      op: "start", template: tpl.value, project: proj.value, name: name.value.trim(), goal: goal.value.trim(),
      permission: perm.value, worktree: shared.checked ? branch.value.trim() : "", agents: agentsChosen,
    });
    for (const n of r.notes || []) toast(n, "err");
    await refresh();
    openChannel(r.channel);
  } catch (e) {
    toast(e.message, "err");
    refresh();
  }
}
