// mad's service worker: shows what the deck's notifications say, pushed
// to this browser (package push), and opens the agent one is about.
"use strict";

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));

self.addEventListener("push", (e) => {
  let m = {};
  try {
    m = e.data ? e.data.json() : {};
  } catch (_) {
    m = { body: e.data ? e.data.text() : "" };
  }
  e.waitUntil(self.registration.showNotification(m.title || "mad", {
    body: m.body || "",
    tag: m.tag || undefined,
    renotify: !!m.tag,
    icon: "icon-192.png",
    badge: "icon-192.png",
    data: { agent: m.agent || "" },
  }));
});

// A click opens mad on the agent: the page open, else a new one.
self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  const agent = (e.notification.data && e.notification.data.agent) || "";
  e.waitUntil((async () => {
    const open = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    for (const c of open) {
      if ("focus" in c) {
        await c.focus();
        if (agent) c.postMessage({ open: agent });
        return;
      }
    }
    await self.clients.openWindow(agent ? `./?agent=${encodeURIComponent(agent)}` : "./");
  })());
});
