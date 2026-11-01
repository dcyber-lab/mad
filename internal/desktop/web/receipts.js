// The words of a receipt: the line under one of the person's own messages in a channel that says who it
// woke and how far it got. This file is the one place the wording lives (the native views copy it):
//   ◷ 待送达  not yet in the agent's digest      ✓ 已送达  delivered to the agent
//   ✓✓ 已读   the agent read it                   ↩ 已回复  the agent replied
// Parts are joined with " · ", the furthest state first. A message that woke no one says so, and a message
// still waiting in a paused channel says it is held.

const RECEIPT_STATES = [ // furthest first
  { state: "replied", mark: "↩", word: "已回复" },
  { state: "read", mark: "✓✓", word: "已读" },
  { state: "delivered", mark: "✓", word: "已送达" },
  { state: "waiting", mark: "◷", word: "待送达" },
];
const RECEIPT_NONE = "没有叫醒谁，@ 一个 agent 才会叫醒它";
const RECEIPT_HELD = "群已暂停，消息先存着";
const RECEIPT_TITLE = "◷ 待送达 · ✓ 已送达给 agent · ✓✓ 已读 · ↩ 已回复";
const RECEIPT_NONE_TITLE = "没有 @ 的消息不会叫醒谁；agent 只在读这个群的时候才看到它。";

// receiptLine is the text, tooltip and kind of the line for a message's targets ([{id, label, state}]).
function receiptLine(targets, paused) {
  const list = targets || [];
  if (!list.length) return { text: RECEIPT_NONE, title: RECEIPT_NONE_TITLE, none: true };
  const parts = [];
  for (const s of RECEIPT_STATES) {
    const names = list.filter((t) => t.state === s.state).map((t) => t.label);
    if (names.length) parts.push(`${s.mark} ${s.word} ${names.join("、")}`);
  }
  let text = parts.join(" · ");
  if (paused && list.some((t) => t.state === "waiting")) text += ` · ${RECEIPT_HELD}`;
  return { text, title: RECEIPT_TITLE, none: false };
}

if (typeof module !== "undefined") module.exports = { receiptLine, RECEIPT_NONE, RECEIPT_HELD };
