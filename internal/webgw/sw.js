// The push-only service worker (MOB-30). It is the only code of the page
// that outlives it, so it does as little as can be: it shows one fixed
// text when the laptop pushes and opens the page on a tap. No fetch
// handler, no cache, no imports, no network: it never serves the page
// and keeps nothing of it. A push holds at most a count and a kind from
// a closed list; the words are this file's own.
"use strict";

// The words in English and Persian. The page registers the worker with
// ?l=en or ?l=fa (its own language); with no l the browser's language
// picks.
const WORDS = {
  en: {
    kind: {
      tool: "The AI waits for your approval.",
      mcp: "An outside agent waits for your approval.",
      plan: "A plan waits for your approval.",
      review: "A change waits for your review.",
      trust: "A project asks for your trust on the laptop.",
      workflow: "A workflow step waits for your approval.",
      ask: "The AI has a question for you.",
    },
    // done and failed are not cards: nothing waits, so a tap just opens the page.
    end: {
      done: "The AI is done.",
      failed: "A run did not finish.",
    },
    more: (k) => " " + k + " more wait.",
    one: "1 thing needs you.",
    many: (n) => n + " things need you.",
    some: "Something needs you.",
  },
  fa: {
    kind: {
      tool: "هوش مصنوعی منتظر تأیید شماست.",
      mcp: "یک عامل بیرونی منتظر تأیید شماست.",
      plan: "یک برنامه منتظر تأیید شماست.",
      review: "یک تغییر منتظر بازبینی شماست.",
      trust: "یک پروژه روی لپ‌تاپ اعتماد شما را می‌خواهد.",
      workflow: "یک گام گردش کار منتظر تأیید شماست.",
      ask: "هوش مصنوعی از شما سؤالی دارد.",
    },
    end: {
      done: "کار هوش مصنوعی تمام شد.",
      failed: "یک اجرا تمام نشد.",
    },
    more: (k) => " " + fa(k) + " مورد دیگر هم منتظر است.",
    one: "۱ چیز منتظر شماست.",
    many: (n) => fa(n) + " چیز منتظر شماست.",
    some: "چیزی منتظر شماست.",
  },
};

// fa writes a number in Persian digits.
function fa(n) {
  return String(n).replace(/[0-9]/g, (d) => "۰۱۲۳۴۵۶۷۸۹"[Number(d)]);
}

function words() {
  const l = new URLSearchParams(self.location.search || "").get("l");
  if (l === "fa" || l === "en") return WORDS[l];
  const nav = (self.navigator && self.navigator.language) || "";
  return /^fa\b/i.test(nav) ? WORDS.fa : WORDS.en;
}

function own(o, k) {
  return typeof k === "string" && Object.prototype.hasOwnProperty.call(o, k);
}

// A session name is the owner's folder name: plain letters, digits,
// spaces and . _ - only, else it is ignored (never put into markup).
const NAME = /^[\p{L}\p{N} ._-]{1,40}$/u;

function title(data) {
  const s = data && data.s;
  return typeof s === "string" && NAME.test(s) ? "termocode: " + s : "termocode";
}

// waits says whether the push is about a card (not a done or failed).
function waits(data) {
  if (data && own(WORDS.en.end, data.k)) return false;
  return !!data && Number.isInteger(data.n) ? data.n > 0 : false;
}

function text(data) {
  const w = words();
  const n = data && Number.isInteger(data.n) ? data.n : 0;
  if (data && own(w.end, data.k)) return w.end[data.k];
  if (data && own(w.kind, data.k)) {
    return n > 1 ? w.kind[data.k] + w.more(n - 1) : w.kind[data.k];
  }
  if (n === 1) return w.one;
  if (n > 1) return w.many(n);
  return w.some;
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
