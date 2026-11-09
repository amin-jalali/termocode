// This device, Pair a Device… and Devices… of the desktop page
// (device.js): the workbench loads this file only when one of them opens
// (lib/device.tsx), so neither the phone's page nor workbench.js carries
// it. It imports no value of the page: Preact, the sheet, the link, the
// fields and the toasts come from the host (one copy of each in the
// page); only types and its own strings are imported.
//
// This device: this browser's own settings (theme, language, direction,
// notifications), the link's facts and, for a controller, Pair a Device…
// and Devices…. Pair a Device…: the hub makes the one-time link
// (web.pair_open) and answers this page alone with it and its QR code;
// the dialog shows the code, the link with Copy link and how long it
// still works. When the phone asks (pair.ask), the same question as the
// laptop's card: the device's name and its code, Not now first and
// focused, then Pair as viewer and Pair with control (armed after a
// moment). The first answer counts, here or on the laptop (pair.end).
// Closing the dialog closes the link (and is Not now while the question
// waits). Devices…: the paired devices with their role and last seen;
// Remove asks first, with the laptop card's text (Cancel first).
import type { ComponentChildren, JSX as PJSX, VNode } from "preact";
import type { h as preactH, render as preactRender } from "preact";
import type { useEffect as UseEffect, useRef as UseRef, useState as UseState } from "preact/hooks";
import type { ReadonlySignal } from "@preact/signals";
import type { Meta, PairAsk, PairEnd, PairLink, WebDevice, WebDeviceList } from "../../../webproto/proto";
import type { Direction, Language, Prefs, Theme } from "./lib/prefs";
import type { StringKey } from "./strings";
import { ago, left, ps } from "./device/strings";

/** DeviceHost is what the page gives this file. */
export interface DeviceHost {
  h: typeof preactH;
  render: typeof preactRender;
  useState: typeof UseState;
  useEffect: typeof UseEffect;
  useRef: typeof UseRef;
  Sheet: (p: { onLater: () => void; children: ComponentChildren; label?: string }) => VNode | null;
  link: {
    meta: ReadonlySignal<Meta | null>;
    request(op: string, args?: unknown): Promise<unknown>;
    onEvent(fn: (kind: string, data: unknown) => void): () => void;
  };
  toast(severity: "info" | "ok" | "warn", title: string): void;
  /** ask asks before a request that needs a confirm (the page's AskLaptop). */
  ask(label: string, text: string, send: () => Promise<unknown>): void;
  t(key: StringKey, vars?: Record<string, string | number>): string;
  prefs: ReadonlySignal<Prefs>;
  setPref<K extends keyof Prefs>(k: K, v: Prefs[K]): void;
  SetPage: (p: { id: string; title: string; groups: { id: string; label: string; n: number }[]; children: ComponentChildren }) => VNode;
  NotifyField: () => VNode | null;
  BookmarkField: (p: { desk?: boolean }) => VNode | null;
}

// The host's Preact, for the JSX below (jsxFactory h) and the hooks.
let h: DeviceHost["h"];

// This file's h is the host's, not preact's, so the type checker finds
// its JSX types here (the other files find them on preact's h). Types
// only: nothing is emitted.
declare global {
  namespace JSX {
    type Element = PJSX.Element;
    type ElementClass = PJSX.ElementClass;
    type ElementAttributesProperty = PJSX.ElementAttributesProperty;
    type ElementChildrenAttribute = PJSX.ElementChildrenAttribute;
    type IntrinsicAttributes = PJSX.IntrinsicAttributes;
    type IntrinsicElements = PJSX.IntrinsicElements;
    type LibraryManagedAttributes<C, P> = PJSX.LibraryManagedAttributes<C, P>;
  }
}
let host: DeviceHost;

/** init takes the page's host; lib/device.tsx calls it once, first. */
export function init(hst: DeviceHost): void {
  host = hst;
  h = hst.h;
}

const ARM_MS = 600;

const errText = (e: unknown) => (e instanceof Error && e.message ? e.message : ps("wp.failed"));

let root: HTMLElement | null = null;

/** open shows Pair a Device… (web-pair) or Devices… (web-devices). */
export function open(what: string): void {
  if (!root) {
    // The desktop's colours are set on .layout-desktop. First in the
    // body, so the page's own sheets (Remove's question) show above it.
    root = document.createElement("div");
    root.className = "layout-desktop device-root";
    document.body.prepend(root);
  }
  const el = root;
  const close = () => host.render(null, el);
  // A fresh tree each time: a new link, a fresh list.
  host.render(null, el);
  host.render(what === "web-devices" ? <Devices onClose={close} onPair={() => open("web-pair")} /> : <Pair onClose={close} />, el);
}

