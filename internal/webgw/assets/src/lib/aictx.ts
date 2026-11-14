// The workbench's side of the AI chat's extras (aictx.js, loaded on
// first use; contract in aictx/api.ts): a row's right-click menu gets Add
// to AI Chat after its own items, and a table grid's right click opens
// the item under it, and a row can be dragged to the chat. aictx.js
// loads when the user asks, starts a drag, or once the chat input takes
// the focus (there it watches for @ and for a send).
import { h, render } from "preact";
import { effect, signal } from "@preact/signals";
import { useEffect, useState } from "preact/hooks";
import type { AIHost, AIItem, GridAt } from "../aictx/api";
import type * as AICtx from "../aictx/main";
import { link, store } from "../session";
import { hostCtrs, hosts, loadHosts } from "../state/hosts";
import { addStrings, t } from "../strings";
import { menu, openMenu } from "../ui/ContextMenu";
import { Sheet } from "../ui/Sheet";
import { AskLaptop } from "../ui/AskLaptop";
import { Cod } from "../ui/icons";
import { Md } from "../ui/Markdown";
import { aiSnap } from "./ai";
import { asked, later } from "./asks";
import { look } from "./toolblocks";
import { pinned } from "../views/Chat";
import { ctrList } from "../views/desk/DeskContainers";
import { conns, loadConns, tables } from "../views/desk/dbstate";
import { addAttach, attaches } from "./attach";
import { getDraft, setDraft } from "./drafts";
import { toast } from "./toasts";
import { quick, setPanes } from "./workbench";

type Mod = typeof AICtx;

const host = (): AIHost => ({
  h,
  render,
  link,
  t,
  addStrings,
  toast,
  openMenu,
  effect,
  quick,
  Sheet,
  store,
  pinned,
  attaches,
  addAttach,
  getDraft,
  setDraft,
  showChat: () => setPanes({ chat: true }),
  conns,
  tables,
  loadConns,
  loadHosts,
  ctrs: ctrList,
  hosts,
  hostCtrs,
  useState,
  useEffect,
  signal,
  Md,
  Cod,
  AskLaptop,
  aiSnap,
  asked,
  later,
});

let loading: Promise<Mod> | null = null;

/** withAI runs f with aictx.js, loading it once (a failed load can be
 * tried again). */
export function withAI<R>(f: (m: Mod) => R): Promise<R> {
  if (!loading)
    loading = import("/assets/aictx.js").then(
      (m: Mod) => (m.init(host()), m),
      (e: unknown) => {
        loading = null;
        throw e;
      },
    );
  // A failed load says so; the answer of f goes to a caller that waits
  // for it (one that does not is no unhandled rejection).
  loading.catch(() => toast("warn", t("cmd.failed")));
  const p = loading.then(f);
  p.catch(() => undefined);
  return p;
}

/** aiMenu is a row's right-click handler: its own menu (own), then Add
 * to AI Chat for item (not for a viewer, who cannot send in the chat). */
export function aiMenu(item: AIItem, own?: (e: MouseEvent) => void): (e: MouseEvent) => void {
  return (e) => {
    if (link.meta.peek()?.role === "viewer") return own?.(e);
    const add = { label: t("ai.attach"), run: () => withAI((m) => m.add(item)) };
    own?.(e);
    const m = menu.peek();
    if (own && m) menu.value = { ...m, items: [...m.items, null, add] };
    else {
      e.preventDefault();
      e.stopPropagation();
      openMenu(e.clientX, e.clientY, add.label, [add]);
    }
  };
}

/** aiDrag starts a row's drag to the AI chat (aictx/drop.ts takes the
 * drop); aictx.js loads meanwhile. */
export const aiDrag = (item: AIItem) => (e: DragEvent) => (e.dataTransfer?.setData("application/x-termocode-item", JSON.stringify(item)), withAI(() => 0));

/** aiGrid is a table grid's right-click handler: the item under the
 * pointer (aictx.js reads which). */
export function aiGrid(at: Omit<GridAt, "el" | "x" | "y">): (e: MouseEvent) => void {
  return (e) => {
    e.preventDefault();
    const g = { ...at, el: e.target as HTMLElement, x: e.clientX, y: e.clientY };
    withAI((m) => m.grid(g));
  };
}

// The chat input's @ and its sends are watched by aictx.js: it loads once
// the input takes the focus.
// The chat's look is in aictx.js too: it loads with the workbench.
withAI((m) => (look.value = m.look));

addEventListener("focusin", (e) => (e.target as Element).matches?.(".composer-box") && withAI(() => 0));
