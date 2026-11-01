// A check of web/receipts.js (the words of a receipt), run by receipts_test.go with node: every case of the
// precedence, the card's rows, the title and the label.
const assert = require("node:assert");
const { receiptSummary, RECEIPT_WORDS, RECEIPT_FOOTER, RECEIPT_NONE_CARD } = require("../web/receipts.js");
const t = (label, state) => ({ id: label + "-id", label, state });

let n = 0;
function check(name, fn) {
  fn();
  n++;
  console.log("ok", name);
}
const word = (targets, paused) => {
  const s = receiptSummary(targets, paused);
  return `${s.word}|${s.tone}`;
};

check("1. no targets: 没有叫醒谁, grey; paused changes nothing", () => {
  assert.strictEqual(word([], false), "没有叫醒谁|grey");
  assert.strictEqual(word(undefined, true), "没有叫醒谁|grey");
  const s = receiptSummary([], false);
  assert.strictEqual(s.none, true);
  assert.deepStrictEqual(s.rows, []);
  assert.strictEqual(s.footer, RECEIPT_NONE_CARD);
  assert.strictEqual(RECEIPT_NONE_CARD, "没有 @ 任何 agent，所以没有叫醒谁。@ 一个 agent 才会叫醒它。");
});
check("2. paused and any target waiting: 群已暂停，恢复后送达, grey, even with others unseen or read", () => {
  assert.strictEqual(word([t("a", "waiting")], true), "群已暂停，恢复后送达|grey");
  assert.strictEqual(word([t("a", "waiting"), t("b", "delivered")], true), "群已暂停，恢复后送达|grey");
  assert.strictEqual(word([t("a", "waiting"), t("b", "read")], true), "群已暂停，恢复后送达|grey");
  assert.strictEqual(receiptSummary([t("a", "waiting")], true).held, true);
});
check("2b. paused with no one waiting follows the normal rules", () => {
  assert.strictEqual(word([t("a", "delivered")], true), "未读|blue");
  assert.strictEqual(word([t("a", "read"), t("b", "read")], true), "全部已读|grey");
  assert.strictEqual(receiptSummary([t("a", "read")], true).held, false);
});
check("3. every target waiting: 待送达, grey", () => {
  assert.strictEqual(word([t("a", "waiting")], false), "待送达|grey");
  assert.strictEqual(word([t("a", "waiting"), t("b", "waiting")], false), "待送达|grey");
});
check("4. any unseen: 未读 for one target, N 个未读 for several (N = the unseen), blue", () => {
  assert.strictEqual(word([t("a", "delivered")], false), "未读|blue");
  assert.strictEqual(word([t("a", "delivered"), t("b", "delivered")], false), "2 个未读|blue");
  assert.strictEqual(word([t("a", "delivered"), t("b", "read"), t("c", "replied")], false), "1 个未读|blue");
  assert.strictEqual(word([t("a", "waiting"), t("b", "delivered"), t("c", "read")], false), "2 个未读|blue", "waiting counts as unseen when not every target waits");
  assert.strictEqual(word([t("a", "waiting"), t("b", "read")], false), "1 个未读|blue");
});
check("5. all seen: 已回复 if any replied, 已读 for one target, 全部已读 for several", () => {
  assert.strictEqual(word([t("a", "replied")], false), "已回复|grey");
  assert.strictEqual(word([t("a", "read"), t("b", "replied")], false), "已回复|grey");
  assert.strictEqual(word([t("a", "read")], false), "已读|grey");
  assert.strictEqual(word([t("a", "read"), t("b", "read")], false), "全部已读|grey");
});
check("the card's rows: replied, read, delivered, waiting, each with its text", () => {
  const s = receiptSummary([t("kim", "waiting"), t("reviewer", "delivered"), t("builder", "read"), t("architect", "replied")], false);
  assert.deepStrictEqual(s.rows.map((r) => [r.label, r.text]), [
    ["architect", "已回复"], ["builder", "已读"], ["reviewer", "已送达，还没看"], ["kim", "待送达 · 忙完这一轮就会收到"],
  ]);
  assert.strictEqual(s.footer, RECEIPT_FOOTER);
  assert.strictEqual(RECEIPT_FOOTER, "只显示被 @ 到的 agent");
});
check("the title is the breakdown; the label is the breakdown in words", () => {
  const s = receiptSummary([t("architect", "replied"), t("builder", "read"), t("reviewer", "delivered")], false);
  assert.strictEqual(s.title, "architect 已回复 · builder 已读 · reviewer 已送达");
  assert.strictEqual(receiptSummary([t("builder", "read"), t("reviewer", "delivered")], false).label, "builder 已读，reviewer 未读");
  assert.strictEqual(receiptSummary([t("reviewer", "waiting")], true).label, "reviewer 待送达，群已暂停，恢复后送达");
});
check("the words are the spec's", () => {
  assert.deepStrictEqual(
    [RECEIPT_WORDS.none, RECEIPT_WORDS.held, RECEIPT_WORDS.waiting, RECEIPT_WORDS.unread, RECEIPT_WORDS.unreadN(3), RECEIPT_WORDS.replied, RECEIPT_WORDS.read, RECEIPT_WORDS.allRead],
    ["没有叫醒谁", "群已暂停，恢复后送达", "待送达", "未读", "3 个未读", "已回复", "已读", "全部已读"]);
});
console.log(`${n} checks`);
