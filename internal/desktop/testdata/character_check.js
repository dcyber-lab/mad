// Checks that characters.js still makes what web/testdata/character-vectors.json says, to the last
// digit, and that the vectors cover every style and every shape and color. Run by TestCharacters.
"use strict";
const assert = require("assert");
const C = require("../web/characters.js");
const { charLook, plan, charSVG, EXPR, SHAPES, COLORS, STRONG, mix } = C;
const vectors = require("../web/testdata/character-vectors.json");
const fs = require("fs");

assert(vectors.length >= 20, `${vectors.length} vectors`);
for (const v of vectors) {
  const { shape, colour } = charLook(v.id);
  assert.strictEqual(shape, v.shape, `${v.id}: shape`);
  assert.strictEqual(colour, v.colour, `${v.id}: colour`);
  const p = plan(v.id);
  for (const k of ["style", "dur", "reach", "tilt", "order", "beat", "delay", "quirk", "pts"]) {
    assert.deepStrictEqual(p[k], v[k], `${v.id}: ${k}`);
  }
}
// the drawing: the shape's body, the pastel and the face's deep color, and a calm face to begin with
for (const v of vectors) {
  const svg = charSVG(v.id);
  assert(svg.includes(SHAPES[v.shape][2]), `${v.id}: the body`);
  assert(svg.includes(`--fill:${COLORS[v.colour]};--face:${mix(STRONG[v.colour], 0.55)}`), `${v.id}: the colors`);
  assert(svg.includes(EXPR.calm), `${v.id}: the first face`);
  assert(charSVG(v.id, null, "ask", true).includes(`${EXPR.ask}`) && charSVG(v.id, null, "ask", true).includes("opacity:0.5"), `${v.id}: a stopped one and its face`);
  assert(svg.includes(`translate(24 ${23 + SHAPES[v.shape][1]})`), `${v.id}: the face's place`);
}
assert.deepStrictEqual(Object.keys(EXPR).sort(), ["ask", "calm", "focus", "happy", "sleep", "think", "wink", "wow"]);
assert(/^#[0-9a-f]{6}$/.test(mix("#6f9bff", 0.55)));
const styles = new Set(vectors.map((v) => v.style));
for (const s of ["wander", "circle", "scan", "peek"]) assert(styles.has(s), `no vector has the style ${s}`);
assert.strictEqual(new Set(vectors.map((v) => v.shape)).size, 8, "the vectors do not cover the 8 shapes");
// No rule of the page may reach a part of a character, as `.avatar .dot` (the status dot's) and
// `.avatar.chara .dot { display: none }` reached the eyes when they were `.dot`: every class a character is
// drawn with is its own (c-), and the stylesheet only paints them (fill, stroke, font), under `.chara`.
const markup = [...Object.values(EXPR), ...SHAPES.map((x) => x[2]), charSVG("x"), charSVG("x", [1, 2, 3])].join("\n");
const used = new Set([...markup.matchAll(/class="([^"]+)"/g)].flatMap((m) => m[1].split(/\s+/)));
for (const c of used) assert(c.startsWith("c-"), `a character part has the class "${c}", which another rule of the page may be about`);
const css = fs.readFileSync(`${__dirname}/../web/style.css`, "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
const painting = new Set(["fill", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin", "font"]);
let rules = 0;
for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
  const selectors = m[1].split(",").map((x) => x.trim());
  const decls = m[2].split(";").map((x) => x.split(":")[0].trim()).filter(Boolean);
  for (const sel of selectors) {
    const hit = [...used].find((c) => new RegExp(`\\.${c}(?![\\w-])`).test(sel));
    if (!hit) continue;
    rules++;
    assert(/^\.chara\s/.test(sel), `"${sel}" is about a character part but not under .chara`);
    for (const d of decls) assert(painting.has(d), `"${sel}" sets ${d} on a character part: it may hide or move it`);
  }
}
for (const part of ["c-body", "c-ink", "c-eye"]) assert(new RegExp(`\\.chara\\s+\\.${part}\\b`).test(css), `no stylesheet rule paints ${part}: the body or the face would be unpainted`);
assert(rules >= 3, `only ${rules} stylesheet rules about the character's parts`);
// and no rule of the page hides a whole character's drawing: svg inside .chara
for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
  if (/display\s*:\s*none/.test(m[2]) && m[1].split(",").some((s) => /\.chara(?![\w-])/.test(s) && !/(\.group|\.dot|\[hidden\])/.test(s))) assert.fail(`"${m[1].trim()}" hides a character`);
}
// ---- what a place drawn again keeps ----

// The key of a group tile has every member, not the three it shows: a fourth that changes is a new tile.
{
  const team = (shown4) => [{ id: "a", shown: "running" }, { id: "b", shown: "idle" }, { id: "c", shown: "idle" }, { id: "d", shown: shown4 }];
  assert.notStrictEqual(C.charTileKey(team("idle"), false), C.charTileKey(team("waiting"), false), "a 4th member that starts to wait must make a new tile");
  assert.notStrictEqual(C.charTileKey(team("idle"), false), C.charTileKey(team("stopped"), false), "a 4th member that stops must make a new tile");
  assert.strictEqual(C.charTileKey(team("idle"), false), C.charTileKey(team("idle"), false));
  assert.notStrictEqual(C.charTileKey(team("idle"), false), C.charTileKey(team("idle"), true));
  // an agent: stopped and exited are different nodes, the same state is the same
  assert.notStrictEqual(C.charAvatarKey("x", "stop", "stopped", false), C.charAvatarKey("x", "stop", "exited", false), "stopped to exited must make a new node");
  assert.strictEqual(C.charAvatarKey("x", "work", "running", false), C.charAvatarKey("x", "work", "running", false));
  assert.notStrictEqual(C.charAvatarKey("x", "work", "running", false), C.charAvatarKey("x", "idle", "idle", false));
}

// charAdopt, on a tiny tree: a character below a node, one passed itself, and the same look twice in order.
{
  class N {
    constructor(ck, kids = []) { this.dataset = ck ? { ck } : {}; this.kids = kids; this.parent = null; kids.forEach((k) => { k.parent = this; }); }
    get isAv() { return !!this.dataset.ck; }
    matches() { return this.isAv; }
    querySelectorAll() { const out = []; const walk = (n) => n.kids.forEach((k) => { if (k.isAv) out.push(k); walk(k); }); walk(this); return out; }
    replaceWith(x) { const i = this.parent.kids.indexOf(this); this.parent.kids[i] = x; x.parent = this.parent; }
  }
  const old = new N("", [new N("", [new N("a|work|running|0"), new N("a|work|running|0"), new N("b|idle|idle|0")]), new N("c|need|waiting|1")]);
  const [oa1, oa2, ob] = old.kids[0].kids, oc = old.kids[1];
  // the new list: below a row, a, a again, b with another status (so a new one); and c passed directly
  const na1 = new N("a|work|running|0"), na2 = new N("a|work|running|0"), nb = new N("b|idle|exited|0"), nc = new N("c|need|waiting|1");
  const row = new N("", [na1, na2, nb]);
  const nodes = C.charAdopt(old, [row, nc]);
  assert.strictEqual(row.kids[0], oa1, "the first a is the one that was");
  assert.strictEqual(row.kids[1], oa2, "the second a is the second that was");
  assert.strictEqual(row.kids[2], nb, "b changed its status: a new node");
  assert.strictEqual(nodes[1], oc, "a character passed itself is replaced in the list by the one that was");
  assert(!old.kids[0].kids.includes(nb) && ob);
}

// Under Reduce Motion a character that has not started yet (off the screen, waiting to wake) when it comes on
// shows the face of its state, not the one it was drawn with: a working one shows focus.
{
  const part = () => ({ innerHTML: "", style: {}, animate: () => assert.fail("a still character was animated") });
  const root = (face) => { const m = { ".c-facein": part(), ".c-head": part(), ".c-all": part(), ".c-leaf": null }; m[".c-facein"].innerHTML = face; return { m, querySelector: (q) => m[q] }; };
  C.charReduced.matches = true;
  try {
    for (const [state, face] of [["work", "focus"], ["need", "ask"], ["idle", "calm"], ["stop", "sleep"]]) {
      // drawn with another face: a working one starts with the first of its own order, and "architect"'s is wow, not focus
      assert.notStrictEqual(plan("architect").order[0], "focus", "the id must not begin with the still face, or the check shows nothing");
      const r = root(EXPR[plan("architect").order[0]]);
      assert.strictEqual(C.charStart(r, "architect", state, {}), null, `${state}: nothing is started`);
      assert.strictEqual(r.m[".c-facein"].innerHTML, EXPR[face], `${state}: a still one shows ${face}`);
    }
    assert.strictEqual(C.charBlinkers.size, 0);
    // the one source: what the page draws a working character with (charExpr) follows it too
    assert.strictEqual(C.charExpr("work", "architect"), "focus", "charExpr does not read charReduced");
    assert(charSVG("architect", null, C.charExpr("work", "architect")).includes(EXPR.focus));
  } finally {
    C.charReduced.matches = false;
  }
  assert.strictEqual(C.charExpr("work", "architect"), plan("architect").order[0], "not reduced: the first of its own order");
}

// One timeout for all the blinks: the next edge, set again when one comes or goes (not a poll).
{
  let pending = 0;
  const set = global.setTimeout, clear = global.clearTimeout;
  const live = new Set();
  global.setTimeout = (fn, ms) => { const h = set(fn, ms); live.add(h); return h; };
  global.clearTimeout = (h) => { live.delete(h); clear(h); };
  const part = () => ({ innerHTML: "", style: {}, animate: () => ({ play() {}, pause() {}, cancel() {} }) });
  const fake = () => { const m = { ".c-facein": part(), ".c-head": part(), ".c-all": part(), ".c-leaf": null }; return { querySelector: (q) => m[q], style: {}, closest: () => null, isConnected: true }; };
  const runs = [];
  for (let i = 0; i < 50; i++) runs.push(C.charStart(fake(), `idle-${i}`, "idle", {}));
  assert.strictEqual(C.charBlinkers.size, 50, "every idle one waits to blink");
  assert.strictEqual(live.size, 1, `${live.size} timeouts pending for 50 blinking characters`);
  assert(C.charBlinkTimer, "no timer is set");
  assert.strictEqual(C.charStart(fake(), "mini", "idle", { blink: false }), null, "a mini does not blink");
  for (const r of runs) r.stop();
  assert.strictEqual(C.charBlinkers.size, 0);
  assert.strictEqual(live.size, 0, "a timer is left with nothing to blink");
  assert.strictEqual(C.charBlinkTimer, 0);
  global.setTimeout = set;
  global.clearTimeout = clear;
}

console.log(`ok ${vectors.length} vectors, ${rules} rules about the parts`);
