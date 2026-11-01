#!/usr/bin/env node
// Prints the characters' motion plans as JSON: the expected values of
// MadUITests/CharacterPlanTests. The plan code below is copied from the approved
// reference (characters-final.html: hash, rng, plan); the samples evaluate it the way the
// Web Animations API does (keyframes by offset under an ease-in-out timing function).
// Until the web page carries the same code, this is the reference. Run:
//   node ios/MadUI/scripts/dump-character-vectors.js > ios/MadUI/Tests/MadUITests/Fixtures/character-vectors.json
function hash(s) { let h = 2166136261; for (const c of s) h = Math.imul(h ^ c.codePointAt(0), 16777619) >>> 0; return h; }
function rng(seed) { let s = seed || 1; return () => { s ^= s << 13; s >>>= 0; s ^= s >> 17; s ^= s << 5; s >>>= 0; return (s % 100000) / 100000; }; }
function plan(id) {
  const r = rng(hash(id)), style = ["wander","circle","scan","peek"][Math.floor(r()*4)];
  const dur = 2800 + r()*3600, reach = 2.2 + r()*1.6, tilt = 3 + r()*6, pts = [];
  if (style === "circle") { const dir = r()<.5?1:-1, st = r()*Math.PI*2; for (let i=0;i<=10;i++){ const a=st+dir*i/10*Math.PI*2; pts.push([Math.cos(a)*reach, Math.sin(a)*reach*.8, i/10]); } }
  else if (style === "scan") { const c=[[-1,-1],[1,-1],[1,1],[-1,1]], o=Math.floor(r()*4); pts.push([0,0,0]); for (let i=0;i<4;i++){ const [x,y]=c[(i+o)%4], t=.1+i*.2; pts.push([x*reach*.85,y*reach*.7,t],[x*reach*.85,y*reach*.7,t+.12]); } pts.push([0,0,1]); }
  else if (style === "peek") { const a=r()*Math.PI*2, b=a+Math.PI*(.6+r()*.8); pts.push([0,0,0],[0,0,.32],[Math.cos(a)*reach,Math.sin(a)*reach*.8,.38],[Math.cos(a)*reach,Math.sin(a)*reach*.8,.5],[0,0,.58],[0,0,.78],[Math.cos(b)*reach*.8,Math.sin(b)*reach*.7,.83],[0,0,.92],[0,0,1]); }
  else { pts.push([0,0,0]); let t=0; const n=4+Math.floor(r()*3); for (let i=0;i<n;i++){ const a=r()*Math.PI*2, d=reach*(.5+r()*.5); t+=.7/n; const x=Math.cos(a)*d, y=Math.sin(a)*d*.8; pts.push([x,y,t],[x,y,Math.min(.95,t+.25/n)]); t+=.25/n; } pts.push([0,0,1]); }
  const order = ["focus","think","happy","wow","wink"].sort(() => r() - .5);
  return { style, dur, tilt, pts, order, beat: 1500 + r()*2200, delay: r()*1200, quirk: 900 + r()*1200 };
}

// cubic-bezier(.42, 0, .58, 1), the timing function "ease-in-out"
function ease(x) {
  const bez = (a, b, t) => 3 * a * t * (1 - t) * (1 - t) + 3 * b * t * t * (1 - t) + t * t * t;
  let lo = 0, hi = 1;
  for (let i = 0; i < 60; i++) { const m = (lo + hi) / 2; if (bez(.42, .58, m) < x) lo = m; else hi = m; }
  return bez(0, 1, (lo + hi) / 2);
}
// keyframes by offset, linear between them
function along(pts, f) {
  if (f <= pts[0][2]) return [pts[0][0], pts[0][1]];
  for (let i = 1; i < pts.length; i++) {
    if (f <= pts[i][2]) {
      const a = pts[i-1], b = pts[i], span = b[2] - a[2];
      if (span <= 0) return [b[0], b[1]];
      const u = (f - a[2]) / span;
      return [a[0] + (b[0]-a[0])*u, a[1] + (b[1]-a[1])*u];
    }
  }
  const l = pts[pts.length-1]; return [l[0], l[1]];
}
// an alternating animation: forward then back, eased
function alternate(ms, dur) { const p = (ms / dur) % 2; return ease(p <= 1 ? p : 2 - p); }

// what a working character shows `ms` after it began: face offset, head turn (deg), bob (units), leaf turn (deg), expression
function working(p, ms) {
  const local = ms - p.delay;
  let x = 0, y = 0, tilt = 0;
  if (local >= 0) {
    const f = ease((local % p.dur) / p.dur);
    [x, y] = along(p.pts, f);
    tilt = x / 4 * p.tilt;
  }
  return { x, y, tilt, bob: -1.3 * alternate(ms, p.quirk), leaf: -12 + 24 * alternate(ms, p.quirk * .8),
    expr: p.order[Math.floor(ms / p.beat) % p.order.length] };
}
const needs = (ms) => ({ tilt: -4 + 8 * alternate(ms, 900) });
const idle = (id, ms) => { const every = 2500 + (hash(id) % 3000); return { every, blink: ms >= every && ms % every < 140 }; };

const ids = process.argv.slice(2);
const sample = ids.length ? ids : ["architect", "builder", "reviewer", "claude-cc23", "claude-3fe9", "codex 32d23a28",
  "a", "", "?", "你好", "😀 mad", ...Array.from({ length: 16 }, (_, i) => "bot-" + (i*7919+13).toString(36) + "-" + i)];
const times = [0, 250, 900, 1337, 2500, 4001, 6789, 12345];
console.log(JSON.stringify(sample.map(id => {
  const p = plan(id);
  return { id, hash: hash(id), plan: p, working: times.map(ms => ({ ms, ...working(p, ms) })),
    needs: times.map(ms => ({ ms, ...needs(ms) })), idle: [{ ms: 0, ...idle(id, 0) }, { ms: idle(id, 0).every + 50, ...idle(id, idle(id, 0).every + 50) },
      { ms: idle(id, 0).every + 200, ...idle(id, idle(id, 0).every + 200) }] };
}), null, 1));
