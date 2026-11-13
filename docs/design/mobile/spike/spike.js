// MOB-1 device spike: what the phone's browser engine can do for the
// termocode panel. Plain browser JS, no build, no network calls, no
// server. It runs two ways:
//   1. in index.html (an iPhone Home-Screen web app, a phone browser);
//   2. pasted into the DevTools console of the Android app's WebView
//      (debug build, chrome://inspect), on the paired laptop's origin,
//      so the passkey is made for that same origin.
// Nothing leaves the phone. The passkey it makes is a test key for this
// origin's host name; delete it in the phone's password settings after.
(function () {
  "use strict";
  var KEY = "tc-spike";
  var DB = "tc-spike";

  function now() { return Date.now(); }
  function ago(ms) {
    var s = Math.round((now() - ms) / 1000);
    if (s < 120) return s + " s ago";
    if (s < 7200) return Math.round(s / 60) + " min ago";
    if (s < 172800) return Math.round(s / 3600) + " h ago";
    return Math.round(s / 86400) + " days ago";
  }
  function errName(e) { return e && e.name ? e.name + (e.message ? ": " + e.message : "") : String(e); }
  function saved() {
    try { return JSON.parse(localStorage.getItem(KEY) || "{}"); } catch (e) { return {}; }
  }
  function save(o) {
    try { localStorage.setItem(KEY, JSON.stringify(o)); } catch (e) { /* reported by the storage check */ }
  }

  // within ends a check that never answers (some engines leave a promise
  // open forever) after ms, with what late gives.
  function within(p, ms, late) {
    return Promise.race([p, new Promise(function (res) { setTimeout(function () { res(late()); }, ms); })]);
  }

  // ok: true (pass), false (fail), null (info only).
  function row(id, what, ok, detail) { return { id: id, what: what, ok: ok, detail: detail || "" }; }

  function env() {
    var ua = navigator.userAgent;
    var standalone = (window.matchMedia && matchMedia("(display-mode: standalone)").matches) || navigator.standalone === true;
    var webview = /; wv\)/.test(ua) || (/(iPhone|iPad)/.test(ua) && !/Safari\//.test(ua));
    return [
      row("env.ua", "Browser engine", null, ua),
      row("env.secure", "Secure context (https)", window.isSecureContext === true, location.origin),
      row("env.standalone", "Runs as an installed app (Home Screen)", null, standalone ? "yes" : "no"),
      row("env.webview", "Looks like a WebView", null, webview ? "yes" : "no"),
    ];
  }

  function webauthn() {
    var out = [];
    var pkc = window.PublicKeyCredential;
    out.push(row("wa.api", "WebAuthn API is there", !!pkc, pkc ? "" : "no PublicKeyCredential"));
    if (!pkc) return Promise.resolve(out);
    var checks = [];
    if (pkc.isUserVerifyingPlatformAuthenticatorAvailable) {
      checks.push(pkc.isUserVerifyingPlatformAuthenticatorAvailable().then(
        function (v) { out.push(row("wa.platform", "Phone unlock can sign (platform authenticator)", v === true, String(v))); },
        function (e) { out.push(row("wa.platform", "Phone unlock can sign (platform authenticator)", false, errName(e))); }));
    }
    if (pkc.isConditionalMediationAvailable) {
      checks.push(pkc.isConditionalMediationAvailable().then(
        function (v) { out.push(row("wa.autofill", "Passkey autofill", null, String(v))); },
        function () { out.push(row("wa.autofill", "Passkey autofill", null, "error")); }));
    }
    if (pkc.getClientCapabilities) {
      checks.push(pkc.getClientCapabilities().then(
        function (c) { out.push(row("wa.caps", "Client capabilities", null, JSON.stringify(c))); },
        function () { /* optional */ }));
    }
    return Promise.all(checks).then(function () { return out; });
  }

  function rnd(n) { var b = new Uint8Array(n); crypto.getRandomValues(b); return b; }
  function b64(buf) {
    var s = "", b = new Uint8Array(buf);
    for (var i = 0; i < b.length; i++) s += String.fromCharCode(b[i]);
    return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  // passkeyMake makes a discoverable passkey for this host (needs a tap).
  function passkeyMake() {
    var opts = {
      publicKey: {
        rp: { id: location.hostname, name: "termocode spike" },
        user: { id: rnd(16), name: "spike@" + location.hostname, displayName: "termocode spike (delete me)" },
        challenge: rnd(32),
        pubKeyCredParams: [{ type: "public-key", alg: -7 }, { type: "public-key", alg: -257 }],
        authenticatorSelection: { residentKey: "required", userVerification: "required" },
        timeout: 120000,
      },
    };
    return navigator.credentials.create(opts).then(function (c) {
      var o = saved();
      o.passkey = { id: b64(c.rawId), at: now() };
      save(o);
      return row("pk.make", "Make a passkey", true, "id " + o.passkey.id.slice(0, 12) + "…");
    }, function (e) { return row("pk.make", "Make a passkey", false, errName(e)); });
  }

  // passkeyUse signs in with any passkey of this host (the account picker).
  function passkeyUse() {
    var opts = { publicKey: { challenge: rnd(32), rpId: location.hostname, userVerification: "required", timeout: 120000 } };
    return navigator.credentials.get(opts).then(function (c) {
      var o = saved(), same = o.passkey && o.passkey.id === b64(c.rawId);
      return row("pk.use", "Sign in with the passkey", true, same ? "the one made " + ago(o.passkey.at) : "another passkey of this host");
    }, function (e) { return row("pk.use", "Sign in with the passkey", false, errName(e)); });
  }

  // The panel's bookmark cookie is httpOnly, so a page cannot see it; a
  // cookie with the same other flags (Secure, SameSite=Strict, 400 days)
  // shows whether the engine keeps one across a restart.
  function cookie() {
    var m = document.cookie.match(/(?:^|; )tc_spike=(\d+)/);
    var before = m ? Number(m[1]) : 0;
    var at = before || now();
    document.cookie = "tc_spike=" + at + "; Max-Age=34560000; Path=/; Secure; SameSite=Strict";
    var set = /(?:^|; )tc_spike=/.test(document.cookie);
    return [
      row("ck.set", "Set a Secure SameSite=Strict cookie", set, ""),
      row("ck.kept", "Cookie kept from an earlier visit", before ? true : null, before ? "set " + ago(before) : "first visit: close the app fully, open it again"),
    ];
  }

  function idb() {
    return new Promise(function (res, rej) {
      if (!window.indexedDB) return rej(new Error("no IndexedDB"));
      var r = indexedDB.open(DB, 1);
      r.onupgradeneeded = function () { r.result.createObjectStore("k"); };
      r.onsuccess = function () { res(r.result); };
      r.onerror = function () { rej(r.error); };
    });
  }
  function idbGet(db, k) {
    return new Promise(function (res, rej) {
      var q = db.transaction("k").objectStore("k").get(k);
      q.onsuccess = function () { res(q.result); };
      q.onerror = function () { rej(q.error); };
    });
  }
  function idbPut(db, k, v) {
    return new Promise(function (res, rej) {
      var t = db.transaction("k", "readwrite");
      t.objectStore("k").put(v, k);
      t.oncomplete = function () { res(); };
      t.onerror = function () { rej(t.error); };
    });
  }

  function storage() {
    var out = [];
    var o = saved();
    out.push(row("st.local", "localStorage kept from an earlier visit", o.first ? true : null, o.first ? "first visit " + ago(o.first) + ", " + (o.visits || 0) + " visits" : "first visit"));
    o.first = o.first || now();
    o.visits = (o.visits || 0) + 1;
    save(o);
    var p = Promise.resolve();
    if (navigator.storage && navigator.storage.persisted) {
      p = navigator.storage.persisted().then(function (v) { out.push(row("st.persist", "Storage marked persistent", null, String(v))); });
    }
    return p.then(function () { return out; }, function () { return out; });
  }

  // crypto: what the relay (ADR 0022) needs: X25519, AES-GCM, SHA-256,
  // and a non-extractable key kept in IndexedDB across restarts.
  function cryptoChecks() {
    var out = [];
    var s = window.crypto && crypto.subtle;
    if (!s) return Promise.resolve([row("cr.subtle", "WebCrypto", false, "no crypto.subtle")]);
    var x = s.generateKey({ name: "X25519" }, false, ["deriveBits"]).then(
      function (k) { out.push(row("cr.x25519", "X25519 keys (relay)", true, "")); return k; },
      function (e) { out.push(row("cr.x25519", "X25519 keys (relay)", false, errName(e))); return null; });
    var aes = s.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt"]).then(
      function () { out.push(row("cr.aes", "AES-256-GCM", true, "")); },
      function (e) { out.push(row("cr.aes", "AES-256-GCM", false, errName(e))); });
    var ec = s.generateKey({ name: "ECDSA", namedCurve: "P-256" }, false, ["sign"]).then(
      function () { out.push(row("cr.p256", "ECDSA P-256", true, "")); },
      function (e) { out.push(row("cr.p256", "ECDSA P-256", false, errName(e))); });
    return Promise.all([x, aes, ec]).then(function (r) {
      var key = r[0];
      return idb().then(function (db) {
        return idbGet(db, "relay").then(function (old) {
          if (old && old.key) {
            out.push(row("cr.kept", "Non-extractable key kept in IndexedDB", old.key.extractable === false, "made " + ago(old.at)));
            return null;
          }
          out.push(row("cr.kept", "Non-extractable key kept in IndexedDB", null, "first visit: close the app fully, open it again"));
          return key ? idbPut(db, "relay", { key: key.privateKey, at: now() }) : null;
        });
      }).then(function () { return out; }, function (e) {
        out.push(row("cr.kept", "Non-extractable key kept in IndexedDB", false, errName(e)));
        return out;
      });
    });
  }

  function pushChecks() {
    return [
      row("pu.sw", "Service worker", "serviceWorker" in navigator, ""),
      row("pu.push", "Web Push (PushManager)", "PushManager" in window, ""),
      row("pu.notify", "Notifications", null, window.Notification ? Notification.permission : "none"),
      row("pu.badge", "App badge", null, "setAppBadge" in navigator ? "yes" : "no"),
      row("cb.write", "Clipboard write", null, navigator.clipboard && navigator.clipboard.writeText ? "yes" : "no"),
    ];
  }

  function all() {
    var rows = env().concat(cookie(), pushChecks());
    var late = function (id, what) { return function () { return [row(id, what, false, "no answer in 4 s")]; }; };
    return Promise.all([
      within(webauthn(), 4000, late("wa.late", "WebAuthn checks")),
      within(storage(), 4000, late("st.late", "Storage checks")),
      within(cryptoChecks(), 4000, late("cr.late", "Crypto checks")),
    ]).then(function (r) {
      return rows.concat(r[0], r[1], r[2]);
    });
  }

  function mark(ok) { return ok === true ? "pass" : ok === false ? "FAIL" : "info"; }

  // markdown is the result table, to paste into the plan's MOB-1 table.
  function markdown(rows, device) {
    var lines = ["Device: " + (device || "?") + " · " + new Date().toISOString().slice(0, 16), "", "| Check | Result | Detail |", "|---|---|---|"];
    rows.forEach(function (r) { lines.push("| " + r.what + " | " + mark(r.ok) + " | " + String(r.detail).replace(/\|/g, "/") + " |"); });
    return lines.join("\n");
  }

  // buttons puts two buttons over any page (the WebView console case): a
  // passkey needs a real tap, which a console call is not.
  function buttons() {
    var box = document.createElement("div");
    box.style.cssText = "position:fixed;left:16px;right:16px;bottom:16px;z-index:2147483647;display:flex;gap:8px";
    [["Make a passkey", passkeyMake], ["Sign in with it", passkeyUse]].forEach(function (b) {
      var el = document.createElement("button");
      el.textContent = b[0];
      el.style.cssText = "flex:1;min-height:48px;font:16px system-ui;border-radius:10px;border:0;background:#0b6bcb;color:#fff";
      el.onclick = function () { b[1]().then(function (r) { console.log(mark(r.ok), r.what, r.detail); el.textContent = mark(r.ok) + " · " + b[0]; }); };
      box.appendChild(el);
    });
    document.body.appendChild(box);
  }

  var api = { all: all, buttons: buttons, passkeyMake: passkeyMake, passkeyUse: passkeyUse, markdown: markdown, mark: mark };
  window.tcSpike = api;
  // In a console with no page around it: run and print.
  if (!document.getElementById("spike-out")) {
    all().then(function (rows) { console.table(rows); console.log(markdown(rows, "")); console.log("Passkey: run tcSpike.buttons(), then tap the buttons on the phone."); });
  }
})();
