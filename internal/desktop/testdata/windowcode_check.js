// A check of web/pair.js's window code (#code=), run by windowcode_test.go
// with node: the code is traded once at /api/window/token, and the
// fragment then holds #token= (with &win= kept) for app.js, as a window
// opened with the token had; a code refused leaves no token and says why.
"use strict";
const assert = require("assert");
const fs = require("fs");
const path = require("path");
const vm = require("vm");
const src = fs.readFileSync(path.join(__dirname, "../web/pair.js"), "utf8");
const ok = (name) => console.log("ok", name);

async function load(hash, answer) {
  const calls = [];
  const loaded = [];
  const ctx = {
    location: { hash, pathname: "/", search: "" },
    history: {
      replaceState(_s, _t, url) {
        const i = url.indexOf("#");
        ctx.location.hash = i < 0 ? "" : url.slice(i);
      },
    },
    fetch: async (url, opts) => {
      calls.push({ url, body: opts && opts.body });
      return answer;
    },
    sessionStorage: { getItem: () => null, setItem() {}, removeItem() {} },
    document: { createElement: () => ({}), body: { append: (s) => loaded.push(s.src) } },
    T: (_zh, en) => en,
    URLSearchParams,
    crypto: { getRandomValues: (b) => b },
    navigator: { userAgent: "" },
    AbortController,
    setTimeout,
    clearTimeout,
  };
  ctx.window = ctx;
  vm.createContext(ctx);
  vm.runInContext(src, ctx);
  for (let i = 0; i < 50 && !loaded.includes("app.js"); i++) await new Promise((r) => setTimeout(r, 1));
  return { calls, hash: ctx.location.hash, error: ctx.madPairError, loaded };
}

(async () => {
  const tok = "0123456789abcdef0123456789abcdef";
  let r = await load("#code=c0de&win=w1", { ok: true, status: 200, json: async () => ({ token: tok }) });
  assert.strictEqual(r.calls.length, 1);
  assert.strictEqual(r.calls[0].url, "/api/window/token");
  assert.deepStrictEqual(JSON.parse(r.calls[0].body), { code: "c0de" });
  const h = new URLSearchParams(r.hash.slice(1));
  assert.strictEqual(h.get("token"), tok);
  assert.strictEqual(h.get("win"), "w1");
  assert.strictEqual(h.get("code"), null);
  assert.strictEqual(r.error, "");
  assert.ok(r.loaded.includes("app.js"));
  ok("a window code is traded for the token");

  r = await load("#code=used&win=w1", { ok: false, status: 403 });
  const h2 = new URLSearchParams(r.hash.slice(1));
  assert.strictEqual(h2.get("token"), null);
  assert.strictEqual(h2.get("code"), null);
  assert.strictEqual(h2.get("win"), "w1");
  assert.ok(r.error.includes("mad desktop"));
  assert.ok(r.loaded.includes("app.js"));
  ok("a refused code leaves no token and says why");

  r = await load("#token=" + tok, { ok: true });
  assert.strictEqual(r.calls.length, 0);
  assert.strictEqual(new URLSearchParams(r.hash.slice(1)).get("token"), tok);
  ok("#token= is left as it was");
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
