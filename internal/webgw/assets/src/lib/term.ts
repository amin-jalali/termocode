// Older terminal rows (WEB-20): the term topic keeps the last 2 000 rows;
// "Load older rows" pages earlier ones with term.history and keeps them
// here, above the topic's rows. Rows the topic drops from its top while
// output grows move here too, so what the reader paged in stays in one
// piece (up to 10 000 rows, like the laptop's scrollback).
import { signal } from "@preact/signals";
import type { TermHistoryRes } from "../../../../webproto/proto";
import { OfflineError } from "../net/link";
import { link } from "../session";
import { errText } from "../ui/CardSheet";
import { toast } from "./toasts";

export const MAX_OLDER = 10000;

export interface Older {
  buf: number;
  /** rows are buffer rows [first, first + rows.length). */
  first: number;
  rows: string[];
  more: boolean;
}

export const older = signal<Older | null>(null);
export const loadingOlder = signal(false);

/** loadOlder pages the rows before row before (the first row shown). */
export function loadOlder(buf: number, before: number): Promise<void> {
  if (loadingOlder.peek()) return Promise.resolve();
  loadingOlder.value = true;
  return link.request("term.history", { buf, before }).then(
    (data) => {
      loadingOlder.value = false;
      const res = data as TermHistoryRes;
      const cur = older.peek();
      const keep = cur && cur.buf === buf && cur.first === before ? cur.rows : [];
      const rows = [...(res.rows ?? []), ...keep].slice(0, MAX_OLDER);
      older.value = { buf, first: res.first, rows, more: res.more === true };
    },
    (e: unknown) => {
      loadingOlder.value = false;
      if (!(e instanceof OfflineError)) toast("warn", errText(e));
    },
  );
}

/** stitch keeps the paged rows joined to the topic's rows when the topic's
 * window moves down from prevFirst to first (prevRows were its rows). */
export function stitch(buf: number, prevFirst: number, prevRows: readonly string[], first: number): void {
  const cur = older.peek();
  if (!cur || cur.buf !== buf) return;
  const end = cur.first + cur.rows.length;
  if (end === first) return;
  if (end === prevFirst && first > prevFirst && first - prevFirst <= prevRows.length) {
    const rows = [...cur.rows, ...prevRows.slice(0, first - prevFirst)];
    const drop = Math.max(0, rows.length - MAX_OLDER);
    older.value = { ...cur, first: cur.first + drop, rows: rows.slice(drop), more: cur.more || drop > 0 };
    return;
  }
  older.value = null; // no longer joined (a reset): start again from the topic
}

export function dropOlder(): void {
  older.value = null;
}
