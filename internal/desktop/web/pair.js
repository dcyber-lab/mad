// A pairing code in the URL's fragment (#pair=, from the QR code) pairs
// this device before the page starts: the rest of the scripts load once
// that is settled. The device makes its own token, once, and sends it with
// the code; a retry sends the same pair, so an answer lost on the way
// costs nothing (the server knows the pair it already took). The code
// stays in the URL until it worked or was refused for good (403): a
// network failure leaves it, with a Retry in the page, since the code works
// only once and the QR code may be gone. On success the fragment holds
// #token= for app.js, which keeps it, so the page is signed in even where
// storage throws.
"use strict";

window.madPairError = "";

// A block: its helpers stay out of the page's globals.
{

let made = ""; // see ownToken

(async () => {
  const hash = new URLSearchParams(location.hash.slice(1));
  const code = hash.get("pair");
  if (code) {
    const token = ownToken();
    for (;;) {
      const r = await exchange(code, token);
      if (r.ok) {
        try { sessionStorage.removeItem("mad.pairtoken"); } catch (_) { /* none */ }
        hash.delete("pair");
        hash.set("token", token);
        replace(hash);
        break;
      }
      if (r.status === 403) {
        window.madPairError = "That pairing code was used or has expired: mad phone shows a new one.";
        try { sessionStorage.removeItem("mad.pairtoken"); } catch (_) { /* none */ }
        hash.delete("pair");
        replace(hash);
        break;
      }
      await retry(r.error);
    }
  }
  for (const src of ["app.js", "actions.js", "bus.js", "team.js", "skills.js", "board.js", "usage.js", "timers.js"]) {
    const s = document.createElement("script");
    s.src = src;
    s.async = false; // in this order
    document.body.append(s);
  }
})();

// deviceName is a readable name for this browser, from its user agent.
function deviceName() {
  const ua = navigator.userAgent;
  const os = /iPhone/.test(ua) ? "iPhone" : /iPad/.test(ua) ? "iPad" : /Android/.test(ua) ? "Android" : /Mac OS X/.test(ua) ? "Mac" : /Windows/.test(ua) ? "Windows" : /Linux/.test(ua) ? "Linux" : "device";
  const br = /Edg\//.test(ua) ? "Edge" : /(Chrome|CriOS)\//.test(ua) ? "Chrome" : /(Firefox|FxiOS)\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : "browser";
  return `${os} ${br}`;
}

// ownToken is this device's token: 16 random bytes as hex, made once per
// page load and kept in sessionStorage too, so a reload mid-retry sends
// the same one.
function ownToken() {
  if (made) return made;
  try { made = sessionStorage.getItem("mad.pairtoken") || ""; } catch (_) { /* none */ }
  if (!/^[0-9a-f]{32}$/.test(made)) {
    const b = crypto.getRandomValues(new Uint8Array(16));
    made = Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  }
  try { sessionStorage.setItem("mad.pairtoken", made); } catch (_) { /* none */ }
  return made;
}

// exchange pairs with the server, for at most 10 seconds.
async function exchange(code, token) {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), 10000);
  try {
    const r = await fetch("/api/pair", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code, name: deviceName(), token }),
      signal: ctl.signal,
    });
    if (r.ok) return { status: r.status, ok: true };
    return { status: r.status, error: `the server answered ${r.status}` };
  } catch (e) {
    return { status: 0, error: ctl.signal.aborted ? "no answer in 10 seconds" : String(e.message || e) };
  } finally {
    clearTimeout(timer);
  }
}

// replace puts hash in the URL's fragment, its other parts kept.
function replace(hash) {
  const rest = hash.toString();
  history.replaceState(null, "", location.pathname + location.search + (rest ? `#${rest}` : ""));
}

// retry says pairing failed and waits for the person to try again.
function retry(why) {
  return new Promise((resolve) => {
    const box = document.createElement("div");
    box.id = "pairretry";
    box.style.cssText = "position:fixed;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px;background:#000c;color:#fff;font:16px system-ui;z-index:9999;padding:24px;text-align:center";
    const msg = document.createElement("p");
    msg.textContent = `Could not pair with mad (${why}).`;
    const btn = document.createElement("button");
    btn.textContent = "Retry";
    btn.style.cssText = "font:inherit;font-weight:600;padding:10px 28px;border:0;border-radius:999px;background:#fff;color:#000";
    btn.onclick = () => { box.remove(); resolve(); };
    box.append(msg, btn);
    document.body.append(box);
  });
}
}
