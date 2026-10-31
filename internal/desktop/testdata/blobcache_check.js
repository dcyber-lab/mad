// A check of web/blobs.js (BlobCache), run by blobs_test.go with node: eviction, sharing, and that
// no object URL is left un-revoked.
"use strict";
const assert = require("assert");
const { BlobCache } = require("../web/blobs.js");

function make(budget, shownSet = new Set()) {
  const live = new Set();
  let n = 0;
  const c = new BlobCache({
    budget,
    create: () => { const u = `blob:${++n}`; live.add(u); return u; },
    revoke: (u) => { assert(live.delete(u), `${u} revoked twice or never made`); },
    shown: (u) => shownSet.has(u),
  });
  return { c, live, shownSet };
}
const blob = (size) => ({ size });
const ok = (name) => console.log("ok", name);

(async () => {
  // eviction by bytes, least recently used first
  {
    const { c, live } = make(100);
    for (const k of ["a", "b", "c", "d", "e"]) await c.fetch(k, async () => blob(30));
    assert.strictEqual(live.size, 3, `5 pictures of 30 in a budget of 100: ${live.size} URLs`);
    assert(c.bytes <= 100);
    assert.deepStrictEqual([...c.shared.keys()], ["c", "d", "e"]);
    // using one makes it the latest
    await c.fetch("c", async () => assert.fail("a cached picture was fetched again"));
    await c.fetch("f", async () => blob(30));
    assert.deepStrictEqual([...c.shared.keys()], ["e", "c", "f"], "order");
    assert(!c.shared.has("d"), "d was the least recently used");
    ok("eviction by bytes, least recently used first");
  }
  // one picture over the whole budget is kept, alone
  {
    const { c, live } = make(100);
    await c.fetch("a", async () => blob(30));
    await c.fetch("big", async () => blob(500));
    assert.deepStrictEqual([...c.shared.keys()], ["big"]);
    assert.strictEqual(live.size, 1);
    ok("a picture bigger than the budget is kept alone");
  }
  // a URL still shown is revoked when nothing shows it, by sweep
  {
    const { c, live, shownSet } = make(50);
    const a = await c.fetch("a", async () => blob(30));
    shownSet.add(a);
    await c.fetch("b", async () => blob(30)); // a leaves the cache but is shown
    assert(!c.shared.has("a"));
    assert(live.has(a), "a shown URL was revoked");
    c.sweep();
    assert(live.has(a), "swept while shown");
    shownSet.delete(a);
    c.sweep();
    assert(!live.has(a), "not revoked once nothing shows it");
    assert.strictEqual(live.size, 1);
    ok("an evicted URL that is shown is revoked once it is not");
  }
  // the same picture asked twice at once is fetched once
  {
    const { c } = make(100);
    let calls = 0;
    const get = async () => { calls++; await new Promise((r) => setTimeout(r, 5)); return blob(10); };
    const [x, y] = await Promise.all([c.fetch("a", get), c.fetch("a", get)]);
    assert.strictEqual(x, y);
    assert.strictEqual(calls, 1);
    ok("one fetch for two askers");
  }
  // a fetch that fails leaves nothing, and can be tried again
  {
    const { c, live } = make(100);
    await assert.rejects(c.fetch("a", async () => { throw new Error("no"); }));
    assert.strictEqual(c.pending.size, 0);
    await c.fetch("a", async () => blob(10));
    assert.strictEqual(live.size, 1);
    ok("a failed fetch leaves nothing");
  }
  // a picture of its own img: replaced, and let go of when the img has left
  {
    const { c, live, shownSet } = make(100);
    const img = { isConnected: true };
    const u1 = c.own(img, blob(10));
    const u2 = c.own(img, blob(10));
    assert(!live.has(u1) && live.has(u2), "the URL an img left was not revoked");
    c.sweep();
    assert(live.has(u2), "swept while its img is on the page");
    img.isConnected = false;
    shownSet.add(u2);
    c.sweep();
    assert(live.has(u2), "swept while still shown");
    shownSet.delete(u2);
    c.sweep();
    assert.strictEqual(live.size, 0);
    ok("an img's own URL goes with it");
  }
  // opening and closing a pictures page ten times, and a thousand pictures: bounded, none leaked
  {
    const { c, live, shownSet } = make(10 * 30);
    for (let round = 0; round < 10; round++) {
      for (let i = 0; i < 100; i++) {
        const u = await c.fetch(`p${round}-${i}`, async () => blob(30));
        shownSet.add(u); // drawn
      }
      shownSet.clear(); // the page closed
      c.sweep();
      assert(live.size <= 10, `round ${round}: ${live.size} URLs live`);
    }
    for (const k of [...c.shared.keys()]) c.drop(k);
    c.sweep();
    assert.strictEqual(live.size, 0, `${live.size} URLs left`);
    ok("bounded over ten rounds, none left");
  }
})().catch((e) => { console.error("FAIL", e.message); process.exit(1); });
