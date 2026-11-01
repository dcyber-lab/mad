// The usage page: for each kind, the 5 hour and weekly windows of the
// snapshot's quota, each as a big percent, a bar, what is left and when it
// resets; and mad's own work today against its budget, which the person
// raises here (the day's budget is what the agents wait on when it is spent).
"use strict";

// sig: what the page showed last, so it is drawn again only when that changed.
const us = { open: false, sig: "" };

function openUsage() {
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
  closeSkills();
  closeBoard();
  closeChanges();
  closeMenu();
  closePopup();
  $("#empty").style.display = "none";
  us.open = true;
  $("#usageview").hidden = false;
  inMain(true);
  us.sig = "";
  drawUsage();
  drawSidebar();
  drawQueue();
}

// closeUsage is called by closeBoard: every place that leaves a page for
// another already closes the board.
function closeUsage() {
  if (!us.open) return;
  us.open = false;
  $("#usageview").hidden = true;
  $("#usageview").replaceChildren();
  $("#empty").style.display = "";
}

// inWords says how long until t: "2h 10m", "3d 4h", "under a minute".
function inWords(ms) {
  const m = Math.floor(ms / 60000);
  if (m < 1) return "under a minute";
  const d = Math.floor(m / 1440), h = Math.floor((m % 1440) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m % 60}m`;
  return `${m}m`;
}

function resetWords(at) {
  const t = new Date(at);
  const ms = t - Date.now();
  if (!(ms > 0)) return "resets now";
  const clock = t.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
  const day = t.toDateString() === new Date().toDateString() ? "" : `${t.toLocaleDateString([], { weekday: "short" })} `;
  return `resets in ${inWords(ms)}, at ${day}${clock}`;
}

// money is dollars as the page says them.
function money2(v) {
  return `$${v < 100 ? (+v).toFixed(2) : Math.round(v)}`;
}

// spendCard is mad's own work today: its cost against the budget, its turns against the turn
// budget, a bar (amber from 80%, red when spent), what the budget makes the agents do, and the
// two things the person does about it: add $10, or lift it for the day (as the inbox's Budget
// item offers).
function spendCard(s) {
  if (!s || !s.day) return null;
  const part = s.part || 0;
  const tone = s.closed ? " over" : part >= 0.8 ? " hot" : "";
  const card = el("section", `uscard usmad${s.closed ? " closed" : ""}`);
  card.append(el("div", "uskind", "mad 自己的工作"));
  card.append(el("div", `uspct usmoney${tone}`, s.budget_usd ? `${money2(s.usd)} / ${money2(s.budget_usd)}` : money2(s.usd)));
  if (s.budget_usd || s.budget_turns) {
    const bar = el("div", "usbar");
    const fill = el("i", s.closed ? "over" : part >= 0.8 ? "hot" : "");
    fill.style.width = `${Math.min(100, Math.round(part * 100))}%`;
    bar.append(fill);
    card.append(bar);
  }
  card.append(el("div", "usleft", `${s.turns} / ${s.budget_turns || "∞"} 轮`));
  if (s.closed) card.append(el("div", "usnote over", "已用完，agent 在等"));
  else if (s.open_today) card.append(el("div", "usnote", "今天不限"));
  const acts = el("div", "usacts");
  const act = (text, cls, body) => {
    const b = button(text, cls);
    b.onclick = async () => {
      b.disabled = true;
      try {
        const r = await api("POST", "/api/budget", {}, body);
        if (r) deck.spend = r;
        us.sig = "";
        drawUsage();
        drawSpendDot();
        drawUsageLine();
        refresh();
      } catch (e) {
        toast(e.message, "err");
        b.disabled = false;
      }
    };
    return b;
  };
  acts.append(act("加 $10", "primary", { op: "raise", usd: 10 }));
  if (!s.open_today) acts.append(act("今天不限", "", { op: "open" }));
  card.append(acts);
  return card;
}

function drawUsage() {
  if (!us.open) return;
  // by the minute, for the resets in words
  const sig = JSON.stringify([deck.quota, deck.spend, Math.floor(Date.now() / 60000)]);
  if (sig === us.sig) return;
  us.sig = sig;
  const root = $("#usageview");
  root.replaceChildren();
  const page = el("div", "uspage");
  const mad = spendCard(deck.spend);
  // when the budget is spent it is what to look at: first
  if (mad && deck.spend.closed) page.append(mad);
  let n = 0;
  for (const [kind, w] of Object.entries(deck.quota || {})) {
    for (const [key, name] of [["five_hour", "5 hours"], ["seven_day", "This week"]]) {
      const win = w[key];
      if (!win || (!win.used && !win.reset_at)) continue;
      n++;
      const used = Math.max(0, Math.round(win.used || 0));
      const card = el("section", "uscard");
      card.append(el("div", "uskind", `${kind[0].toUpperCase()}${kind.slice(1)} · ${name}`));
      card.append(el("div", `uspct${used >= 100 ? " over" : used >= 80 ? " hot" : ""}`, `${used}%`));
      const bar = el("div", "usbar");
      const fill = el("i", used >= 100 ? "over" : used >= 80 ? "hot" : "");
      fill.style.width = `${Math.min(100, used)}%`;
      bar.append(fill);
      card.append(bar, el("div", "usleft", `${Math.max(0, 100 - used)}% left`));
      if (win.reset_at) card.append(el("div", "usreset", resetWords(win.reset_at)));
      page.append(card);
    }
  }
  if (mad && !deck.spend.closed) page.append(mad);
  if (!n && !mad) page.append(el("p", "muted", "No usage figures yet."));
  root.append(page);
}
