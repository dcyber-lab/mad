// A check of web/receipts.js (the words of a receipt), run by receipts_test.go with node.
const assert = require("node:assert");
const { receiptLine, RECEIPT_NONE, RECEIPT_HELD } = require("../web/receipts.js");
const t = (label, state) => ({ id: label, label, state });

function check(name, fn) {
  fn();
  console.log("ok", name);
}

check("one state each, furthest first", () => {
  const r = receiptLine([t("kim", "waiting"), t("reviewer", "delivered"), t("builder", "read"), t("architect", "replied")], false);
  assert.strictEqual(r.text, "↩ 已回复 architect · ✓✓ 已读 builder · ✓ 已送达 reviewer · ◷ 待送达 kim");
  assert.strictEqual(r.none, false);
});
check("several targets in a state share its mark", () => {
  assert.strictEqual(receiptLine([t("builder", "delivered"), t("reviewer", "delivered")], false).text, "✓ 已送达 builder、reviewer");
});
check("no targets: it woke no one", () => {
  const r = receiptLine([], false);
  assert.strictEqual(r.text, "没有叫醒谁，@ 一个 agent 才会叫醒它");
  assert.strictEqual(r.text, RECEIPT_NONE);
  assert.strictEqual(r.none, true);
  assert.strictEqual(receiptLine(undefined, true).text, RECEIPT_NONE, "paused changes nothing for a message that woke no one");
});
check("paused with someone waiting: held", () => {
  assert.strictEqual(receiptLine([t("reviewer", "waiting")], true).text, "◷ 待送达 reviewer · 群已暂停，消息先存着");
  assert.strictEqual(RECEIPT_HELD, "群已暂停，消息先存着");
});
check("paused with no one waiting: nothing is held", () => {
  assert.strictEqual(receiptLine([t("builder", "read")], true).text, "✓✓ 已读 builder");
});
check("the tooltip says what the marks mean", () => {
  assert.strictEqual(receiptLine([t("builder", "read")], false).title, "◷ 待送达 · ✓ 已送达给 agent · ✓✓ 已读 · ↩ 已回复");
});
