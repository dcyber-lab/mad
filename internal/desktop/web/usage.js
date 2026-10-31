// The usage page of a phone: for each kind, the 5 hour and weekly windows of
// the snapshot's quota, each as a big percent, a bar, what is left and when
// it resets. The footer's small bars (drawQuota) are not drawn on a phone.
"use strict";

const us = { open: false };

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

function drawUsage() {
  if (!us.open) return;
  const root = $("#usageview");
  root.replaceChildren();
  const page = el("div", "uspage");
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
  if (!n) page.append(el("p", "muted", "No usage figures yet."));
  root.append(page);
}
