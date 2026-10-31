#!/usr/bin/env node
// Prints what the web page's charLook picks for some ids, as JSON: the
// expected values of MadUITests/LookTests. It reads the page's own code
// (internal/desktop/web/app.js), so it cannot drift from it.
//   node ios/MadUI/scripts/dump-looks.js [id ...]
const fs = require("fs");
const path = require("path");
const src = fs.readFileSync(path.join(__dirname, "../../../internal/desktop/web/app.js"), "utf8");
const from = src.indexOf("const CHAR_SHAPES");
const to = src.indexOf("// charSVG is");
const look = new Function(`${src.slice(from, to)}
  return (key) => {
    const l = charLook(key);
    return { id: key, hash: charHash(key), shape: CHAR_SHAPES.indexOf(l.shape), name: l.shape[0],
      face: CHAR_FACES.indexOf(l.face), color: l.color };
  };`)();
const ids = process.argv.slice(2);
const sample = ids.length ? ids : ["architect", "builder", "reviewer", "claude-cc23", "claude-3fe9", "codex 32d23a28",
  "a", "", "?", "agent-0", "agent-1", "agent-2", "agent-3", "agent-4", "agent-5", "agent-6", "agent-7", "你好", "😀 mad"];
console.log(JSON.stringify(sample.map(look), null, 1));
