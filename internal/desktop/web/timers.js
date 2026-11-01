// What is scheduled (bus.Timer, deck.timers): the agents' check-ins to
// themselves, which you can cancel, and your routines, which you can add,
// pause, fire now and delete. It is the "Scheduled" strip of the Board page
// and the "+ Routine" dialog, which a channel's menu opens too.
"use strict";

const weekdayNames = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];
const weekdayZh = { mon: "周一", tue: "周二", wed: "周三", thu: "周四", fri: "周五", sat: "周六", sun: "周日" };

// everyText words a schedule as the server writes it ("weekdays 09:00",
// "mon,wed 09:30", "every 30m 08:00-18:00") in Chinese.
function everyText(s) {
  const m = /^every (\d+)([mh])(.*)$/.exec(s || "");
  if (m) return `每 ${m[1]} ${m[2] === "h" ? "小时" : "分钟"}${m[3]}`;
  const [word, ...rest] = (s || "").split(" ");
  const days = { daily: "每天", weekdays: "工作日", weekends: "周末" }[word] || word.split(",").map((d) => weekdayZh[d] || d).join("、");
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
  h.append(el("h2", "", "定时"), el("span", "muted", timers.length ? ` ${timers.length}` : ""));
  h.append(button("+ 例行任务", "", () => routineDialog()));
  box.append(h);
  const checkins = timers.filter((t) => t.kind === "once" && t.held !== "expired");
  const routines = timers.filter((t) => t.kind === "routine");
  if (!checkins.length && !routines.length) {
    box.append(el("p", "muted bnone", "没有定时的事。例行任务按节奏发消息；agent 会预约一次回头看看。"));
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
    who.append(el("span", "", a ? label(a) : "agent"), el("span", "muted", t.held ? t.why : `${ageText((new Date(t.next_at) - Date.now()) * 1e6)}后`));
  } else {
    who.append(el("b", "", everyText(t.every)), el("span", "muted", t.channel.startsWith("dm.") ? "私信" : "#" + t.channel));
  }
  row.append(who);
  const what = t.kind === "once" ? t.note : t.action === "report" ? "发进度报告" : t.text;
  row.append(el("span", "twhat", what));
  const meta = el("span", "tmeta muted");
  if (t.kind === "routine") {
    const bits = [t.paused ? "已暂停" : `下次 ${whenText(t.next_at)}`];
    if (t.missed) bits.push(`错过 ${t.missed} 次`);
    if (t.why) bits.push(t.why);
    meta.textContent = bits.join(" · ");
  }
  row.append(meta);
  const more = el("button", "icon-btn");
  more.type = "button";
  more.title = t.kind === "once" ? "取消它" : "暂停、立即触发、删除";
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
    items.push({ label: "取消这次预约", desc: "不会为它叫醒 agent", icon: "trash", danger: true, onclick: call({ op: "rm", id: t.id }) });
  } else {
    items.push({ label: t.paused ? "继续" : "暂停", desc: t.paused ? "从下一次开始" : "继续之前它什么都不发", icon: t.paused ? "play" : "pause", onclick: call({ op: t.paused ? "resume" : "pause", id: t.id }) });
    items.push({ label: "立即触发", desc: "现在发一次", icon: "play", onclick: call({ op: "fire", id: t.id }, "已触发") });
    items.push("-");
    items.push({ label: "删除", icon: "trash", danger: true, onclick: call({ op: "rm", id: t.id }) });
  }
  if (t.channel) items.push({ label: t.channel.startsWith("dm.") ? "打开私信" : `打开 #${t.channel}`, icon: "chat", onclick: () => openChannel(t.channel) });
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
    toast("先建一个群：例行任务要发在群里", "err");
    return;
  }
  const f = { mode: "at", days: [] };
  const body = el("div", "rform");
  const seg = el("div", "seg");
  const atBtn = el("button", "on", "定时"), everyBtn = el("button", "", "每隔…");
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
  presets.append(chip("每天", [0, 1, 2, 3, 4, 5, 6]), chip("工作日", [0, 1, 2, 3, 4]), chip("周末", [5, 6]));
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
  atBox.append(presets, dayRow, el("label", "rline", "时间 "), f.time);

  const everyBox = el("div", "rrow");
  everyBox.hidden = true;
  f.n = el("input", "say");
  f.n.type = "number";
  f.n.min = "1";
  f.n.value = "30";
  f.unit = el("select", "say");
  f.unit.append(new Option("分钟", "m"), new Option("小时", "h"));
  f.window = el("input");
  f.window.type = "checkbox";
  f.from = el("input", "say");
  f.from.type = "time";
  f.from.value = "08:00";
  f.to = el("input", "say");
  f.to.type = "time";
  f.to.value = "18:00";
  const win = el("label", "rline");
  win.append(f.window, " 只在 ");
  everyBox.append(el("label", "rline", "每隔 "), f.n, f.unit, el("div", "rline"), win, f.from, " 到 ", f.to, " 之间");

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
  reportRow.append(doReport, " 发进度报告，不发消息");
  const text = el("textarea", "say wide rtext");
  text.rows = 3;
  text.placeholder = "要发的话，例如 @all 昨天做了什么？";
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

  body.append(seg, atBox, everyBox, el("label", "rline", "发在"), where, text, sug, reportRow,
    el("p", "dtext muted", "例行任务算你的指令：你不在也会发。最短每 15 分钟一次。"));
  const v = await dialog({
    title: "新的例行任务", body,
    actions: [{ label: "取消", value: null }, { label: "添加", cls: "primary", value: "ok" }],
    ready: () => text.focus(),
  });
  if (v !== "ok") return;
  try {
    await api("POST", "/api/timer", {}, {
      op: "add", every: scheduleOf(f), channel: where.value, text: text.value.trim(), action: doReport.checked ? "report" : "post",
    });
    toast("已添加例行任务");
    await refresh();
  } catch (e) {
    toast(e.message, "err");
  }
}
