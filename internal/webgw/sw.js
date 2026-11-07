// The push-only service worker (MOB-30). It is the only code of the page
// that outlives it, so it does as little as can be: it shows one fixed
// text when the laptop pushes and opens the page on a tap. No fetch
// handler, no cache, no imports, no network: it never serves the page
// and keeps nothing of it. A push holds at most a count and a kind from
// a closed list; the words are this file's own.
"use strict";

const KIND = {
  tool: "The AI waits for your approval.",
  mcp: "An outside agent waits for your approval.",
  plan: "A plan waits for your approval.",
  review: "A change waits for your review.",
  trust: "A project asks for your trust on the laptop.",
  workflow: "A workflow step waits for your approval.",
};

function text(data) {
  const n = data && Number.isInteger(data.n) ? data.n : 0;
  if (data && typeof data.k === "string" && Object.prototype.hasOwnProperty.call(KIND, data.k)) {
    return n > 1 ? KIND[data.k] + " " + (n - 1) + " more wait." : KIND[data.k];
  }
  if (n === 1) return "1 thing needs you.";
  if (n > 1) return n + " things need you.";
  return "Something needs you.";
}

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));

self.addEventListener("push", (e) => {
  let data = null;
  try {
    data = e.data ? e.data.json() : null;
  } catch {
    data = null;
  }
  e.waitUntil(
    self.registration.showNotification("termocode", {
      body: text(data),
      tag: "termocode-push",
      renotify: true,
      icon: "/assets/icon.svg",
    }),
  );
});

self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  e.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((list) => {
      for (const c of list) {
        if (new URL(c.url).origin === self.location.origin && "focus" in c) return c.focus();
      }
      return self.clients.openWindow("/");
    }),
  );
});
