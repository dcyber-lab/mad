// The ⌘K palette (v5): one field over the window that finds an agent, a group or a command and goes there.
// ↑ ↓ pick, ⏎ goes, Esc closes. On a phone the sidebar's own search stays what search is.
"use strict";

const pal = { open: false, sel: 0, items: [] };

function paletteCommands() {
  return [
    { label: T("新对话", "New chat"), key: "⌘N", run: () => openNew() },
    { label: T("跳到下一个等你的", "Jump to next waiting"), key: "⌥N", run: () => nextItem() },
    { label: T("今日", "Today"), run: () => goHome() },
    { label: T("看板", "Board"), key: "⌥B", run: () => openBoard() },
    { label: T("用量", "Usage"), run: () => openUsage() },
    { label: T("技能", "Skills"), run: () => openSkills() },
    { label: T("新群聊", "New group"), run: () => createChannel() },
    { label: T("新小组…", "New team…"), run: () => newTeam() },
    { label: T("添加项目…", "Add project…"), run: () => addProject() },
  ];
}

// paletteFind is what q finds: agents and groups whose names (or project, kind, what they do) hold it,
// then the commands whose names do.
function paletteFind(q) {
  const s = q.trim().toLowerCase();
  const has = (...xs) => !s || xs.some((x) => String(x || "").toLowerCase().includes(s));
  const out = [];
  const rank = (a) => ({ waiting: 0, running: 1, finished: 2 }[shownStatus(a)] ?? 3);
  const list = [...agents.values()].filter((a) => has(label(a), a.title, a.kind, a.tool, a.target, a.project && a.project.name)).sort((x, y) => rank(x) - rank(y));
  for (const a of list.slice(0, s ? 8 : 5)) out.push({ sect: T("agent", "Agents"), agent: a, run: () => show(a.id) });
  for (const c of (deck.bus || []).filter((c) => (!c.dm || c.yours) && has(c.id, c.title, c.topic, c.team && c.team.name)).slice(0, s ? 5 : 3)) {
    out.push({ sect: T("群聊", "Groups"), channel: c, run: () => openChannel(c.id) });
  }
  for (const c of paletteCommands().filter((c) => has(c.label))) out.push({ sect: T("命令", "Commands"), ...c });
  return out;
}

function openPalette() {
  if (phone.matches) {
    document.body.classList.add("searching");
    $("#filter").focus();
    return;
  }
  closePopup();
  pal.open = true;
  pal.sel = 0;
  let bg = $("#palette");
  if (!bg) {
    bg = el("div", "");
    bg.id = "palette";
    bg.innerHTML = '<div class="pal" role="dialog" aria-modal="true"><input type="text" autocomplete="off" spellcheck="false"><div class="prows"></div></div>';
    document.body.append(bg);
    bg.addEventListener("pointerdown", (e) => { if (e.target === bg) closePalette(); });
    const input = bg.querySelector("input");
    input.addEventListener("input", () => { pal.sel = 0; drawPalette(); });
    input.addEventListener("keydown", (e) => {
      if (e.isComposing) return;
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); closePalette(); }
      else if (e.key === "ArrowDown") { e.preventDefault(); pal.sel = Math.min(pal.items.length - 1, pal.sel + 1); markPalette(); }
      else if (e.key === "ArrowUp") { e.preventDefault(); pal.sel = Math.max(0, pal.sel - 1); markPalette(); }
      else if (e.key === "Enter") { e.preventDefault(); runPalette(pal.sel); }
    });
  }
  const input = bg.querySelector("input");
  bg.querySelector(".pal").setAttribute("aria-label", T("搜索或跳转", "Search or jump"));
  input.placeholder = T("搜索 agent、群聊、命令…", "Search agents, groups, commands…");
  input.value = "";
  bg.hidden = false;
  drawPalette();
  input.focus();
}

function closePalette() {
  pal.open = false;
  const bg = $("#palette");
  if (bg) bg.hidden = true;
}

function runPalette(i) {
  const it = pal.items[i];
  if (!it) return;
  closePalette();
  it.run();
}

function drawPalette() {
  const bg = $("#palette");
  if (!bg) return;
  pal.items = paletteFind(bg.querySelector("input").value);
  const box = bg.querySelector(".prows");
  charPut(box, () => {
    const nodes = [];
    let sect = "";
    pal.items.forEach((it, i) => {
      if (it.sect !== sect) { sect = it.sect; nodes.push(el("div", "psect", sect)); }
      const r = el("button", "prow");
      r.type = "button";
      r.dataset.i = i;
      const txt = el("span", "ptxt");
      if (it.agent) {
        r.append(avatar(it.agent, true));
        txt.append(el("span", "pnm", label(it.agent)), el("span", "psub", `${it.agent.project.name} · ${it.agent.kind}`));
      } else if (it.channel) {
        r.append(groupChar(chanAgents(it.channel), true));
        txt.append(el("span", "pnm", it.channel.team ? it.channel.team.name : it.channel.id), el("span", "psub", chanSub(it.channel)));
      } else {
        txt.append(el("span", "pnm", it.label));
      }
      r.append(txt);
      if (it.key) r.append(el("kbd", "", it.key));
      r.onclick = () => runPalette(i);
      r.onpointermove = () => { if (pal.sel !== i) { pal.sel = i; markPalette(); } };
      nodes.push(r);
    });
    if (!pal.items.length) nodes.push(el("div", "pnone", T("没有找到", "Nothing found")));
    return nodes;
  });
  markPalette();
}

function markPalette() {
  const bg = $("#palette");
  if (!bg) return;
  for (const r of bg.querySelectorAll(".prow")) r.classList.toggle("on", Number(r.dataset.i) === pal.sel);
  bg.querySelector(".prow.on")?.scrollIntoView({ block: "nearest" });
}
