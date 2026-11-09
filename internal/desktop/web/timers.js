// What is scheduled (bus.Timer, deck.timers): the agents' check-ins to
// themselves, which you can cancel, and your routines, which you can add,
// pause, fire now and delete. It is the "Scheduled" strip of the Board page
// and the "+ Routine" dialog, which a channel's menu opens too.
"use strict";

const weekdayNames = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];
const weekdayZh = { mon: T("周一", "Mon"), tue: T("周二", "Tue"), wed: T("周三", "Wed"), thu: T("周四", "Thu"), fri: T("周五", "Fri"), sat: T("周六", "Sat"), sun: T("周日", "Sun") };

// everyText words a schedule as the server writes it ("weekdays 09:00",
// "mon,wed 09:30", "every 30m 08:00-18:00") in Chinese.
function everyText(s) {
  const m = /^every (\d+)([mh])(.*)$/.exec(s || "");
  if (m) return T(`每 ${m[1]} ${m[2] === "h" ? T("小时", "h") : T("分钟", "min")}${m[3]}`, `every ${m[1]} ${m[2] === "h" ? "h" : "min"}${m[3]}`);
  const [word, ...rest] = (s || "").split(" ");
  const days = { daily: T("每天", "Daily"), weekdays: T("工作日", "Weekdays"), weekends: T("周末", "Weekends") }[word] || word.split(",").map((d) => weekdayZh[d] || d).join("、");
  return [days, ...rest].join(" ");
}

// whenText writes a time as 15:04 today, Mon 15:04 within a week, or the date.
function whenText(iso) {
  const d = new Date(iso);
  if (!iso || isNaN(d) || d.getFullYear() < 2000) return "";
  const hm = d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
  const days = (d - new Date()) / 864e5;
  if (d.toDateString() === new Date().toDateString()) return hm;
  if (days > 0 && days < 6) return `${d.toLocaleDateString("zh-CN", { weekday: "short" })} ${hm}`;
  return `${d.toLocaleDateString("zh-CN", { month: "short", day: "numeric" })} ${hm}`;
}

// timerStrip draws the Scheduled strip: check-ins, then routines.
function timerStrip() {
  const timers = deck.timers || [];
  const box = el("section", "bsched");
  const h = el("div", "bschedhead");
  h.append(el("h2", "", T("定时", "Scheduled")), el("span", "muted", timers.length ? ` ${timers.length}` : ""));
  h.append(button(T("+ 例行任务", "+ Routine"), "", () => routineDialog()));
  box.append(h);
  const checkins = timers.filter((t) => t.kind === "once" && t.held !== "expired");
  const routines = timers.filter((t) => t.kind === "routine");
  if (!checkins.length && !routines.length) {
    box.append(el("p", "muted bnone", T("没有定时的事。例行任务按节奏发消息；agent 会预约一次回头看看。", "Nothing scheduled. A routine posts on a schedule; an agent books a check-in.")));
  }
  for (const t of checkins) box.append(timerRow(t));
  for (const t of routines) box.append(timerRow(t));
  return box;
}

function timerRow(t) {
  const row = el("div", `trow k-${t.kind}${t.paused ? " off" : ""}`);
  const a = agents.get(t.by);
  const who = el("span", "twho");
  if (t.kind === "once") {
    if (a) who.append(avatar(a, true));
    who.append(el("span", "", a ? label(a) : "agent"), el("span", "muted", t.held ? t.why : T(`${ageText((new Date(t.next_at) - Date.now()) * 1e6)}后`, `in ${ageText((new Date(t.next_at) - Date.now()) * 1e6)}`)));
  } else {
    who.append(el("b", "", everyText(t.every)), el("span", "muted", t.channel.startsWith("dm.") ? T("私信", "DM") : "#" + t.channel));
  }
  row.append(who);
  const what = t.kind === "once" ? t.note : t.action === "report" ? T("发进度报告", "Post a progress report") : t.text;
  row.append(el("span", "twhat", what));
  const meta = el("span", "tmeta muted");
  if (t.kind === "routine") {
    const bits = [t.paused ? T("已暂停", "Paused") : T(`下次 ${whenText(t.next_at)}`, `next ${whenText(t.next_at)}`)];
    if (t.missed) bits.push(T(`错过 ${t.missed} 次`, `missed ${t.missed} times`));
    if (t.why) bits.push(t.why);
    meta.textContent = bits.join(" · ");
  }
  row.append(meta);
  const more = el("button", "icon-btn");
  more.type = "button";
  more.title = t.kind === "once" ? T("取消它", "Cancel it") : T("暂停、立即触发、删除", "Pause, run now, remove");
  more.append(icon("more"));
  more.onclick = (e) => {
    e.stopPropagation();
    timerMenu(t, more);
  };
  row.append(more);
  row.onclick = () => timerMenu(t, more);
  return row;
}

