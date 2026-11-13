// The other people's carets (G-COL), in coedit.js: the laptop's cursor
// from the buf topic as the editor draws it, and this page's caret sent
// to the laptop (its editor shows it with the device's name).
import type { BufSnap } from "../../../../webproto/proto";
import type { FramePeer } from "../monaco/protocol";
import { utf8Len } from "../lib/textdiff";
import { host } from "./host";
import { colUtf16 } from "./merge";

/** peersOf is the carets to draw in a buffer: the laptop's, named
 * laptop. Columns become the editor's (1-based, UTF-16). */
export function peersOf(snap: BufSnap | null, laptop: string): FramePeer[] {
  const lines = snap?.lines ?? [];
  const at = (row: number, col: number) => ({ line: row + 1, col: colUtf16(lines[row] ?? "", col) + 1 });
  const c = snap?.laptop;
  return c ? [{ ...at(c.row, c.col), sel: c.sel ? at(c.sel.row, c.sel.col) : undefined, name: laptop }] : [];
}

/** carets are this page's carets waiting to go out, by buffer. */
const carets = new Map<number, { line: number; col: number }>();

/** caret tells the laptop where this page's caret is, at most every
 * 200 ms; the newest wins. line and col are the editor's (1-based,
 * UTF-16); text is the editor's text when it goes. */
export function caret(buf: number, line: number, col: number, text: () => string): void {
  const waiting = carets.has(buf);
  carets.set(buf, { line, col });
  if (waiting) return;
  setTimeout(() => {
    const c = carets.get(buf);
    carets.delete(buf);
    if (!c || !host.live()) return;
    const row = text().split("\n")[c.line - 1] ?? "";
    host.request("buf.cursor", { buf, row: c.line - 1, col: utf8Len(row.slice(0, c.col - 1)) }).then(
      () => undefined,
      () => undefined,
    );
  }, 200);
}
