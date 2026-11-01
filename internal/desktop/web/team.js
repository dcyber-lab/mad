// Teams in the window (package team): a template's roles started
// together in a channel with a lead, the team's roles in the channel's
// header with their live status, and putting a team to sleep or ending
// it.
"use strict";

// teamItems are what a team channel's menu adds to the channel's own.
function teamItems(c) {
  const t = c.team;
  return [
    "-",
    {
      label: "让小组休眠", desc: "它的所有 agent 都停下；来一条消息会叫醒其中一个", icon: "sleep",
      onclick: async () => {
        try {
          const r = await api("POST", "/api/team", {}, { op: "sleep", team: t.name });
          toast(`${r.slept} 个已休眠`);
          refresh();
        } catch (e) {
          toast(e.message, "err");
          refresh();
        }
      },
    },
    {
      label: "移除小组", desc: "移除它的 agent（对话仍留在磁盘上），并归档这个群", icon: "trash", danger: true,
      onclick: async () => {
        const v = await dialog({
          title: `移除小组 ${t.name}？`,
          body: el("p", "dtext", `它的 ${t.roles.length} 个 agent 会被停止并从 mad 移除，#${c.id} 会被归档。`),
          actions: [{ label: "取消", value: null }, { label: "移除", cls: "danger", value: "ok" }],
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
    toast("请先添加一个项目", "err");
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
      const opts = info.choices.map((c) => [c.agent, c.agent === "codex" ? "codex（用它自己的模型）" : c.agent]);
      if (!choice(agent0)) opts.unshift([agent0, agent0]);
      const eff = el("span", "");
      const drawEffort = () => {
        const levels = (choice(picked[r.name].agent) || { efforts: [] }).efforts;
        const e = select([["", "默认强度"], ...levels.map((l) => [l, l])], levels.includes(picked[r.name].effort) ? picked[r.name].effort : "", (v) => (picked[r.name].effort = v));
        picked[r.name].effort = e.value;
        eff.replaceChildren(e);
      };
      const ag = select(opts, agent0, (v) => {
        picked[r.name].agent = v;
        drawEffort();
      });
      drawEffort();
      const name = el("div", "tname");
      name.append(el("b", "", r.name), r.lead ? el("span", "pill working", "组长") : "", r.read_only ? el("span", "pill quiet", "只读") : "");
      const row = el("div", "trole-row");
      row.title = r.brief || "";
      row.append(name, ag, eff);
      rows.push(row);
    }
    roles.replaceChildren(...rows);
  };
  drawRoles();
  const name = el("input", "say wide");
  name.placeholder = "名字：字母、数字、短横线或任何语言的文字（留空则取自目标）";
  name.autocapitalize = "none";
  name.autocomplete = "off";
  name.spellcheck = false;
  const goal = el("textarea", "say wide tgoal");
  goal.placeholder = "目标：这个小组要完成什么";
  goal.rows = 3;
  const perm = select(info.permissions.map((p) => [p, p === "auto" ? "auto：由 claude 判断每个操作（推荐）" : p === "allowlist" ? "allowlist：改文件、跑测试、构建、提交" : "bypass：什么都不问（有风险）"]), info.permissions[0]);
  const shared = el("input");
  shared.type = "checkbox";
  const branch = el("input", "say wide");
  branch.placeholder = "分支，例如 team/api";
  branch.hidden = true;
  shared.onchange = () => {
    branch.hidden = !shared.checked;
    if (shared.checked && !branch.value) branch.focus();
  };
  const wt = el("label", "tcheck");
  wt.append(shared, el("span", "", "所有人共用一个 worktree（否则各自在项目目录里工作）"));
  const field = (text, node) => {
    const f = el("label", "tfield");
    f.append(el("span", "muted", text), node);
    return f;
  };
  body.append(field("项目", proj), field("模板", tpl), roles, field("名字", name), field("目标", goal), field("权限", perm), wt, branch,
    el("p", "dtext muted", `组长和组员会一起在一个新群里启动，目标交给组长。同时最多运行 ${info.max_teams} 个小组。`));
  // The start runs with the form open: a refusal is shown in it, and what
  // was typed stays to be fixed.
  let started = null;
  const start = async () => {
    if (!goal.value.trim()) {
      goal.focus();
      return "小组需要一个目标。";
    }
    const agentsChosen = {};
    for (const [r, p] of Object.entries(picked)) agentsChosen[r] = p.agent + (p.effort ? "@" + p.effort : "");
    started = await api("POST", "/api/team", {}, {
      op: "start", template: tpl.value, project: proj.value, name: name.value.trim(), goal: goal.value.trim(),
      permission: perm.value, worktree: shared.checked ? branch.value.trim() : "", agents: agentsChosen,
    });
    return null;
  };
  const v = await dialog({
    title: "新建小组", body,
    actions: [{ label: "取消", value: null }, { label: "启动", cls: "primary", value: "ok", submit: start, busy: "启动中：agent 一个接一个起来…" }],
    ready: () => goal.focus(),
  });
  if (v !== "ok" || !started) return;
  for (const n of started.notes || []) toast(n, "err");
  await refresh();
  openChannel(started.channel);
}
