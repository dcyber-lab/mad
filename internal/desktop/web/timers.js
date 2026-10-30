// What is scheduled (bus.Timer, deck.timers): the agents' check-ins to
// themselves, which you can cancel, and your routines, which you can add,
// pause, fire now and delete. It is the "Scheduled" strip of the Board page
// and the "+ Routine" dialog, which a channel's menu opens too.
"use strict";

const weekdayNames = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];

// whenText writes a time as 15:04 today, Mon 15:04 within a week, or the date.
function whenText(iso) {
  const d = new Date(iso);
  if (!iso || isNaN(d) || d.getFullYear() < 2000) return "";
  const hm = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
  const days = (d - new Date()) / 864e5;
  if (d.toDateString() === new Date().toDateString()) return hm;
  if (days > 0 && days < 6) return `${d.toLocaleDateString([], { weekday: "short" })} ${hm}`;
  return `${d.toLocaleDateString([], { month: "short", day: "numeric" })} ${hm}`;
}

// timerStrip draws the Scheduled strip: check-ins, then routines.
function timerStrip() {
  const timers = deck.timers || [];
  const box = el("section", "bsched");
  const h = el("div", "bschedhead");
  h.append(el("h2", "", "Scheduled"), el("span", "muted", timers.length ? ` ${timers.length}` : ""));
  h.append(button("+ Routine", "", () => routineDialog()));
  box.append(h);
  const checkins = timers.filter((t) => t.kind === "once" && t.held !== "expired");
  const routines = timers.filter((t) => t.kind === "routine");
  if (!checkins.length && !routines.length) {
    box.append(el("p", "muted bnone", "Nothing scheduled. A routine posts on a rhythm; an agent books a check-in to look at something later."));
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
    who.append(el("span", "", a ? label(a) : "agent"), el("span", "muted", t.held ? t.why : `in ${ageText((new Date(t.next_at) - Date.now()) * 1e6)}`));
  } else {
    who.append(el("b", "", t.every), el("span", "muted", t.channel.startsWith("dm.") ? "DM" : "#" + t.channel));
  }
  row.append(who);
  const what = t.kind === "once" ? t.note : t.action === "report" ? "post the progress report" : t.text;
  row.append(el("span", "twhat", what));
  const meta = el("span", "tmeta muted");
  if (t.kind === "routine") {
    const bits = [t.paused ? "paused" : `next ${whenText(t.next_at)}`];
    if (t.missed) bits.push(`${t.missed} missed`);
    if (t.why) bits.push(t.why);
    meta.textContent = bits.join(" · ");
  }
  row.append(meta);
  const more = el("button", "icon-btn");
  more.type = "button";
  more.title = t.kind === "once" ? "cancel it" : "pause, fire now, delete";
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
    items.push({ label: "Cancel the check-in", desc: "the agent is not woken for it", icon: "trash", danger: true, onclick: call({ op: "rm", id: t.id }) });
  } else {
    items.push({ label: t.paused ? "Resume" : "Pause", desc: t.paused ? "from its next time" : "it posts nothing until resumed", icon: t.paused ? "play" : "pause", onclick: call({ op: t.paused ? "resume" : "pause", id: t.id }) });
    items.push({ label: "Fire now", desc: "posts it once, now", icon: "play", onclick: call({ op: "fire", id: t.id }, "Fired") });
    items.push("-");
    items.push({ label: "Delete", icon: "trash", danger: true, onclick: call({ op: "rm", id: t.id }) });
  }
  if (t.channel) items.push({ label: t.channel.startsWith("dm.") ? "Open the DM" : `Open #${t.channel}`, icon: "chat", onclick: () => openChannel(t.channel) });
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
    toast("Make a channel first: a routine posts in one", "err");
    return;
  }
  const f = { mode: "at", days: [] };
  const body = el("div", "rform");
  const seg = el("div", "seg");
  const atBtn = el("button", "on", "At a time"), everyBtn = el("button", "", "Every…");
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
  presets.append(chip("Daily", [0, 1, 2, 3, 4, 5, 6]), chip("Weekdays", [0, 1, 2, 3, 4]), chip("Weekends", [5, 6]));
  const dayRow = el("div", "rchips");
  weekdayNames.forEach((n, i) => {
    const b = el("button", `rchip day${i < 5 ? " on" : ""}`, n[0].toUpperCase() + n.slice(1, 3));
    b.type = "button";
    b.onclick = () => b.classList.toggle("on");
    f.days.push(b);
    dayRow.append(b);
  });
  f.time = el("input", "say");
  f.time.type = "time";
  f.time.value = "09:00";
  atBox.append(presets, dayRow, el("label", "rline", "at "), f.time);

  const everyBox = el("div", "rrow");
  everyBox.hidden = true;
  f.n = el("input", "say");
  f.n.type = "number";
  f.n.min = "1";
  f.n.value = "30";
  f.unit = el("select", "say");
  f.unit.append(new Option("minutes", "m"), new Option("hours", "h"));
  f.window = el("input");
  f.window.type = "checkbox";
  f.from = el("input", "say");
  f.from.type = "time";
  f.from.value = "08:00";
  f.to = el("input", "say");
  f.to.type = "time";
  f.to.value = "18:00";
  const win = el("label", "rline");
  win.append(f.window, " only between ");
  everyBox.append(el("label", "rline", "every "), f.n, f.unit, el("div", "rline"), win, f.from, " and ", f.to);

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
  reportRow.append(doReport, " post the progress report instead of a message");
  const text = el("textarea", "say wide rtext");
  text.rows = 3;
  text.placeholder = "what to post, e.g. @all what did you do yesterday?";
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

  body.append(seg, atBox, everyBox, el("label", "rline", "in"), where, text, sug, reportRow,
    el("p", "dtext muted", "A routine counts as your order: it posts even when you are away. At least every 15 minutes."));
  const v = await dialog({
    title: "A new routine", body,
    actions: [{ label: "Cancel", value: null }, { label: "Add", cls: "primary", value: "ok" }],
    ready: () => text.focus(),
  });
  if (v !== "ok") return;
  try {
    await api("POST", "/api/timer", {}, {
      op: "add", every: scheduleOf(f), channel: where.value, text: text.value.trim(), action: doReport.checked ? "report" : "post",
    });
    toast("Routine added");
    await refresh();
  } catch (e) {
    toast(e.message, "err");
  }
}
