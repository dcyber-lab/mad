// The page's language: Chinese or English. It follows the browser (the
// apps' web views take the app's language) unless the person picked one in
// ⋯ → 语言 / Language, which is kept in this browser. Every word the page
// itself shows is written in both, side by side where it is used:
// T("在等你", "Waiting"). Each language is written for its readers, not
// translated word by word. Not translated: tool names, claude / codex,
// commands, what agents wrote, and the accessibility labels the apps' UI
// tests look for (切换 Mac, 更多, 搜索, 新建, needs you, working, idle,
// stopped).
"use strict";

const LANG = (() => {
  let pick = "";
  try { pick = localStorage.getItem("mad.lang") || ""; } catch (_) { /* private window */ }
  if (pick === "zh" || pick === "en") return pick;
  const nav = (navigator.languages && navigator.languages[0]) || navigator.language || "zh";
  return /^zh\b/i.test(nav) ? "zh" : "en";
})();
document.documentElement.lang = LANG === "zh" ? "zh-CN" : "en";

// T is the text in the page's language: zh, else en (en may be left out
// where both read the same).
function T(zh, en) {
  return LANG === "en" && en !== undefined ? en : zh;
}

// langPick is what ⋯ → Language keeps: "auto", "zh" or "en".
function langPick() {
  try { return localStorage.getItem("mad.lang") || "auto"; } catch (_) { return "auto"; }
}

// setLang keeps pick and draws the page again in it.
function setLang(pick) {
  try {
    if (pick === "auto") localStorage.removeItem("mad.lang");
    else localStorage.setItem("mad.lang", pick);
  } catch (_) { /* private window: this visit only */ }
  location.reload();
}

// The page's own HTML is written in Chinese; an element with data-en says
// it in English (its text), data-en-title its title, data-en-ph its
// placeholder.
function translateStatic(root) {
  if (LANG !== "en") return;
  for (const n of root.querySelectorAll("[data-en]")) n.textContent = n.dataset.en;
  for (const n of root.querySelectorAll("[data-en-title]")) n.title = n.dataset.enTitle;
  for (const n of root.querySelectorAll("[data-en-ph]")) n.placeholder = n.dataset.enPh;
}
translateStatic(document);

if (typeof module !== "undefined") module.exports = { T, LANG };
