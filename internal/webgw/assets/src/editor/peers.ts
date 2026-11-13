// The other people's carets in the phone's editor (G-COL), pure: the
// carets (1-based line, UTF-16 column, as the desktop editor takes them)
// as offsets in the document, kept inside it.
import type { Text } from "@codemirror/state";
import type { FramePeer } from "../monaco/protocol";

export interface PeerRange {
  at: number;
  name: string;
  /** from and to: the selection, when there is one. */
  from?: number;
  to?: number;
}

function offset(doc: Text, line: number, col: number): number {
  const l = doc.line(Math.min(Math.max(line, 1), doc.lines));
  return l.from + Math.min(Math.max(col - 1, 0), l.length);
}

/** peerRanges places the carets in doc. */
export function peerRanges(doc: Text, items: readonly FramePeer[]): PeerRange[] {
  return items.map((p) => {
    const at = offset(doc, p.line, p.col);
    if (!p.sel) return { at, name: p.name };
    const s = offset(doc, p.sel.line, p.sel.col);
    return s === at ? { at, name: p.name } : { at, name: p.name, from: Math.min(s, at), to: Math.max(s, at) };
  });
}
