// The phone Term page's AI parts (pterm.js, contract in pterm/api.ts):
// the terminal loads them with its one dynamic import and gives them the
// page's Preact, link, chat refs and menus, so neither app.js nor
// home.js carries them. A failed load is tried again on the next open.
import { Fragment, h } from "preact";
import { useState } from "preact/hooks";
import { signal } from "@preact/signals";
import type { PTermHost } from "../pterm/api";
import type * as PTerm from "../pterm/main";
import { link } from "../session";
import { t } from "../strings";
import { Chip } from "../ui/Chip";
import { errText } from "../ui/CardSheet";
import { RowMenu } from "../ui/RowMenu";
import { typeTerm } from "../views/phone/termtype";
import { copyText } from "./clip";
import { pendingDraft } from "./drafts";
import { linkItems } from "./links";
import { go } from "./router";
import { toast } from "./toasts";
import { addRef, toAttach } from "./tochat";

type Mod = typeof PTerm;

/** ptermMod is the loaded module (null: not yet). */
export const ptermMod = signal<Mod | null>(null);

let p: Promise<Mod> | undefined;

const host = (): PTermHost => ({
  h,
  Fragment,
  useState,
  request: (op, args) => link.request(op as never, args as never),
  toast: (kind, title) => void toast(kind, title),
  errText,
  type: typeTerm,
  addRef,
  attach: toAttach,
  askChat: (text) => {
    pendingDraft.value = { text, notChat: "" };
    go({ tab: "chat", rest: "", query: {} });
  },
  copy: copyText,
  links: linkItems,
  RowMenu,
  Chip,
  words: () => ({ copyAll: t("term.copy_all"), paste: t("term.paste"), again: t("item.run_again") }),
});

/** loadPTerm loads pterm.js once (again after a failure). */
export function loadPTerm(): void {
  p ??= import("/assets/pterm.js").then((m: Mod) => {
    m.init(host());
    return (ptermMod.value = m);
  });
  p.catch(() => (p = undefined));
}
