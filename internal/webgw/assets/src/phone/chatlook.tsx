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
import { goTab } from "../lib/router";
import { look } from "../lib/toolblocks";
import { link, store } from "../session";
import { addStrings, t } from "../strings";
import { AskLaptop } from "../ui/AskLaptop";
import type { MenuItems } from "../ui/ContextMenu";
import { Cod, IconPlus } from "../ui/icons";
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

/** Pick is the phone composer's + (the common chat composer's): a round
 * button at the start of the pill that opens a sheet to add a file and
 * to choose the AI profile and model (the same ai topic as the
 * desktop's), the one in use named once, muted, in its header. The sheet
 * stays open under a picker's list: a pick's ask to the laptop lives in
 * its buttons. */
function Pick() {
  useEffect(() => link.subscribe("ai"), []);
  const [open, setOpen] = useState(false);
  const s = aiSnap();
  const close = () => setOpen(false);
  return (
    <>
      <button type="button" class="composer-plus" aria-haspopup="dialog" aria-expanded={open} aria-label={t("plus.label")} title={t("plus.label")} onClick={() => setOpen(true)}>
        <IconPlus />
      </button>
      {open && (
        <Sheet onLater={close} label={t("plus.title")}>
          <div class="actsheet plus-sheet">
            <h2 class="actsheet-title">{t("plus.title")}</h2>
            {s?.configured && (
              <p class="actsheet-hint plus-now" dir="auto">
                {[s.profile, s.model].filter(Boolean).join(" · ")}
              </p>
            )}
            <div class="actsheet-list">
              <button
                type="button"
                class="actsheet-row plus-file"
                onClick={() => {
                  close();
                  goTab("files");
                }}
              >
                {t("plus.file")}
              </button>
              {s?.configured && (
                <>
                  <p class="actsheet-hint plus-label">{t("ai.model")}</p>
                  <span class="plus-picks">{deskLook.pick()}</span>
                </>
              )}
            </div>
          </div>
        </Sheet>
      )}
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
