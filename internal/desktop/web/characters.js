// The characters: one of eight shapes in a pastel color, a face in a deep shade of the bot's
// strong color, and, by its state, an expression and a way of moving of its own. Which shape and
// color an agent has comes from a hash of its id (so it looks the same everywhere); how it looks
// around while it works comes from `plan(id)`, a function of the id alone (xorshift seeded by the
// same hash), which the SwiftUI app ports exactly: its results are in testdata/character-vectors.json
// (made by testdata/dump_character_vectors.js, checked by character_check.js).
"use strict";

// SHAPES: [name, how far down the face sits, the body]; the order is the hash's.
const SHAPES = [
  ["圆石", 0, '<circle class="c-body" cx="24" cy="26" r="17"/>'],
  ["吐司", 0, '<path class="c-body" d="M9 20 a9.5 9.5 0 0 1 9.5 -11 h11 a9.5 9.5 0 0 1 9.5 11 v15.5 a6 6 0 0 1 -6 6 h-18 a6 6 0 0 1 -6 -6 z"/>'],
  ["豆子", 0, '<ellipse class="c-body" cx="24" cy="27" rx="19" ry="14" transform="rotate(-8 24 27)"/>'],
  ["水滴", 4, '<path class="c-body" d="M24 6 C33 16 40 23 40 31 a16 16 0 0 1 -32 0 C8 23 15 16 24 6 z"/>'],
  ["小猫", 4, '<path class="c-body" d="M9 40 V17 L15 9 L20 14.5 h8 L33 9 L39 17 V40 a3.5 3.5 0 0 1 -3.5 3.5 h-23 a3.5 3.5 0 0 1 -3.5 -3.5 z"/>'],
  ["六边形", 0, '<path class="c-body" d="M24 7 l14 8 a3.5 3.5 0 0 1 1.8 3 v13.6 a3.5 3.5 0 0 1 -1.8 3 L24 43 l-14 -8 a3.5 3.5 0 0 1 -1.8 -3 V18 a3.5 3.5 0 0 1 1.8 -3 z"/>'],
  ["云朵", 0, '<path class="c-body" d="M12.5 40 a7.5 7.5 0 0 1 -3 -14.2 a9.6 9.6 0 0 1 13.6 -11.5 a9.6 9.6 0 0 1 15.4 5.8 a7.6 7.6 0 0 1 0 19.9 z"/>'],
  ["豆芽", 4, '<g class="c-leaf"><path d="M24 15 C22.5 9 17 7 12.5 8.5 C14 13.5 19 15 24 15 Z" fill="#bfe8b0"/><path d="M24 15 C25.5 8 31 6 35.5 7.5 C34 13.5 29 15 24 15 Z" fill="#d6f2c9"/></g><rect class="c-body" x="10" y="15" width="28" height="28" rx="14"/>'],
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



// EXPR: the expressions, strokes and dots in the face's color; the face's origin (0, 0) is between the eyes.
const EXPR = {
  happy: '<path class="c-ink" d="M-7.5 1 q2.5 -3 5 0"/><path class="c-ink" d="M2.5 1 q2.5 -3 5 0"/><path class="c-ink" d="M-3.5 6 q3.5 3.5 7 0"/>',
  focus: '<circle class="c-eye" cx="-5" cy="0" r="1.9"/><circle class="c-eye" cx="5" cy="0" r="1.9"/><path class="c-ink" d="M-7.5 -4.2 l5 0.3"/><path class="c-ink" d="M7.5 -4.2 l-5 0.3"/><path class="c-ink" d="M-2.5 7 h5"/>',
  think: '<circle class="c-eye" cx="-4" cy="-1.5" r="1.9"/><circle class="c-eye" cx="6" cy="-1.5" r="1.9"/><path class="c-ink" d="M-1 7 q3 -1.5 6 0"/><path class="c-ink" d="M11 -9 q2 -3 0 -5" opacity=".7"/><circle class="c-eye" cx="12.5" cy="-1" r="1" opacity=".6"/>',
  wow: '<circle class="c-ink" cx="-5" cy="0" r="2.6"/><circle class="c-ink" cx="5" cy="0" r="2.6"/><ellipse class="c-ink" cx="0" cy="7.5" rx="2" ry="2.6"/>',
  wink: '<circle class="c-eye" cx="-5" cy="0" r="1.9"/><path class="c-ink" d="M2.5 0.5 h5"/><path class="c-ink" d="M-3.5 5.5 q3.5 3.5 7 0"/>',
  ask: '<circle class="c-eye" cx="-5" cy="0" r="1.9"/><circle class="c-eye" cx="5" cy="0" r="1.9"/><path class="c-ink" d="M-8 -5 q3 -2 6 0"/><path class="c-ink" d="M2 -5.5 q3 -2.5 6 -1"/><path class="c-ink" d="M-2.5 7.5 q1.2 -1.2 2.5 0 q1.3 1.2 2.5 0"/><text x="12" y="-9" font-size="9" font-weight="700">?</text>',
  calm: '<circle class="c-eye" cx="-5" cy="0" r="1.9"/><circle class="c-eye" cx="5" cy="0" r="1.9"/><path class="c-ink" d="M-3 6 q3 2.5 6 0"/>',
  sleep: '<path class="c-ink" d="M-7.5 0 q2.5 2 5 0"/><path class="c-ink" d="M2.5 0 q2.5 2 5 0"/><path class="c-ink" d="M-2 7 h4"/><text x="11" y="-7" font-size="7" font-weight="700" opacity=".7">z</text>',
};

// charExpr is the expression a character of key has in state ("work", "need", "idle" or "stop"), before it
// moves: a working one starts with the first of its own order (still, with reduced motion, "focus").
function charExpr(state, key) {
  const still = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
  return state === "work" ? (still ? "focus" : plan(key).order[0]) : state === "need" ? "ask" : state === "stop" ? "sleep" : "calm";
}

// charSVG is the character of key, 48 by 48 (nested at x y with width w when box is given): its body in
// the pastel (CSS --fill), its face in the deep color (--face), with the face of expr; a stopped one is
// at half opacity. It is all there is to a still one; charStart makes the others move.
function charSVG(key, box, expr = "calm", stopped = false) {
  const { shape, colour } = charLook(key);
  const [, dy, body] = SHAPES[shape];
  const style = `--fill:${COLORS[colour]};--face:${mix(STRONG[colour], 0.55)}${stopped ? ";opacity:0.5" : ""}`;
  const inner = `<g class="c-all"><g class="c-head">${body}<g class="c-face" transform="translate(24 ${23 + dy})"><g class="c-facein">${EXPR[expr]}</g></g></g></g>`;
  return box
    ? `<svg x="${box[0]}" y="${box[1]}" width="${box[2]}" height="${box[2]}" viewBox="0 0 48 48" style="${style}">${inner}</svg>`
    : `<svg viewBox="0 0 48 48" style="${style}">${inner}</svg>`;
}

// ---- how it moves: only a working one, and only while it is on the screen ----

// charLive: the characters that have timers or animations, to be let go of when they leave the page.
const charLive = new Set();
const charReduced = typeof matchMedia === "function" ? matchMedia("(prefers-reduced-motion: reduce)") : { matches: false };
// charSeen follows which characters are on the screen: one that has not moved yet is started when it first is
// (a list drawn and thrown away starts nothing), one that is moving stops while it is not.
const charSeen = typeof IntersectionObserver === "function" ? new IntersectionObserver((entries) => {
  for (const e of entries) {
    const t = e.target;
    if (e.isIntersecting && t.__wake) {
      const wake = t.__wake;
      t.__wake = null;
      wake();
    }
    t.__chars?.forEach((r) => (e.isIntersecting ? r.resume() : r.pause()));
  }
}) : null;

// charHold: while true, characters made are not yet followed: the list is built each second and kept only
// when it changed, so what is built is followed (charArm) once it is on the page.
let charHold = false;
function charHolding(on) {
  charHold = on;
}

// charWake runs start (which calls charStart for what is drawn in el) when el is first on the screen.
function charWake(el, start) {
  if (!charSeen) return start();
  el.__wake = start;
  if (!charHold) charSeen.observe(el);
}

// charArm follows the characters in root that wait to be started.
function charArm(root) {
  if (!charSeen) return;
  for (const el of root.querySelectorAll(".avatar.chara")) {
    if (el.__wake && !el.__armed) {
      el.__armed = true;
      charSeen.observe(el);
    }
  }
}

// charAdopt keeps what moves: a list drawn again has new characters; where one of the same look (data-ck: the
// agent and its state, or a group's members and theirs) was in the list before, in the same order, that one
// goes into the new list instead, with its motion running. Call it before the new nodes replace the old.
function charAdopt(old, nodes) {
  const live = new Map();
  for (const e of old.querySelectorAll(".avatar.chara[data-ck]")) {
    if (!live.has(e.dataset.ck)) live.set(e.dataset.ck, []);
    live.get(e.dataset.ck).push(e);
  }
  const used = new Map();
  for (const n of nodes) {
    for (const e of n.querySelectorAll ? [...n.querySelectorAll(".avatar.chara[data-ck]")] : []) {
      const k = e.dataset.ck, i = used.get(k) || 0;
      used.set(k, i + 1);
      const keep = live.get(k)?.[i];
      if (keep) e.replaceWith(keep);
    }
  }
}

// The blinks of every idle character come from one timer, which runs while there is one to blink.
const charBlinkers = new Set();
let charBlinkTimer = 0;
function charBlinkTick() {
  const now = performance.now();
  for (const r of charBlinkers) {
    const b = r.blink;
    if (b.until) {
      if (now >= b.until) { b.until = 0; b.set("calm"); }
    } else if (now >= b.next) {
      b.set("sleep");
      b.until = now + 140;
      b.next = now + b.period;
    }
  }
}

// charStart gives the character drawn in root (an element holding its .c-head, .c-facein, .c-all and .c-leaf)
// the expression and the motion of state: "work" (its plan: it looks around, its expressions change
// in its own order and pace), "need" (asking, its head slowly tilting), "idle" (calm, and a blink every
// 2.5 to 5.5 seconds unless opts.blink is false), "stop" (asleep, at half opacity). Reduced motion: still,
// with the state's expression. watch is the element whose being on the screen it follows (default root).
function charStart(root, key, state, opts = {}) {
  if (charReduced.matches || state === "stop" || (state === "idle" && opts.blink === false)) return null; // nothing to move
  const facein = root.querySelector(".c-facein"), head = root.querySelector(".c-head"), all = root.querySelector(".c-all"), leaf = root.querySelector(".c-leaf");
  if (!facein || !head) return null;
  const set = (k) => { facein.innerHTML = EXPR[k]; };
  const run = { root, anims: [], ticks: [], handles: [], on: false };
  const still = charReduced.matches;
  if (state === "work") {
    const p = plan(key);
    set(still ? "focus" : p.order[0]);
    if (!still) {
      head.style.transformOrigin = all.style.transformOrigin = "50% 100%";
      const opt = { duration: p.dur, iterations: Infinity, easing: "ease-in-out", delay: p.delay };
      const kf = (fn) => p.pts.map(([x, y, t]) => Object.assign({ offset: t }, fn(x, y)));
      run.anims.push(facein.animate(kf((x, y) => ({ transform: `translate(${x}px,${y}px)` })), opt));
      run.anims.push(head.animate(kf((x) => ({ transform: `rotate(${x / 4 * p.tilt}deg)` })), opt));
      run.anims.push(all.animate([{ transform: "translateY(0)" }, { transform: "translateY(-1.3px)" }], { duration: p.quirk, iterations: Infinity, direction: "alternate", easing: "ease-in-out" }));
      if (leaf) {
        leaf.style.transformOrigin = "50% 100%";
        run.anims.push(leaf.animate([{ transform: "rotate(-12deg)" }, { transform: "rotate(12deg)" }], { duration: p.quirk * 0.8, iterations: Infinity, direction: "alternate", easing: "ease-in-out" }));
      }
      let i = 0;
      run.ticks.push({ ms: p.beat, fn: () => { i = (i + 1) % p.order.length; set(p.order[i]); } });
    }
  } else if (state === "need") {
    set("ask");
    if (!still) {
      head.style.transformOrigin = "50% 100%";
      run.anims.push(head.animate([{ transform: "rotate(-4deg)" }, { transform: "rotate(4deg)" }], { duration: 900, iterations: Infinity, direction: "alternate", easing: "ease-in-out" }));
    }
  } else if (state === "idle") {
    set("calm");
    // a blink every 2.5 to 5.5 seconds, from the one timer all blinks share
    if (!still && opts.blink !== false) run.blink = { period: 2500 + (charHash(key) % 3000), next: 0, until: 0, set };
  } else {
    set("sleep");
  }
  if (!run.anims.length && !run.ticks.length && !run.blink) return run;
  run.resume = () => {
    if (run.on) return;
    run.on = true;
    run.anims.forEach((a) => a.play());
    run.handles = run.ticks.map((t) => setInterval(t.fn, t.ms));
    if (run.blink) {
      run.blink.next = performance.now() + run.blink.period;
      run.blink.until = 0;
      charBlinkers.add(run);
      if (!charBlinkTimer) charBlinkTimer = setInterval(charBlinkTick, 70);
    }
  };
  run.pause = () => {
    run.on = false;
    run.anims.forEach((a) => a.pause());
    run.handles.forEach(clearInterval);
    run.handles = [];
    if (run.blink) {
      if (run.blink.until) run.blink.set("calm");
      run.blink.until = 0;
      charBlinkers.delete(run);
      if (!charBlinkers.size && charBlinkTimer) { clearInterval(charBlinkTimer); charBlinkTimer = 0; }
    }
  };
  run.stop = () => {
    run.pause();
    run.anims.forEach((a) => a.cancel());
    run.anims = [];
    charLive.delete(run);
  };
  charLive.add(run);
  run.resume();
  const w = opts.watch || root;
  (w.__chars = w.__chars || []).push(run);
  charSeen?.observe(w);
  return run;
}

// What left the page is let go of: its animations cancelled, its timers cleared.
if (typeof document !== "undefined") setInterval(() => {
  for (const r of [...charLive]) {
    if (r.root.isConnected) continue;
    r.stop();
    for (const w of [r.root, r.root.closest?.(".avatar")]) if (w?.__chars) { w.__chars = w.__chars.filter((x) => x !== r); if (!w.__chars.length) charSeen?.unobserve(w); }
  }
}, 2000);

if (typeof module !== "undefined") module.exports = { SHAPES, COLORS, STRONG, EXPR, mix, charHash, charLook, rng, plan, charSVG, charExpr };
