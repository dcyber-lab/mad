// The words of a receipt: the one quiet line under the person's latest own message in a channel, which says
// whether it reached anyone and how far (the DingTalk style: a count of who has not seen it), and the card
// that lists who. This file is the one place the wording lives; the native views copy it exactly.
//
// A target's state is waiting (not yet in the agent's digest), delivered (in it), read, or replied.
// "Seen" is read or replied; "unseen" is waiting or delivered. The word, in this order of precedence:
//   no targets                      没有叫醒谁                  grey
//   paused, and any target waiting  群已暂停，恢复后送达         grey
//   every target waiting            待送达                      grey
//   any unseen: one target          未读                        blue
//               several             N 个未读 (N = the unseen)    blue
//   all seen:   any replied         已回复                      grey
//               one target, read    已读                        grey
//               several, none replied  全部已读                  grey
// No glyphs, no names in the line, no box.

const RECEIPT_WORDS = {
  none: "没有叫醒谁",
  held: "群已暂停，恢复后送达",
  waiting: "待送达",
  unread: "未读",
  unreadN: (n) => `${n} 个未读`,
  replied: "已回复",
  read: "已读",
  allRead: "全部已读",
};
// The state of each target in the card (right of its name), and in the line's title and aria-label.
const RECEIPT_ROW_STATE = {
  replied: "已回复",
  read: "已读",
  delivered: "已送达，还没看",
  waiting: "待送达 · 忙完这一轮就会收到",
};
const RECEIPT_TITLE_STATE = { replied: "已回复", read: "已读", delivered: "已送达", waiting: "待送达" };
const RECEIPT_LABEL_STATE = { replied: "已回复", read: "已读", delivered: "未读", waiting: "待送达" };
const RECEIPT_FOOTER = "只显示被 @ 到的 agent";
const RECEIPT_NONE_CARD = "没有 @ 任何 agent，所以没有叫醒谁。@ 一个 agent 才会叫醒它。";
const RECEIPT_ORDER = ["replied", "read", "delivered", "waiting"]; // the card's rows, furthest first

// receiptSummary is everything the line and the card need for a message's targets ([{id, label, state}]):
//   word, tone ("grey" | "blue"), title (the breakdown), label (the breakdown in words, for a screen reader),
//   rows (the card's: [{id, label, state, text}]), held, none, footer.
function receiptSummary(targets, paused) {
  const list = targets || [];
  const rows = RECEIPT_ORDER.flatMap((st) =>
    list.filter((t) => t.state === st).map((t) => ({ id: t.id, label: t.label, state: st, text: RECEIPT_ROW_STATE[st] })));
  const waiting = list.filter((t) => t.state === "waiting").length;
  const unseen = list.filter((t) => t.state === "waiting" || t.state === "delivered").length;
  const held = !!paused && waiting > 0;
  let word, tone = "grey";
  if (!list.length) word = RECEIPT_WORDS.none;
  else if (held) word = RECEIPT_WORDS.held;
  else if (waiting === list.length) word = RECEIPT_WORDS.waiting;
  else if (unseen) {
    word = list.length === 1 ? RECEIPT_WORDS.unread : RECEIPT_WORDS.unreadN(unseen);
    tone = "blue";
  } else if (list.some((t) => t.state === "replied")) word = RECEIPT_WORDS.replied;
  else word = list.length === 1 ? RECEIPT_WORDS.read : RECEIPT_WORDS.allRead;
  const none = !list.length;
  const title = none ? RECEIPT_NONE_CARD : rows.map((r) => `${r.label} ${RECEIPT_TITLE_STATE[r.state]}`).join(" · ");
  const label = none ? RECEIPT_WORDS.none
    : rows.map((r) => `${r.label} ${RECEIPT_LABEL_STATE[r.state]}`).join("，") + (held ? `，${RECEIPT_WORDS.held}` : "");
  return { word, tone, title, label, rows, held, none, footer: none ? RECEIPT_NONE_CARD : RECEIPT_FOOTER };
}

if (typeof module !== "undefined") module.exports = { receiptSummary, RECEIPT_WORDS, RECEIPT_ROW_STATE, RECEIPT_FOOTER, RECEIPT_NONE_CARD };
