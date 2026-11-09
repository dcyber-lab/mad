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

// inWords says how long until t: "2 小时 10 分", "3 天 4 小时", "不到 1 分钟".
function inWords(ms) {
  const m = Math.floor(ms / 60000);
  if (m < 1) return T("不到 1 分钟", "under a minute");
  const d = Math.floor(m / 1440), h = Math.floor((m % 1440) / 60);
  if (d) return T(`${d} 天 ${h} 小时`, `${d}d ${h}h`);
  if (h) return T(`${h} 小时 ${m % 60} 分`, `${h}h ${m % 60}m`);
  return T(`${m} 分钟`, `${m} min`);
}

// resetWords says when a window resets: within a day, how long until; later, the weekday and time.
function resetWords(at) {
  const t = new Date(at);
  const ms = t - Date.now();
  if (!(ms > 0)) return T("马上重置", "Resets now");
  const loc = LANG === "zh" ? "zh-CN" : "en-US";
  if (ms < 24 * 3600e3) return T(`${inWords(ms)}后重置`, `Resets in ${inWords(ms)}`);
  const when = `${t.toLocaleDateString(loc, { weekday: "short" })} ${t.toLocaleTimeString(loc, { hour: "2-digit", minute: "2-digit", hour12: false })}`;
  return T(`${when} 重置`, `Resets ${when}`);
}

// money is dollars as the page says them.
function money2(v) {
  return `$${v < 100 ? (+v).toFixed(2) : Math.round(v)}`;
}

// budgetAct is a button that changes mad's own budget (/api/budget) and draws what it became.
function budgetAct(text, cls, body) {
  const b = button(text, cls);
  b.onclick = async () => {
    b.disabled = true;
    try {
      const r = await api("POST", "/api/budget", {}, body);
      if (r) {
        deck.spend = r;
        deckGen++;
      }
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
}

// drawUsage is the page (v5): a header with No limit today; the subscription windows as cards (amber from
// 80%); what the sessions cost, by project (their totals so far: today's alone is known only over every
// session), and the agents that cost most; mad's own budget for today.
function drawUsage() {
  if (!us.open) return;
  // by the minute, for the resets in words
  const sig = JSON.stringify([deck.quota, deck.spend, deck.today, deckGen, Math.floor(Date.now() / 60000)]);
  if (sig === us.sig) return;
  us.sig = sig;
  const root = $("#usageview");
  const page = el("div", "uspage v5");
  const s = deck.spend;
  const head = el("div", "thead");
  const tt = el("div", "tt");
  tt.append(el("b", "", T("用量", "Usage")), el("span", "", T("订阅额度和今天的花费（按 API 价格估算）", "Subscription limits and today’s spend (at API prices)")));
  const right = el("div", "tright");
  if (s && s.day && !s.open_today) right.append(budgetAct(T("今天不限", "No limit today"), "", { op: "open" }));
  head.append(tt, right);
  page.append(head);
  const col = el("div", "tcol");
  const block = (title, k, body) => {
    const b = el("div", "tblock");
    const h = el("div", "tbh");
    h.append(title);
    if (k) h.append(el("span", "k", k));
    b.append(h, body);
    return b;
  };

  const quota = el("div", "quota");
  for (const [kind, w] of Object.entries(deck.quota || {})) {
    for (const [key, name] of [["five_hour", T("5 小时", "5-hour")], ["seven_day", T("每周", "Weekly")]]) {
      const win = w[key];
      if (!win || (!win.used && !win.reset_at)) continue;
      const used = Math.max(0, Math.round(win.used || 0));
      const q = el("section", "q");
      const top = el("div", "top2");
      top.append(el("span", "", `${kind[0].toUpperCase()}${kind.slice(1)} · ${name}`), el("span", "", win.reset_at ? resetWords(win.reset_at) : ""));
      const bar = el("div", "qbar");
      const fill = el("i", used >= 100 ? "over" : used >= 80 ? "hot" : "");
      fill.style.width = `${Math.min(100, used)}%`;
      bar.append(fill);
      q.append(top, el("div", "big", `${used}%`), bar);
      quota.append(q);
    }
  }
  col.append(block(T("订阅额度", "Subscription limits"), T("超过 80% 变琥珀色", "Amber above 80%"),
    quota.childNodes.length ? quota : el("div", "tquiet", T("还没有额度数据：claude 或 codex 回答过一次后才有", "No limits yet: they come once claude or codex has answered"))));

  const all = [...agents.values()];
  const lines = el("div", "tlines");
  for (const p of deck.projects) {
    const cost = p.agents.reduce((n, a) => n + (a.cost || 0), 0);
    if (!cost) continue;
    const l = el("div", "tline");
    const t2 = el("span", "tt");
    const sub = el("span", "");
    if (p.branch) sub.append(el("code", "", p.branch), " · ");
    sub.append(T(`${p.agents.length} 个 agent`, `${p.agents.length} agent${p.agents.length === 1 ? "" : "s"}`));
    t2.append(el("b", "", p.name), sub);
    l.append(el("span", "avatar sys proj"), t2, el("span", "num", money2(cost)), el("span"));
    lines.append(l);
  }
  col.append(block(T("会话花费", "Session spend"), deck.today ? T(`今天所有会话共 ${money2(deck.today)}`, `${money2(deck.today)} today across all sessions`) : "",
    lines.childNodes.length ? lines : el("div", "tquiet", T("还没有花费", "Nothing spent yet"))));

  const top = all.filter((a) => a.cost).sort((x, y) => y.cost - x.cost).slice(0, 3);
  if (top.length) {
    const tl = el("div", "tlines");
    for (const a of top) {
      const l = el("div", "tline");
      const t2 = el("span", "tt");
      t2.append(el("b", "", label(a)), el("span", "", [a.project.name, a.model || a.kind, a.tokens_in ? T(`${Math.round(a.tokens_in / 1000)}k 输入`, `${Math.round(a.tokens_in / 1000)}k input`) : ""].filter(Boolean).join(" · ")));
      l.append(avatar(a, true), t2, el("span", "num", money2(a.cost)), el("span"));
      l.onclick = () => show(a.id);
      tl.append(l);
    }
    col.append(block(T("花得最多的 agent", "Top spenders"), "", tl));
  }

  if (s && s.day) {
    const part = s.part || 0;
    const b = el("div", "budget");
    const bar = el("div", "qbar");
    const fill = el("i", s.closed ? "over" : part >= 0.8 ? "hot" : "");
    fill.style.width = `${Math.min(100, Math.round(part * 100))}%`;
    bar.append(fill);
    const line = el("div", "bline");
    line.append(el("span", "", T(`${s.turns} / ${s.budget_turns || "∞"} 轮`, `${s.turns} / ${s.budget_turns || "∞"} turns`)));
    if (s.closed) line.append(el("span", "over", T("已用完，agent 在等", "Spent: the agents wait")));
    else if (s.open_today) line.append(el("span", "", T("今天不限", "No limit today")));
    const acts = el("span", "bacts");
    acts.append(budgetAct(T("加 $10", "Add $10"), "sm primary", { op: "raise", usd: 10 }));
    line.append(acts);
    b.append(bar, line);
    const k = s.budget_usd ? T(`每天 ${money2(s.budget_usd)} · 已用 ${money2(s.usd)}`, `${money2(s.budget_usd)} a day · ${money2(s.usd)} used`) : T(`已用 ${money2(s.usd)}`, `${money2(s.usd)} used`);
    col.append(block(T("mad 自己的 agent 预算", "mad’s own agent budget"), k, b));
  }
  page.append(col);
  root.replaceChildren(page);
}
