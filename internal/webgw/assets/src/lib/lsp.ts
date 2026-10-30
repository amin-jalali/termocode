// The desktop editor's language features (workbench): the laptop's
// language servers through the hub, never one in the browser. Problems
// come from the diagnostics topic (squiggles in the editor, the PROBLEMS
// panel, the counts in the status bar); hover, completion and go to
// definition are asked by the editor's frame (lib/deskeditor.ts) and
// answered here with the lsp.* reads; find references fills the
// references list (ui/RefsPeek.tsx). A read is asked at the changedtick
// the page's text is known at: unsent typing goes out first.
import { effect, signal } from "@preact/signals";
import type { DiagFile, DiagSnap, LspLocations, LspLoc } from "../../../../webproto/proto";
import type { FrameMarker, LspKind } from "../monaco/protocol";
import { ReqError } from "../net/link";
import { link, store } from "../session";
import { session } from "./live";
import { route } from "./router";
import { panes, setPanes, showFile } from "./workbench";

/** PanelTab is what the bottom panel shows: "problems", "output", "terminal" or
 * a registered tab's id (lib/views.ts). */
export type PanelTab = string;
export const panelTab = signal<PanelTab>("terminal");

// A terminal route shows the terminal in the panel.
effect(() => {
  if (route.value.tab === "term") panelTab.value = "terminal";
});

/** toggleProblems shows the PROBLEMS panel, or hides the panel when it
 * shows already (Ctrl/Cmd+Shift+M, the status bar's counts). */
export function toggleProblems(): void {
  if (panes.peek().panel && panelTab.peek() === "problems") return setPanes({ panel: false });
  panelTab.value = "problems";
  setPanes({ panel: true });
}

/** diagSnap reads the diagnostics topic (null: not here yet or odd). */
export function diagSnap(data: unknown): DiagSnap | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as DiagSnap;
  return Array.isArray(d.files) ? d : null;
}

/** diags is the diagnostics topic now. */
export function diags(): DiagSnap | null {
  return diagSnap(store.topic("diagnostics").value?.data);
}

/** fileMarkers turns a file's diagnostics into the editor's markers
 * (1-based; an empty range marks one character, as the laptop does). */
export function fileMarkers(f: DiagFile | undefined): FrameMarker[] {
  if (!f) return [];
  return f.items.map((d) => {
    const empty = d.endLine === d.line && d.endCol <= d.col;
    return {
      line: d.line + 1,
      col: d.col + 1,
      endLine: d.endLine + 1,
      endCol: (empty ? d.col + 1 : d.endCol) + 1,
      sev: d.sev,
      message: d.message,
      source: d.source,
      code: d.code,
    };
  });
}

/** revealAt is a place to show once its file is in the editor. */
export const revealAt = signal<{ path: string; line: number; col: number } | null>(null);

/** goTo opens path in the editor at a 0-based line and UTF-16 column. */
export function goTo(path: string, line: number, col: number): void {
  revealAt.value = { path, line, col };
  showFile(path);
}

/** noServer remembers buffers whose file has no language server, for a
 * while, so typing does not ask again and again. */
const noServer = new Map<number, number>();
const NO_SERVER_MS = 30_000;

/** settledTick sends what is unsent and gives the tick the page's text
 * is known at (null: unsent text, a conflict, or no live editor). */
async function settledTick(buf: number): Promise<number | null> {
  const s = session.peek();
  if (!s || s.buf !== buf) return null;
  await s.flush();
  if (session.peek() !== s) return null;
  return s.settled();
}

/** ask runs lsp.<kind> for buffer buf at a 0-based line and UTF-16
 * column, at the tick the page's text is known at. A changed buffer is
 * asked once more after a moment (the laptop's change on its way). */
export async function ask(kind: LspKind | "references", buf: number, line: number, col: number): Promise<unknown> {
  const until = noServer.get(buf);
  if (until !== undefined && Date.now() < until) return null;
  for (let tries = 0; tries < 2; tries++) {
    const tick = await settledTick(buf);
    if (tick === null) return null;
    try {
      return await link.request("lsp." + kind, { buf, tick, line, col });
    } catch (e) {
      if (e instanceof ReqError && e.code === "conflict" && tries === 0) {
        await new Promise((r) => setTimeout(r, 200));
        continue;
      }
      if (e instanceof ReqError && e.code === "not_found" && kind !== "references") noServer.set(buf, Date.now() + NO_SERVER_MS);
      if (kind === "references") throw e;
      return null;
    }
  }
  return null;
}

/** Refs is the references list: where it was asked, and its places. */
export interface Refs {
  path: string;
  line: number;
  word: string;
  state: "loading" | "done" | "error";
  locs: LspLoc[];
  cut?: boolean;
  note?: string;
}

export const refs = signal<Refs | null>(null);

/** findRefs fills the references list for a position of buffer buf. */
export function findRefs(path: string, buf: number, line: number, col: number, word: string): void {
  const mine: Refs = { path, line, word, state: "loading", locs: [] };
  refs.value = mine;
  ask("references", buf, line, col).then(
    (data) => {
      if (refs.peek() !== mine) return;
      const d = data as LspLocations | null;
      refs.value = { ...mine, state: "done", locs: d && Array.isArray(d.locs) ? d.locs : [], cut: d?.cut };
    },
    (e: unknown) => {
      if (refs.peek() !== mine) return;
      refs.value = { ...mine, state: "error", note: e instanceof Error ? e.message : "" };
    },
  );
}

/** wordAt is the word around a UTF-16 column of a row (for the list's title). */
export function wordAt(row: string, col: number): string {
  const isW = (c: string) => /[\p{L}\p{N}_$]/u.test(c);
  let a = col, b = col;
  while (a > 0 && isW(row[a - 1])) a--;
  while (b < row.length && isW(row[b])) b++;
  return row.slice(a, b);
}