function timerMenu(t, anchor) {
  const call = (body, done) => async () => {
    try {
      await api("POST", "/api/timer", {}, body);
      if (done) toast(done);
      await refresh();
    } catch (e) {
      toast(e.message, "err");
    }
  };
  const items = [];
  if (t.kind === "once") {
    items.push({ label: T("取消这次预约", "Cancel this check-in"), desc: T("不会为它叫醒 agent", "No agent is woken for it"), icon: "trash", danger: true, onclick: call({ op: "rm", id: t.id }) });
  } else {
    items.push({ label: t.paused ? T("继续", "Resume") : T("暂停", "Pause"), desc: t.paused ? T("从下一次开始", "From the next time on") : T("继续之前它什么都不发", "It posts nothing until resumed"), icon: t.paused ? "play" : "pause", onclick: call({ op: t.paused ? "resume" : "pause", id: t.id }) });
    items.push({ label: T("立即触发", "Run now"), desc: T("现在发一次", "Post once now"), icon: "play", onclick: call({ op: "fire", id: t.id }, T("已触发", "Ran")) });
    items.push("-");
    items.push({ label: T("删除", "Remove"), icon: "trash", danger: true, onclick: call({ op: "rm", id: t.id }) });
  }
  if (t.channel) items.push({ label: t.channel.startsWith("dm.") ? T("打开私信", "Open DM") : T(`打开 #${t.channel}`, `Open #${t.channel}`), icon: "chat", onclick: () => openChannel(t.channel) });
  popup(anchor, items, { right: true });
}

// scheduleOf reads the schedule the dialog's controls say, as bus.ParseSchedule
// does: "weekdays 09:00", "mon,wed 09:30", "every 30m 08:00-18:00".
function scheduleOf(f) {
  if (f.mode === "every") {
    const n = Math.max(1, parseInt(f.n.value, 10) || 0);
    let s = `every ${n}${f.unit.value}`;
    if (f.window.checked && f.from.value && f.to.value) s += ` ${f.from.value}-${f.to.value}`;
    return s;
  }
  const on = weekdayNames.filter((_, i) => f.days[i].classList.contains("on"));
  if (!on.length) return "";
  const set = on.join(",");
  const word = on.length === 7 ? "daily" : set === "mon,tue,wed,thu,fri" ? "weekdays" : set === "sat,sun" ? "weekends" : set;
  return `${word} ${f.time.value || "09:00"}`;
}

