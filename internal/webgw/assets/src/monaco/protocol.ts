// The messages between the page and the desktop editor's frame (the
// workbench editor, monaco.html). The frame holds the vendored editor and
// nothing else: no link, no token, no store. It gets the text and the
// colours from the page and gives back the text the user typed. Both
// sides check every message's origin and source.
import type { HlRow } from "../editor/api";

/** FrameOpts are the editor's settings the page can change at any time. */
export interface FrameOpts {
  readOnly?: boolean;
  wrap?: boolean;
  tabSize?: number;
  insertSpaces?: boolean;
  /** colors are the workbench's colours by token ("--wb-editor-bg": "#1e1e1e"). */
  colors?: Record<string, string>;
  dark?: boolean;
  /** lsp: ask the page for hover, completion and definitions (a file
   * with a language server on the laptop); refsLabel names the editor's
   * "find references" action. */
  lsp?: boolean;
  refsLabel?: string;
  /** lang: the editor colours the text itself ("sql": a query editor,
   * where Ctrl/Cmd+Enter runs the query). */
  lang?: "sql";
}

/** FrameMarker is one diagnostic for the editor's squiggles: rows and
 * columns 1-based in the editor's units (UTF-16), the end excluded. */
export interface FrameMarker {
  line: number;
  col: number;
  endLine: number;
  endCol: number;
  sev: number;
  message: string;
  source?: string;
  code?: string;
}

/** FrameLines colours rows [from, to] (1-based) of the editor. */
export interface FrameLines {
  from: number;
  to: number;
  kind: "current" | "incoming" | "marker";
}

/** LspKind is what the frame asks the page about a position. */
export type LspKind = "hover" | "complete" | "definition";

/** FrameDiff makes the editor a side-by-side diff: original is the older
 * text (read only, on the left); the frame's own text is the newer one. */
export interface FrameDiff {
  original: string;
  /** inline: one column with the removed rows over the added ones. */
  inline?: boolean;
  /** collapse: unchanged regions fold away (a click opens them). */
  collapse?: boolean;
}

/** GlyphKind is what the editor's glyph margin shows on a row: a
 * breakpoint (conditional, logpoint, not verified) or a test's state. */
export type GlyphKind = "bp" | "bp-cond" | "bp-log" | "bp-off" | "test-pending" | "test-passed" | "test-failed" | "test-running" | "test-skipped";

/** FrameGlyph is one row's mark in the glyph margin (1-based). */
export interface FrameGlyph {
  line: number;
  kind: GlyphKind;
  /** title is its tooltip. */
  title?: string;
}

export type ToFrame =
  /** init: path is the file's workspace path (a definition there stays
   * in this editor); diff makes it a diff editor. */
  | ({ t: "init"; doc: string; label: string; path?: string; diff?: FrameDiff } & FrameOpts)
  /** original: the diff's older text changed (git read it again). */
  | { t: "original"; text: string }
  /** diffView: switch side by side / inline, fold unchanged regions. */
  | { t: "diffView"; inline?: boolean; collapse?: boolean }
  /** insert: put text at the cursor as the user's own edit (the chat's
   * Insert at Cursor). */
  | { t: "insert"; text: string }
  /** diffGo: to the next (1) or previous (-1) change. */
  | { t: "diffGo"; dir: 1 | -1 }
  | ({ t: "opts" } & FrameOpts)
  /** remote: the laptop's text. seen is the newest frame version the
   * page had; a frame that changed since keeps its text and sends it. */
  | { t: "remote"; text: string; seen: number }
  /** replace: the user chose this text (one undo step). */
  | { t: "replace"; text: string }
  | { t: "hl"; rows: HlRow[] }
  /** gutter: git change marks, rows [from, to] 1-based. */
  | { t: "gutter"; marks: { from: number; to: number; kind: "add" | "mod" | "del" }[] }
  /** lines: whole rows [from, to] (1-based) in a colour of the merge
   * editor: the current or incoming side of a conflict, or its markers. */
  | { t: "lines"; items: FrameLines[] }
  | { t: "undo" }
  | { t: "redo" }
  | { t: "focus" }
  | { t: "reveal"; line: number; col?: number }
  /** edit: a context menu choice. cut drops the selection (the page put
   * it on the clipboard); def, refs and format run the editor's own action. */
  | { t: "edit"; op: "cut" | "def" | "refs" | "format" }
  /** markers: the file's diagnostics (all of them, each time). */
  | { t: "markers"; items: FrameMarker[] }
  /** glyphs: the glyph margin's marks (all of them, each time) and the
   * row the debugger stopped on (0: none). */
  | { t: "glyphs"; items: FrameGlyph[]; stop: number }
  /** lspRes answers the frame's lsp question id (data null: nothing). */
  | { t: "lspRes"; id: number; data: unknown };

/** MenuCan says which of the editor's actions work here now. */
export interface MenuCan {
  def: boolean;
  refs: boolean;
  format: boolean;
}

export type FromFrame =
  | { t: "ready" }
  /** text: the frame's text at version v; user says the user changed it
   * (typing, paste, undo), not the page. */
  | { t: "text"; text: string; v: number; user: boolean; canUndo: boolean; canRedo: boolean }
  | { t: "viewport"; first: number; last: number }
  /** diff: the diff editor counted its changes (after each change). */
  | { t: "diff"; changes: number; added: number; removed: number }
  | { t: "cursor"; line: number; col: number }
  | { t: "save" }
  /** menu: a right click at (x, y) in the frame; sel is the selected text. */
  | { t: "menu"; x: number; y: number; sel: string; readOnly: boolean; can: MenuCan }
  /** lsp asks about a position: line 0-based, col in UTF-16 units. */
  | { t: "lsp"; id: number; kind: LspKind; line: number; col: number }
  /** refs: the user asked for the references at a position (0-based). */
  | { t: "refs"; line: number; col: number }
  /** open: go to another file (a definition there), 0-based. */
  | { t: "open"; path: string; line: number; col: number }
  /** glyph: a click in the glyph margin on a 1-based row; menu: a right
   * click at (x, y) in the frame. */
  | { t: "glyph"; line: number; menu: boolean; x: number; y: number }
  /** key: a workbench key pressed in the editor (Ctrl/Cmd+P, +B, +J …). */
  | { t: "key"; key: string; code: string; ctrl: boolean; meta: boolean; shift: boolean; alt: boolean };

/** FRAME_PATH is where the gateway serves the frame. */
export const FRAME_PATH = "/assets/monaco.html";
