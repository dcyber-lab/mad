// A check of web/stale.js (staleAction), run by stale_test.go with node.
"use strict";
const assert = require("assert");
const { staleAction } = require("../web/stale.js");
const ok = (name) => console.log("ok", name);

assert.strictEqual(staleAction({ mine: "v1", seen: "v1", busy: false }), "none"); ok("same build");
assert.strictEqual(staleAction({ mine: "v1", seen: "v2", busy: false }), "reload"); ok("new build, idle");
assert.strictEqual(staleAction({ mine: "v1", seen: "v2", busy: true }), "toast"); ok("new build, busy");
assert.strictEqual(staleAction({ mine: "", seen: "v2", busy: false }), "none"); ok("page without a build");
assert.strictEqual(staleAction({ mine: "v1", seen: "", busy: false }), "none"); ok("server without a build");
