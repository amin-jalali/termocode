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

// done and failed are not cards: nothing waits, so a tap just opens the page.
const END = {
  done: "The AI is done.",
  failed: "A run did not finish.",
};

// A session name is the owner's folder name: plain letters, digits,
// spaces and . _ - only, else it is ignored (never put into markup).
const NAME = /^[\p{L}\p{N} ._-]{1,40}$/u;

function title(data) {
  const s = data && data.s;
  return typeof s === "string" && NAME.test(s) ? "termocode: " + s : "termocode";
}

// waits says whether the push is about a card (not a done or failed).
function waits(data) {
  if (data && typeof data.k === "string" && Object.prototype.hasOwnProperty.call(END, data.k)) return false;
  return !!data && Number.isInteger(data.n) ? data.n > 0 : false;
}

function text(data) {
  const n = data && Number.isInteger(data.n) ? data.n : 0;
  if (data && typeof data.k === "string" && Object.prototype.hasOwnProperty.call(END, data.k)) return END[data.k];
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
    self.registration.showNotification(title(data), {
      body: text(data),
      data: { open: waits(data) ? "cards" : "" },
      tag: "termocode-push",
      renotify: true,
      icon: "/assets/icon.svg",
    }),
  );
});

self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  // A card push leads to the cards; an open page is told, a closed one
  // is opened with ?open=cards.
  const open = e.notification.data && e.notification.data.open === "cards" ? "cards" : "";
  e.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((list) => {
      for (const c of list) {
        if (new URL(c.url).origin === self.location.origin && "focus" in c) {
          if (open) c.postMessage({ open });
          return c.focus();
        }
      }
      return self.clients.openWindow(open ? "/?open=" + open : "/");
    }),
  );
});
