// A check of the page's @you rule (YOU_MENTION in web/bus.js) against the table the server's rule is held to
// too (internal/bus/testdata/at-you.json), run by atyou_test.go with node.
const fs = require("node:fs");
const assert = require("node:assert");
const src = fs.readFileSync(__dirname + "/../web/bus.js", "utf8");
const m = src.match(/const YOU_MENTION = (\/.*\/[a-z]*);/);
assert.ok(m, "bus.js has no YOU_MENTION");
const re = eval(m[1]);
const table = JSON.parse(fs.readFileSync(__dirname + "/../../bus/testdata/at-you.json", "utf8"));
let n = 0;
for (const c of table.cases) {
  assert.strictEqual(re.test(c.text), c.want, JSON.stringify(c.text) + " should be " + c.want);
  n++;
}
console.log(`ok ${n} cases`);
