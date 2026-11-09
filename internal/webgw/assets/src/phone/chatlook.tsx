// The phone chat's look (owner, 2026-11-09): the desktop's approved one
// (aictx/look.tsx, runview.tsx, picker.tsx), phone-sized. home.js gives
// those parts the page's own Preact, hooks, link and sheets as their host
// (phone set), so they draw the same answer: no run card, a muted line
// "gpt-6-sol · 54s" that a tap opens, a short line per tool call, Thinking…
// with a Stop, a strip of a run's steps that opens a full-screen sheet,
// the rules file once at the top. A list the desktop opens as a menu (the
// profile and model picker) opens here as a sheet. app.js carries none of
// it: Chat.tsx loads home.js on a phone and asks lib/toolblocks.ts look.
import { Fragment, h, render } from "preact";
import { useEffect, useState } from "preact/hooks";
import { effect, signal } from "@preact/signals";
import type { AIHost } from "../aictx/api";
import { setHost } from "../aictx/jsx";
import { look as deskLook } from "../aictx/look";
import { AS } from "../aictx/strings";
import { aiSnap } from "../lib/ai";
import { layout } from "../lib/layout";
import { look } from "../lib/toolblocks";
import { link, store } from "../session";
import { addStrings, t } from "../strings";
import { AskLaptop } from "../ui/AskLaptop";
import type { MenuItems } from "../ui/ContextMenu";
import { Cod } from "../ui/icons";
import { Md } from "../ui/Markdown";
import { Sheet } from "../ui/Sheet";
import { pinned } from "../views/Chat";

/** menu is the list the picker opened (null: none). */
const menu = signal<{ label: string; items: MenuItems } | null>(null);

/** MenuSheet is a picker's list as a phone sheet: one 44 px row each,
 * the one in use checked; a row that cannot be picked is dimmed. */
function MenuSheet() {
  const m = menu.value;
  if (!m) return null;
  const close = () => (menu.value = null);
  return (
    <Sheet onLater={close} label={m.label}>
      <div class="actsheet ai-menu" dir="ltr">
        <h2 class="actsheet-title">{m.label}</h2>
        <div class="actsheet-list">
          {m.items.map(
            (it) =>
              it && (
                <button
                  type="button"
                  class="actsheet-row"
                  disabled={it.disabled}
                  onClick={() => {
                    close();
                    it.run();
                  }}
                >
                  <span class="ai-menu-label">{it.label}</span>
                  {it.keys && <span class="ai-menu-keys">{it.keys}</span>}
                </button>
              ),
          )}
        </div>
      </div>
    </Sheet>
  );
}

/** Pick is the composer's profile and model picker on a phone: it reads
 * the same ai topic as the desktop's. It waits behind a ✦ button at the
 * start of the composer's row, so the box gets the row; a tap opens the
 * profile and model buttons in a row above the box (closed, they stay
 * mounted: a pick's ask to the laptop lives in them). */
function Pick() {
  useEffect(() => link.subscribe("ai"), []);
  const [open, setOpen] = useState(false);
  const s = aiSnap();
  if (!s?.configured) return null;
  const what = `${t("ai.profile")}: ${[s.profile, s.model].filter(Boolean).join(" · ")}`;
  return (
    <>
      <button type="button" class="pick-toggle" aria-expanded={open} aria-label={what} title={what} onClick={() => setOpen(!open)}>
        <span aria-hidden="true">✦</span>
      </button>
      <span class={`pick-list${open ? " is-open" : ""}`}>{deskLook.pick()}</span>
      <MenuSheet />
    </>
  );
}

const host = {
  h,
  Fragment: Fragment as unknown as AIHost["Fragment"],
  render,
  link,
  t,
  addStrings,
  effect,
  signal,
  useState,
  useEffect,
  store,
  pinned,
  Md,
  Cod,
  Sheet,
  AskLaptop,
  aiSnap,
  openMenu: (_x: number, _y: number, label: string, items: MenuItems) => (menu.value = { label, items }),
  phone: true,
} as Partial<AIHost> as AIHost;

/** phoneLook puts the look in the chat (not on a desktop, whose
 * aictx.js has its own). */
export function phoneLook(): void {
  if (layout.peek() === "desktop" || look.peek()) return;
  setHost(host);
  addStrings(AS);
  look.value = { ...deskLook, pick: () => <Pick /> };
}