// routineDialog asks for a routine: when, where and what, with agents to
// @mention offered as you type; channel is the one to start with.
async function routineDialog(channel) {
  const chans = (deck.bus || []).filter((c) => !c.dm);
  if (!chans.length) {
    toast(T("先建一个群：例行任务要发在群里", "Make a group first: a routine posts in a group"), "err");
    return;
  }
  const f = { mode: "at", days: [] };
  const body = el("div", "rform");
  const seg = el("div", "seg");
  const atBtn = el("button", "on", T("定时", "Scheduled")), everyBtn = el("button", "", T("每隔…", "Every…"));
  atBtn.type = everyBtn.type = "button";
  seg.append(atBtn, everyBtn);

  const atBox = el("div", "rrow");
  const presets = el("div", "rchips");
  const chip = (text, days) => {
    const b = el("button", "rchip", text);
    b.type = "button";
    b.onclick = () => f.days.forEach((d, i) => d.classList.toggle("on", days.includes(i)));
    return b;
  };
  presets.append(chip(T("每天", "Daily"), [0, 1, 2, 3, 4, 5, 6]), chip(T("工作日", "Weekdays"), [0, 1, 2, 3, 4]), chip(T("周末", "Weekends"), [5, 6]));
  const dayRow = el("div", "rchips");
  weekdayNames.forEach((n, i) => {
    const b = el("button", `rchip day${i < 5 ? " on" : ""}`, weekdayZh[n]);
    b.type = "button";
    b.onclick = () => b.classList.toggle("on");
    f.days.push(b);
    dayRow.append(b);
  });
  f.time = el("input", "say");
  f.time.type = "time";
  f.time.value = "09:00";
  atBox.append(presets, dayRow, el("label", "rline", T("时间 ", "At ")), f.time);

  const everyBox = el("div", "rrow");
  everyBox.hidden = true;
  f.n = el("input", "say");
  f.n.type = "number";
  f.n.min = "1";
  f.n.value = "30";
  f.unit = el("select", "say");
  f.unit.append(new Option(T("分钟", "min"), "m"), new Option(T("小时", "h"), "h"));
  f.window = el("input");
  f.window.type = "checkbox";
  f.from = el("input", "say");
  f.from.type = "time";
  f.from.value = "08:00";
  f.to = el("input", "say");
  f.to.type = "time";
  f.to.value = "18:00";
  const win = el("label", "rline");
  win.append(f.window, T(" 只在 ", " only between "));
  everyBox.append(el("label", "rline", T("每隔 ", "Every ")), f.n, f.unit, el("div", "rline"), win, f.from, T(" 到 ", " and "), f.to, T(" 之间", ""));

  const setMode = (m) => {
    f.mode = m;
    atBtn.classList.toggle("on", m === "at");
    everyBtn.classList.toggle("on", m === "every");
    atBox.hidden = m !== "at";
    everyBox.hidden = m !== "every";
  };
  atBtn.onclick = () => setMode("at");
  everyBtn.onclick = () => setMode("every");

  const where = el("select", "say wide");
  for (const c of chans) where.append(new Option("#" + c.id, c.id));
  if (channel) where.value = channel;

  const doReport = el("input");
  doReport.type = "checkbox";
  const reportRow = el("label", "rline");
  reportRow.append(doReport, T(" 发进度报告，不发消息", " post a progress report, not a message"));
  const text = el("textarea", "say wide rtext");
  text.rows = 3;
  text.placeholder = T("要发的话，例如 @all 昨天做了什么？", "What to post, such as @all what got done yesterday?");
  const sug = el("div", "suggest rsug");
  const complete = () => {
    const before = text.value.slice(0, text.selectionStart);
    const m = /(^|[\s(])@([\w-]*)$/.exec(before);
    sug.replaceChildren();
    if (!m) return;
    const q = m[2].toLowerCase();
    const names = [...agents.values()].filter((a) => a.chat && (!a.role || a.team)).map((a) => label(a)).concat(["all"])
      .filter((n) => n.toLowerCase().startsWith(q)).slice(0, 6);
    for (const n of names) {
      const r = el("button", "srow", "");
      r.type = "button";
      r.append(el("span", "t", "@" + n));
      r.onmousedown = (e) => e.preventDefault();
      r.onclick = () => {
        const at = text.selectionStart - m[2].length;
        text.value = text.value.slice(0, at) + n + " " + text.value.slice(text.selectionStart);
        text.selectionStart = text.selectionEnd = at + n.length + 1;
        sug.replaceChildren();
        text.focus();
      };
      sug.append(r);
    }
  };
  text.oninput = complete;
  text.onclick = complete;
  doReport.onchange = () => { text.hidden = doReport.checked; sug.hidden = doReport.checked; };

  body.append(seg, atBox, everyBox, el("label", "rline", T("发在", "Post in")), where, text, sug, reportRow,
    el("p", "dtext muted", T("例行任务算你的指令：你不在也会发。最短每 15 分钟一次。", "A routine counts as yours: it posts when you are away too. Every 15 minutes at most.")));
  const v = await dialog({
    title: T("新的例行任务", "New routine"), body,
    actions: [{ label: T("取消", "Cancel"), value: null }, { label: T("添加", "Add"), cls: "primary", value: "ok" }],
    ready: () => text.focus(),
  });
  if (v !== "ok") return;
  try {
    await api("POST", "/api/timer", {}, {
      op: "add", every: scheduleOf(f), channel: where.value, text: text.value.trim(), action: doReport.checked ? "report" : "post",
    });
    toast(T("已添加例行任务", "Routine added"));
    await refresh();
  } catch (e) {
    toast(e.message, "err");
  }
}
