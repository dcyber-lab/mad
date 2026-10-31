// Checks that characters.js still makes what web/testdata/character-vectors.json says, to the last
// digit, and that the vectors cover every style and every shape and color. Run by TestCharacters.
"use strict";
const assert = require("assert");
const { charLook, plan, charSVG, EXPR, SHAPES, COLORS, STRONG, mix } = require("../web/characters.js");
const vectors = require("../web/testdata/character-vectors.json");

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
console.log(`ok ${vectors.length} vectors`);
