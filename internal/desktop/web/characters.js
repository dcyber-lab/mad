// The characters: one of eight shapes in a pastel color, a face in a deep shade of the bot's
// strong color, and, by its state, an expression and a way of moving of its own. Which shape and
// color an agent has comes from a hash of its id (so it looks the same everywhere); how it looks
// around while it works comes from `plan(id)`, a function of the id alone (xorshift seeded by the
// same hash), which the SwiftUI app ports exactly: its results are in testdata/character-vectors.json
// (made by testdata/dump_character_vectors.js, checked by character_check.js).
"use strict";

// SHAPES: [name, how far down the face sits, the body]; the order is the hash's.
const SHAPES = [
  ["圆石", 0, '<circle class="body" cx="24" cy="26" r="17"/>'],
  ["吐司", 0, '<path class="body" d="M9 20 a9.5 9.5 0 0 1 9.5 -11 h11 a9.5 9.5 0 0 1 9.5 11 v15.5 a6 6 0 0 1 -6 6 h-18 a6 6 0 0 1 -6 -6 z"/>'],
  ["豆子", 0, '<ellipse class="body" cx="24" cy="27" rx="19" ry="14" transform="rotate(-8 24 27)"/>'],
  ["水滴", 4, '<path class="body" d="M24 6 C33 16 40 23 40 31 a16 16 0 0 1 -32 0 C8 23 15 16 24 6 z"/>'],
  ["小猫", 4, '<path class="body" d="M9 40 V17 L15 9 L20 14.5 h8 L33 9 L39 17 V40 a3.5 3.5 0 0 1 -3.5 3.5 h-23 a3.5 3.5 0 0 1 -3.5 -3.5 z"/>'],
  ["六边形", 0, '<path class="body" d="M24 7 l14 8 a3.5 3.5 0 0 1 1.8 3 v13.6 a3.5 3.5 0 0 1 -1.8 3 L24 43 l-14 -8 a3.5 3.5 0 0 1 -1.8 -3 V18 a3.5 3.5 0 0 1 1.8 -3 z"/>'],
  ["云朵", 0, '<path class="body" d="M12.5 40 a7.5 7.5 0 0 1 -3 -14.2 a9.6 9.6 0 0 1 13.6 -11.5 a9.6 9.6 0 0 1 15.4 5.8 a7.6 7.6 0 0 1 0 19.9 z"/>'],
  ["豆芽", 4, '<g class="leaf"><path d="M24 15 C22.5 9 17 7 12.5 8.5 C14 13.5 19 15 24 15 Z" fill="#bfe8b0"/><path d="M24 15 C25.5 8 31 6 35.5 7.5 C34 13.5 29 15 24 15 Z" fill="#d6f2c9"/></g><rect class="body" x="10" y="15" width="28" height="28" rx="14"/>'],
];

// the body's pastel, and the bot's strong color (the face is that, darkened 55%)
const COLORS = ["#cfe0ff", "#ffe1c2", "#c6f0df", "#e6d9ff", "#ffd6e4", "#fff0b8", "#cdeff7", "#dcf4cf"];
const STRONG = ["#6f9bff", "#ffa04d", "#2fbf8c", "#a77bff", "#ff7aa5", "#f2bd1d", "#3fc3e0", "#7dcc5a"];

// mix darkens a #rrggbb color toward black by t (0 to 1).
function mix(hex, t) {
  const n = parseInt(hex.slice(1), 16), c = [n >> 16, (n >> 8) & 255, n & 255];
  return "#" + c.map((v) => Math.round(v * (1 - t)).toString(16).padStart(2, "0")).join("");
}

// charHash is FNV-1a over the code points of key.
function charHash(key) {
  let h = 2166136261;
  for (const c of String(key)) h = Math.imul(h ^ c.codePointAt(0), 16777619) >>> 0;
  return h;
}

// charLook is which shape and which color a key has.
function charLook(key) {
  const h = charHash(key);
  return { shape: h % 8, colour: (h >>> 3) % 8 };
}

// rng is xorshift32 from seed, giving numbers in [0, 1).
function rng(seed) {
  let s = seed || 1;
  return () => {
    s ^= s << 13; s >>>= 0;
    s ^= s >> 17;
    s ^= s << 5; s >>>= 0;
    return (s % 100000) / 100000;
  };
}

// plan is how the character of id moves while it works: how it looks around (style), over how many
// milliseconds (dur), how far (reach) and how much its head tilts (tilt), the points its gaze goes
// through, [x, y, t] with t in 0..1 of the cycle, the order of its expressions and how long each
// stays (beat), when it begins (delay) and the pace of its small bob and its leaf (quirk).
function plan(id) {
  const r = rng(charHash(id));
  const style = ["wander", "circle", "scan", "peek"][Math.floor(r() * 4)];
  const dur = 2800 + r() * 3600, reach = 2.2 + r() * 1.6, tilt = 3 + r() * 6, pts = [];
  if (style === "circle") {
    const dir = r() < 0.5 ? 1 : -1, st = r() * Math.PI * 2;
    for (let i = 0; i <= 10; i++) {
      const a = st + dir * i / 10 * Math.PI * 2;
      pts.push([Math.cos(a) * reach, Math.sin(a) * reach * 0.8, i / 10]);
    }
  } else if (style === "scan") {
    const c = [[-1, -1], [1, -1], [1, 1], [-1, 1]], o = Math.floor(r() * 4);
    pts.push([0, 0, 0]);
    for (let i = 0; i < 4; i++) {
      const [x, y] = c[(i + o) % 4], t = 0.1 + i * 0.2;
      pts.push([x * reach * 0.85, y * reach * 0.7, t], [x * reach * 0.85, y * reach * 0.7, t + 0.12]);
    }
    pts.push([0, 0, 1]);
  } else if (style === "peek") {
    const a = r() * Math.PI * 2, b = a + Math.PI * (0.6 + r() * 0.8);
    pts.push([0, 0, 0], [0, 0, 0.32], [Math.cos(a) * reach, Math.sin(a) * reach * 0.8, 0.38], [Math.cos(a) * reach, Math.sin(a) * reach * 0.8, 0.5], [0, 0, 0.58], [0, 0, 0.78],
      [Math.cos(b) * reach * 0.8, Math.sin(b) * reach * 0.7, 0.83], [0, 0, 0.92], [0, 0, 1]);
  } else {
    pts.push([0, 0, 0]);
    let t = 0;
    const n = 4 + Math.floor(r() * 3);
    for (let i = 0; i < n; i++) {
      const a = r() * Math.PI * 2, d = reach * (0.5 + r() * 0.5);
      t += 0.7 / n;
      const x = Math.cos(a) * d, y = Math.sin(a) * d * 0.8;
      pts.push([x, y, t], [x, y, Math.min(0.95, t + 0.25 / n)]);
      t += 0.25 / n;
    }
    pts.push([0, 0, 1]);
  }
  const order = ["focus", "think", "happy", "wow", "wink"].sort(() => r() - 0.5);
  return { style, dur, reach, tilt, pts, order, beat: 1500 + r() * 2200, delay: r() * 1200, quirk: 900 + r() * 1200 };
}

if (typeof module !== "undefined") module.exports = { SHAPES, COLORS, STRONG, mix, charHash, charLook, rng, plan };
