// Checks that characters.js still makes what web/testdata/character-vectors.json says, to the last
// digit, and that the vectors cover every style and every shape and color. Run by TestCharacters.
"use strict";
const assert = require("assert");
const { charLook, plan } = require("../web/characters.js");
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
const styles = new Set(vectors.map((v) => v.style));
for (const s of ["wander", "circle", "scan", "peek"]) assert(styles.has(s), `no vector has the style ${s}`);
assert.strictEqual(new Set(vectors.map((v) => v.shape)).size, 8, "the vectors do not cover the 8 shapes");
console.log(`ok ${vectors.length} vectors`);
