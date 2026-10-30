// The reducers of the file and terminal topics (web design 2.3–2.5): how
// one delta of tree, buf:<n> and term:<buf> folds into the topic's data.
// They mirror the hub's apply functions (internal/app/web_tree.go,
// web_buf.go, web_term.go) rule for rule, so apply(snap₀, deltas…) gives
// the hub's own snapₙ (TestStoreReplayMatchesGo). bufs and terms send the
// whole list as their delta and keep the store's default reducer.
//
// Pure: no DOM, timers or network; inputs are never changed.
import type { BufSnap, TermSnap, TreeDir, TreeSnap } from "../../../../webproto/proto";
import { arr, bad, int, obj, str } from "./delta";
import type { Reducer } from "./store";

/** MAX_TERM_ROWS is webproto.MaxTermRows: a terminal view keeps its last 2 000 rows. */
export const MAX_TERM_ROWS = 2000;

export interface Lines {
  first: number;
  rows: string[];
}

/** linesApply replaces rows [d.first, d.last) (buffer rows) in the window
 * that starts at row first, then keeps the last max rows (0 = all). It is
 * webLinesApply: null when the window cannot know the result (it would
 * need rows it does not have). */
export function linesApply(first: number, rows: readonly string[], dFirst: number, dLast: number, lines: readonly string[], max: number): Lines | null {
  if (dFirst < 0 || dLast < dFirst) return null;
  const end = first + rows.length;
  if (dLast > end) return null;
  let out: string[];
  if (dLast <= first) {
    // All above the window: it moves.
    first += lines.length - (dLast - dFirst);
    out = rows.slice();
  } else if (dFirst < first) {
    // Reaches into the window from above.
    out = [...lines, ...rows.slice(dLast - first)];
    first = dFirst;
  } else {
    out = [...rows.slice(0, dFirst - first), ...lines, ...rows.slice(dLast - first)];
  }
  if (first < 0) return null;
  if (max > 0 && out.length > max) {
    first += out.length - max;
    out = out.slice(out.length - max);
  }
  if (max > 0 && out.length < max && first > 0) return null; // older rows would show: unknown here
  return { first, rows: out };
}

function strs(v: unknown, what: string): string[] {
  return arr<unknown>(v, what).map((x) => {
    if (typeof x !== "string") bad(what + " holds a non-text row");
    return x;
  });
}

function bool(v: unknown, what: string): boolean {
  if (typeof v !== "boolean") bad(what + " is not true or false");
  return v;
}

/** under reports whether folder p is dir or inside it (webTreeUnder). */
export function under(dir: string, p: string): boolean {
  return dir === "" || p === dir || p.startsWith(dir + "/");
}

/** tree: dir replaces one folder's listing, remove drops a folder and
 * every folder under it, hidden follows the laptop's dotfile toggle. */
export const reduceTree: Reducer = (data, delta) => {
  const s = obj(data, "tree") as unknown as TreeSnap;
  const d = obj(delta, "tree delta");
  const dirs: Record<string, TreeDir> = { ...(s.dirs ?? {}) };
  switch (d.kind) {
    case "dir": {
      const dir: TreeDir = { entries: d.entries === undefined ? (null as unknown as TreeDir["entries"]) : (arr(d.entries, "entries") as TreeDir["entries"]) };
      if (d.truncated === true) dir.truncated = true;
      dirs[str(d.path)] = dir;
      return { ...s, dirs };
    }
    case "remove": {
      const path = str(d.path);
      for (const p of Object.keys(dirs)) if (under(path, p)) delete dirs[p];
      return { ...s, dirs };
    }
    case "hidden":
      return { ...s, dirs, hidden: bool(d.on, "hidden") };
    default:
      return bad("tree: unknown delta kind " + String(d.kind));
  }
};

/** markGone returns data with a gone mark the views read. The mark is not
 * enumerable: the hub's own snapshot has no such field (a gone delta
 * changes nothing in it), so the replay check still compares equal. */
function markGone<T extends object>(data: T): T {
  return Object.defineProperty({ ...data }, "gone", { value: true, enumerable: false });
}

/** isGone: the laptop closed the buffer; the topic sends nothing more. */
export function isGone(data: unknown): boolean {
  return typeof data === "object" && data !== null && (data as { gone?: unknown }).gone === true;
}

/** buf:<n>: lines replaces rows, tick and dirty set one field, reset
 * replaces the whole buffer, gone ends the topic. */
export const reduceBuf: Reducer = (data, delta) => {
  const s = obj(data, "buf") as unknown as BufSnap;
  const d = obj(delta, "buf delta");
  const lines = strs(s.lines, "lines");
  switch (d.kind) {
    case "lines": {
      const r = linesApply(0, lines, int(d.first, "first"), int(d.last, "last"), strs(d.lines, "lines"), 0);
      if (!r) bad("buf: lines outside the buffer");
      return { ...s, lines: r.rows, tick: int(d.tick, "tick") };
    }
    case "tick":
      return { ...s, lines, tick: int(d.tick, "tick") };
    case "dirty":
      return { ...s, lines, dirty: bool(d.on, "dirty") };
    case "reset":
      return { ...s, tick: int(d.tick, "tick"), lines: strs(d.lines, "lines"), dirty: bool(d.dirty, "dirty") };
    case "gone":
      return markGone({ ...s, lines });
    default:
      return bad("buf: unknown delta kind " + String(d.kind));
  }
};

/** term:<buf>: lines replaces rows and keeps the window's last 2 000,
 * reset replaces the window, mode turns screen mode on or off, gone ends
 * the topic. more always says whether older rows exist. */
export const reduceTerm: Reducer = (data, delta) => {
  const s = obj(data, "term") as unknown as TermSnap;
  const d = obj(delta, "term delta");
  let out: TermSnap = { ...s, rows: strs(s.rows, "rows") };
  switch (d.kind) {
    case "lines": {
      const r = linesApply(int(s.first, "first"), out.rows, int(d.first, "first"), int(d.last, "last"), strs(d.lines, "lines"), MAX_TERM_ROWS);
      if (!r) bad("term: lines do not fit the window");
      out = { ...out, first: r.first, rows: r.rows, tick: int(d.tick, "tick") };
      break;
    }
    case "tick":
      out.tick = int(d.tick, "tick");
      break;
    case "reset":
      out = { ...out, first: int(d.first, "first"), rows: strs(d.rows, "rows"), tick: int(d.tick, "tick") };
      break;
    case "mode":
      out = { ...out, screen: bool(d.screen, "screen"), cols: int(d.cols, "cols"), height: int(d.height, "height") };
      break;
    case "gone":
      out.more = out.first > 0;
      return markGone(out);
    default:
      return bad("term: unknown delta kind " + String(d.kind));
  }
  out.more = out.first > 0;
  return out;
};
