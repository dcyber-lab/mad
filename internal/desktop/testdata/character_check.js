// Checks that characters.js still makes what web/testdata/character-vectors.json says, to the last
// digit, and that the vectors cover every style and every shape and color. Run by TestCharacters.
"use strict";
const assert = require("assert");
const { charLook, plan, charSVG, EXPR, SHAPES, COLORS, STRONG, mix } = require("../web/characters.js");
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
console.log(`ok ${vectors.length} vectors, ${rules} rules about the parts`);
