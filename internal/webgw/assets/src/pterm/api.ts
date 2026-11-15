// The contract between the page and the phone Term page's AI parts
// (pterm.js, built alone: cmd/webbuild ptermChunk): the chips and the
// "Ask AI to write a command" field above the keys, and the menu of a
// long press on lines. home.js loads it (lib/pterm.ts) with the terminal
// and gives it everything with state through this host, so pterm.js
// imports types and pure helpers only: one Preact, one session.
import type { Fragment as PFragment, h as PH } from "preact";
import type * as Hooks from "preact/hooks";
import type { ChatRef, AddResult } from "../lib/tochat";
import type { MenuItem, RowMenu as PRowMenu } from "../ui/RowMenu";
import type { Chip as PChip } from "../ui/Chip";

export interface PTermHost {
  h: typeof PH;
  Fragment: typeof PFragment;
  useState: typeof Hooks.useState;
  request(op: string, args: unknown): Promise<unknown>;
  toast(kind: "ok" | "warn", title: string): void;
  errText(e: unknown): string;
  /** type sends bytes to the shown shell (views/phone/termtype.ts). */
  type(text: string): void;
  addRef(r: ChatRef, title?: string): AddResult;
  /** attach is a ref as chat.send's attach (lib/tochat.ts toAttach). */
  attach(r: ChatRef, label: string): unknown;
  /** askChat opens the chat with text in its box (the ref waits there). */
  askChat(text: string): void;
  copy(text: string): void;
  links(text: string): MenuItem[];
  RowMenu: typeof PRowMenu;
  Chip: typeof PChip;
  /** words are the page's own (its strings, in the page's language). */
  words(): { copyAll: string; paste: string; again: string };
}
