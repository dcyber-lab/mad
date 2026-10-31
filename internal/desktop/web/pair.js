// A pairing code in the URL's fragment (#pair=, from the QR code) is
// traded for this device's own token before the page starts: the rest of
// the scripts load once the trade is settled. The code stays in the URL
// until it worked or was refused for good (403): a network failure leaves
// it, with a Retry in the page, since the code works only once and the QR
// code may be gone.
"use strict";

window.madPairError = "";

// A block: its helpers stay out of the page's globals.
{

(async () => {
  const hash = new URLSearchParams(location.hash.slice(1));
  const code = hash.get("pair");
  if (code) {
    for (;;) {
      const r = await exchange(code);
      if (r.token) {
        try { localStorage.setItem("mad.token", r.token); } catch (_) { /* private window */ }
        forget(hash);
        break;
      }
      if (r.status === 403) {
        window.madPairError = "That pairing code was used or has expired: mad phone shows a new one.";
        forget(hash);
        break;
      }
      await retry(r.error);
    }
  }
  for (const src of ["app.js", "actions.js", "bus.js", "team.js", "skills.js", "board.js", "timers.js"]) {
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

// exchange asks the server for the token, for at most 10 seconds.
async function exchange(code) {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), 10000);
  try {
    const r = await fetch("/api/pair", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code, name: deviceName() }),
      signal: ctl.signal,
    });
    if (r.ok) return { status: r.status, token: (await r.json()).token };
    return { status: r.status, error: `the server answered ${r.status}` };
  } catch (e) {
    return { status: 0, error: ctl.signal.aborted ? "no answer in 10 seconds" : String(e.message || e) };
  } finally {
    clearTimeout(timer);
  }
}

// forget takes the code out of the fragment, keeping its other parts.
function forget(hash) {
  hash.delete("pair");
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
    btn.style.cssText = "font:inherit;padding:8px 20px";
    btn.onclick = () => { box.remove(); resolve(); };
    box.append(msg, btn);
    document.body.append(box);
  });
}
}
