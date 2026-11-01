// A page left open over a deploy is old: the server names its build (the page's mad-build meta, and
// X-Mad-Build on /api/deck), and a poll that sees another build than the page's asks for a reload.
"use strict";

// staleAction says what to do about a build the server reported: nothing when either is unknown or
// they agree (a page served without a build, or an old server, never reloads); else reload at once,
// or, when the person is in the middle of something, ask them ("toast") and reload later.
function staleAction({ mine, seen, busy }) {
  if (!mine || !seen || mine === seen) return "none";
  return busy ? "toast" : "reload";
}

if (typeof module !== "undefined") module.exports = { staleAction };
