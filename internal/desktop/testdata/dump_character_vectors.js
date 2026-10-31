// Writes web/testdata/character-vectors.json: for some ids, what the page's characters.js
// makes of them. Run it from internal/desktop:  node testdata/dump_character_vectors.js
// The SwiftUI port reads that file; character_check.js (run by TestCharacters) fails when the page's
// plan() no longer gives it.
"use strict";
const fs = require("fs");
const { charLook, plan } = require("../web/characters.js");

const ids = [
  "work0001-aaaa", "need0001-bbbb", "stop0001-cccc", "idle0001-dddd", "unti0001-eeee", "cjk00001-ffff", "emoji001-gggg",
  "team0001-arch", "team0001-bld0", "team0001-rev0", "fold0001-aaaa", "fold0002-aaaa", "many0001-aaaa", "usage001-aaaa",
  "3f2b9c1e-7a4d-4c66-9b0e-1d2f3a4b5c6d", "a", "0", "claude 5de5801b", "🚀 发布助手", "给我绘制一个表格",
];
// and, for each shape the ids above do not have, the first "bot-N" that does
for (let shape = 0; shape < 8; shape++) {
  if (ids.some((id) => charLook(id).shape === shape)) continue;
  for (let n = 0; ; n++) if (charLook(`bot-${n}`).shape === shape) { ids.push(`bot-${n}`); break; }
}
const out = ids.map((id) => {
  const { shape, colour } = charLook(id);
  const { style, dur, reach, tilt, pts, order, beat, delay, quirk } = plan(id);
  return { id, shape, colour, style, dur, reach, tilt, order, beat, delay, quirk, pts };
});
fs.mkdirSync(`${__dirname}/../web/testdata`, { recursive: true });
fs.writeFileSync(`${__dirname}/../web/testdata/character-vectors.json`, JSON.stringify(out, null, 1) + "\n");
console.log(`${out.length} ids`);