/** DevicePage is the This device page: this browser's own settings and
 * the link's facts; a controller pairs and lists devices from here. */
export function DevicePage() {
  const t = host.t;
  const p = host.prefs.value;
  const m = host.link.meta.value;
  const themes = [
    { value: "system", label: t("more.theme.system") },
    { value: "dark", label: t("more.theme.dark") },
    { value: "light", label: t("more.theme.light") },
  ] as const;
  const langs = [
    { value: "auto", label: t("more.dir.auto") },
    { value: "en", label: t("more.lang.en") },
    { value: "fa", label: t("more.lang.fa") },
  ] as const;
  const dirs = [
    { value: "auto", label: t("more.dir.auto") },
    { value: "ltr", label: t("more.dir.ltr") },
    { value: "rtl", label: t("more.dir.rtl") },
  ] as const;
  const ctl = m?.role === "controller";
  const groups = [
    { id: "look", label: t("dev.look"), n: 3 },
    { id: "notify", label: t("notify.title"), n: 1 },
    { id: "link", label: t("dev.link"), n: 4 },
    ...(ctl ? [{ id: "devices", label: t("dev.devices"), n: 1 }] : []),
    { id: "about", label: t("more.about"), n: 1 },
  ];
  return (
    <host.SetPage id="more" title={t("act.device")} groups={groups}>
      <section class="set-group" data-group="look" aria-label={t("dev.look")}>
        <h2 class="set-group-h">{t("dev.look")}</h2>
        <Choice<Theme> name="theme" title={t("theme")} desc={t("dev.theme_desc")} value={p.theme} choices={themes} set={(v) => host.setPref("theme", v)} />
        <Choice<Language> name="lang" title={t("more.lang")} desc={t("dev.lang_desc")} value={p.lang} choices={langs} set={(v) => host.setPref("lang", v)} />
        <Choice<Direction> name="dir" title={t("more.dir")} desc={t("dev.dir_desc")} value={p.dir} choices={dirs} set={(v) => host.setPref("dir", v)} />
      </section>
      <section class="set-group" data-group="notify" aria-label={t("notify.title")}>
        <h2 class="set-group-h">{t("notify.title")}</h2>
        <div class="set-item set-notify">
          <host.NotifyField />
        </div>
      </section>
      <section class="set-group" data-group="link" aria-label={t("dev.link")}>
        <h2 class="set-group-h">{t("dev.link")}</h2>
        <Fact title={t("dev.name")} value={m?.device || "—"} />
        <Fact title={t("dev.role")} value={m ? t(m.role === "viewer" ? "dev.viewer" : "dev.controller") : "—"} />
        <Fact title={t("dev.viewers")} value={m ? String(m.viewers) : "—"} />
        <host.BookmarkField desk />
      </section>
      {ctl && (
        <section class="set-group" data-group="devices" aria-label={t("dev.devices")}>
          <h2 class="set-group-h">{t("dev.devices")}</h2>
          <div class="set-item set-pair">
            <button type="button" class="btn btn-primary btn-small" onClick={() => open("web-pair")}>
              {t("dev.pair")}
            </button>
            <button type="button" class="btn btn-neutral btn-small" onClick={() => open("web-devices")}>
              {t("dev.list")}
            </button>
          </div>
        </section>
      )}
      <section class="set-group" data-group="about" aria-label={t("more.about")}>
        <h2 class="set-group-h">{t("more.about")}</h2>
        <div class="set-item">
          <p class="set-desc">{t("more.about.body")}</p>
          <a class="set-link" href="/assets/licenses.txt">
            {t("more.licenses")}
          </a>
        </div>
      </section>
    </host.SetPage>
  );
}

