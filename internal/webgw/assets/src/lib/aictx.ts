// The workbench's side of the AI chat's extras (aictx.js, loaded on
// first use; contract in aictx/api.ts): a row's right-click menu gets Add
// to AI Chat after its own items, and a table grid's right click opens
// the item under it. aictx.js loads when the user asks, or once the chat
// input takes the focus (there it watches for @ and for a send).
import { h, render } from "preact";
import { effect } from "@preact/signals";
import type { AIHost, AIItem, GridAt } from "../aictx/api";
import type * as AICtx from "../aictx/main";
import { link, store } from "../session";
import { hostCtrs, hosts, loadHosts } from "../state/hosts";
import { addStrings, t } from "../strings";
import { menu, openMenu } from "../ui/ContextMenu";
import { Sheet } from "../ui/Sheet";
import { pinned } from "../views/Chat";
import { ctrList } from "../views/desk/DeskContainers";
import { conns, loadConns, tables } from "../views/desk/dbstate";
import { addAttach, attaches } from "./attach";
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
  showChat: () => setPanes({ chat: true }),
  conns,
  tables,
  loadConns,
  loadHosts,
  ctrs: ctrList,
  hosts,
  hostCtrs,
});

let loading: Promise<Mod> | null = null;

/** withAI runs f with aictx.js, loading it once (a failed load can be
 * tried again). */
export function withAI(f: (m: Mod) => void): void {
  if (!loading)
    loading = import("/assets/aictx.js").then(
      (m: Mod) => (m.init(host()), m),
      (e: unknown) => {
        loading = null;
        throw e;
      },
    );
  loading.then(f, () => toast("warn", t("cmd.failed")));
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
addEventListener("focusin", (e) => (e.target as Element).matches?.(".composer-box") && withAI(() => 0));
