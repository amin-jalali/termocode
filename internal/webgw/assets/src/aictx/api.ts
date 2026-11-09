// The contract between the desktop workbench and the AI chat's extras
// (aictx.js): Add to AI Chat (a right click's item, an @ in the chat
// input, an item dropped on the chat), AI: Side Question… and, beside
// them, a grid's Export Result…. aictx.js is built alone (cmd/webbuild
// aictxChunk) and the workbench loads it on first use (lib/aictx.ts
// withAI). It imports types and pure helpers only and gets everything
// with state through this host, so there is one Preact and one session,
// and workbench.js stays small.
import type { h as PH, render as PRender } from "preact";
import type { ReadonlySignal, Signal } from "@preact/signals";
import type { Attach, ChatSnap } from "../../../../webproto/proto";
import type { Link } from "../net/link";
import type { DBRowsView } from "../state/more";
import type { Store } from "../state/store";
import type { effect as PEffect } from "@preact/signals";
import type { quick as Quick } from "../lib/workbench";
import type { loadHosts as LoadHosts } from "../state/hosts";
import type { ctrList as CtrList } from "../views/desk/DeskContainers";
import type { HostCtr, HostLoad, HostView } from "../state/hosts";
import type { StringKey } from "../strings";
import type { MenuItems } from "../ui/ContextMenu";
import type { Sheet as PSheet } from "../ui/Sheet";
import type { conns as Conns, loadConns as LoadConns, tables as Tables } from "../views/desk/dbstate";

/** ItemKind is an item of Add to AI Chat (webproto ItemSpec.item). */
export type ItemKind = "db_conn" | "db_table" | "db_column" | "db_rows" | "db_cell" | "host" | "compose_project" | "container";

/** ItemSpec is an item attach's spec (webproto ItemSpec): the item's
 * address in the names termocode's MCP tools take, and its summary. */
export interface ItemSpec {
  item: ItemKind;
  host?: string;
  project?: string;
  container?: string;
  conn?: string;
  schema?: string;
  table?: string;
  column?: string;
  key?: string[];
  columns?: string[];
  rows?: (string | null)[][];
  masked?: string[];
  ddl?: string;
}

/** AIItem is a row's item: its address (the chunk adds the summary).
 * row is a row's number in its grid (from 1). */
export interface AIItem extends ItemSpec {
  row?: number;
}

/** GridAt is a right click in a table's grid: the item under it. */
export interface GridAt {
  conn: string;
  table: string;
  r: DBRowsView;
  /** from is the first shown row's number - 1. */
  from: number;
  /** el is the element the right click hit (a cell, a header). */
  el: HTMLElement;
  x: number;
  y: number;
}

export interface AIHost {
  h: typeof PH;
  render: typeof PRender;
  link: Link;
  t(key: StringKey, vars?: Record<string, string | number>): string;
  addStrings(d: Readonly<Record<string, string>>): void;
  toast(kind: "ok" | "info" | "warn", text: string): void;
  openMenu(x: number, y: number, label: string, items: MenuItems): void;
  effect: typeof PEffect;
  /** quick is the open quick input (a question, a picker). */
  quick: typeof Quick;
  Sheet: typeof PSheet;
  /** store and pinned say whose composer the chips go to: the old chat
   * the page reads (pinned), else the laptop's open one. */
  store: Store;
  pinned: ReadonlySignal<ChatSnap | null>;
  attaches: Signal<Readonly<Record<string, readonly Attach[]>>>;
  addAttach(chat: string, a: Attach): boolean;
  /** showChat opens the chat pane. */
  showChat(): void;
  loadConns: typeof LoadConns;
  loadHosts: typeof LoadHosts;
  /** ctrs is the Containers view's list of the host in use. */
  ctrs: typeof CtrList;
  conns: typeof Conns;
  /** tables is the Databases tree's read tables, by connection. */
  tables: typeof Tables;
  hosts: ReadonlySignal<HostLoad<HostView[]> | null>;
  hostCtrs: ReadonlySignal<ReadonlyMap<string, HostLoad<HostCtr[]>>>;
}