function Choice<T extends string>({ name, title, desc, value, choices, set }: { name: string; title: string; desc: string; value: T; choices: readonly { value: T; label: string }[]; set: (v: T) => void }) {
  return (
    <div class="set-item" data-key={name}>
      <div class="set-title">
        <span class="set-name">{title}</span>
      </div>
      <p class="set-desc">{desc}</p>
      <div class="set-value">
        <select class="set-input set-select" aria-label={title} value={value} onChange={(e) => set((e.currentTarget as HTMLSelectElement).value as T)}>
          {choices.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

function Fact({ title, value }: { title: string; value: string }) {
  return (
    <div class="set-item">
      <div class="set-title">
        <span class="set-name">{title}</span>
      </div>
      <div class="set-value">
        <input class="set-input" value={value} disabled aria-label={title} />
      </div>
    </div>
  );
}

type PairStep =
  | { s: "opening" }
  | { s: "link"; l: PairLink; until: number }
  | { s: "ask"; a: PairAsk; at: number }
  | { s: "sending" }
  | { s: "end"; text: string; ok: boolean }
  | { s: "expired" };

function Pair({ onClose }: { onClose: () => void }) {
  const [step, setStep] = host.useState<PairStep>({ s: "opening" });
  const [, tick] = host.useState(0);
  const cur = host.useRef(step);
  cur.current = step;
  const safe = host.useRef<HTMLButtonElement | null>(null);

  const make = () => {
    setStep({ s: "opening" });
    host.link.request("web.pair_open").then(
      (r) => {
        const l = r as PairLink;
        setStep({ s: "link", l, until: Date.now() + l.ttl_s * 1000 });
      },
      (e: unknown) => setStep({ s: "end", text: errText(e), ok: false }),
    );
  };
  host.useEffect(() => {
    make();
    const off = host.link.onEvent((kind, data) => {
      const s = cur.current.s;
      if (kind === "pair.ask" && (s === "link" || s === "expired")) setStep({ s: "ask", a: data as PairAsk, at: Date.now() });
      if (kind === "pair.end" && s !== "end") {
        const d = data as PairEnd;
        const name = cur.current.s === "ask" ? (cur.current as { a: PairAsk }).a.name : "";
        const text = d.why === "closed" ? ps("wp.closed") : d.role ? ps("wp.laptop_paired", { name }) : ps("wp.laptop_refused");
        setStep({ s: "end", text, ok: !!d.role });
      }
    });
    const timer = setInterval(() => {
      const c = cur.current;
      if (c.s === "link" && Date.now() >= c.until) {
        setStep({ s: "expired" });
        host.link.request("web.pair_close").catch(() => undefined);
      } else tick((n) => n + 1);
    }, 1000);
    return () => {
      off();
      clearInterval(timer);
    };
  }, []);
  host.useEffect(() => {
    if (step.s !== "ask") return;
    safe.current?.focus();
    const armed = setTimeout(() => tick((n) => n + 1), ARM_MS); // the roles arm
    return () => clearTimeout(armed);
  }, [step.s]);

  const later = () => {
    const s = cur.current.s;
    if (s === "link" || s === "ask") host.link.request("web.pair_close").catch(() => undefined);
    onClose();
  };
  const answer = (role: string) => {
    const c = cur.current;
    if (c.s !== "ask" || (role && Date.now() - c.at < ARM_MS)) return;
    const name = c.a.name;
    setStep({ s: "sending" });
    host.link.request("web.pair_answer", role ? { role } : {}).then(
      () => setStep({ s: "end", text: role ? ps("wp.paired", { name }) : ps("wp.refused"), ok: !!role }),
      (e: unknown) => setStep({ s: "end", text: errText(e), ok: false }),
    );
  };
  const copy = (link: string) => {
    const done = () => host.toast("ok", ps("wp.copied"));
    const failed = () => host.toast("warn", ps("wp.copy_failed"));
    if (!navigator.clipboard) return failed();
    navigator.clipboard.writeText(link).then(done, failed);
  };

  let body: VNode;
  if (step.s === "ask") {
    const a = step.a;
    const armed = Date.now() - step.at >= ARM_MS;
    body = (
      <div class="ask pair-ask">
        <p class="ask-text">{ps("wp.ask_title")}</p>
        <p>{ps("wp.ask_body", { name: a.name })}</p>
        <p class="pair-code-label">{ps("wp.code_label")}</p>
        <p class="pair-code">{a.code.slice(0, 3) + " " + a.code.slice(3)}</p>
        <p class="ask-note">{ps("wp.code_hint")}</p>
        <p class="ask-note">{ps("wp.roles")}</p>
        {a.laptop ? (
          <p class="ask-note pair-laptop">{ps("wp.on_laptop")}</p>
        ) : (
          <div class="choices">
            <button type="button" class="btn btn-neutral" ref={safe} onClick={() => answer("")}>
              {ps("wp.not_now")}
            </button>
            <button type="button" class="btn btn-primary" aria-disabled={armed ? undefined : "true"} onClick={() => answer("viewer")}>
              {ps("wp.viewer")}
            </button>
            <button type="button" class="btn btn-neutral" aria-disabled={armed ? undefined : "true"} onClick={() => answer("control")}>
              {ps("wp.control")}
            </button>
          </div>
        )}
      </div>
    );
  } else if (step.s === "link") {
    const l = step.l;
    const q = 2; // the quiet zone, in modules
    body = (
      <div class="ask pair-link">
        <p class="ask-text">{ps("wp.title")}</p>
        <p class="ask-note">{ps("wp.scan")}</p>
        {l.size > 0 && (
          <svg class="pair-qr" viewBox={`${-q} ${-q} ${l.size + 2 * q} ${l.size + 2 * q}`} role="img" aria-label={ps("wp.qr")} shape-rendering="crispEdges">
            <rect x={-q} y={-q} width={l.size + 2 * q} height={l.size + 2 * q} fill="#fff" />
            <path d={l.qr} fill="#000" />
          </svg>
        )}
        <p class="pair-addr">{l.addr}</p>
        <div class="pair-row">
          <input class="set-input pair-url" value={l.link} readOnly aria-label={ps("wp.link")} onFocus={(e) => (e.currentTarget as HTMLInputElement).select()} />
          <button type="button" class="btn btn-neutral btn-small" onClick={() => copy(l.link)}>
            {ps("wp.copy")}
          </button>
        </div>
        <p class="ask-note pair-left" role="timer">{ps("wp.left", { time: left((step.until - Date.now()) / 1000) })}</p>
      </div>
    );
  } else if (step.s === "expired") {
    body = (
      <div class="ask">
        <p class="ask-text">{ps("wp.expired")}</p>
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={later}>
            {ps("wp.close")}
          </button>
          <button type="button" class="btn btn-primary" onClick={make}>
            {ps("wp.new")}
          </button>
        </div>
      </div>
    );
  } else if (step.s === "end") {
    body = (
      <div class="ask">
        <p class={step.ok ? "ask-text" : "ask-text pair-error"} role="status">
          {step.text}
        </p>
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={onClose}>
            {ps("wp.close")}
          </button>
        </div>
      </div>
    );
  } else {
    body = (
      <div class="ask">
        <p class="ask-note" role="status">
          {ps("wp.opening")}
        </p>
      </div>
    );
  }
  return (
    <host.Sheet onLater={later} label={ps("wp.title")}>
      {body}
    </host.Sheet>
  );
}

function Devices({ onClose, onPair }: { onClose: () => void; onPair: () => void }) {
  const [list, setList] = host.useState<WebDevice[] | null>(null);
  const [err, setErr] = host.useState("");
  const load = () =>
    host.link.request("web.devices").then(
      (r) => setList((r as WebDeviceList).items ?? []),
      (e: unknown) => setErr(errText(e)),
    );
  host.useEffect(() => {
    load();
    // A removal answered here or on the laptop: the list again.
    return host.link.onEvent((kind) => {
      if (kind === "answered") load();
    });
  }, []);
  const remove = (d: WebDevice) =>
    host.ask(ps("wp.dev_remove_label", { name: d.name }), ps("wp.dev_remove_body", { name: d.name }), () => host.link.request("web.device_remove", { id: d.id }));
  return (
    <host.Sheet onLater={onClose} label={ps("wp.dev_title")}>
      <div class="ask pair-devices">
        <p class="ask-text">{ps("wp.dev_title")}</p>
        {err ? (
          <p class="ask-note pair-error" role="status">
            {err}
          </p>
        ) : list === null ? (
          <p class="ask-note" role="status">
            {ps("wp.dev_loading")}
          </p>
        ) : list.length === 0 ? (
          <p class="ask-note">{ps("wp.dev_none")}</p>
        ) : (
          <ul class="pair-list">
            {list.map((d) => {
              const facts = [d.role === "control" ? ps("wp.dev_control") : ps("wp.dev_view")];
              if (d.guest) facts.push(ps("wp.dev_guest"));
              if (d.self) facts.push(ps("wp.dev_self"));
              facts.push(d.repair ? ps("wp.dev_repair") : d.live ? ps("wp.dev_live") : d.last_seen ? ps("wp.dev_seen", { time: ago(d.last_seen) }) : ps("wp.dev_never"));
              return (
                <li class="pair-dev" key={d.id} data-id={d.id}>
                  <span class="pair-dev-name">{d.name}</span>
                  <span class="pair-dev-facts">{facts.join(" · ")}</span>
                  <button type="button" class="btn btn-danger btn-small" onClick={() => remove(d)}>
                    {ps("wp.dev_remove")}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={onClose}>
            {ps("wp.close")}
          </button>
          <button type="button" class="btn btn-primary" onClick={onPair}>
            {ps("wp.dev_pair")}
          </button>
        </div>
      </div>
    </host.Sheet>
  );
}
