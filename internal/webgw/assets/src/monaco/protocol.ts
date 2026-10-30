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
}

export type ToFrame =
  | ({ t: "init"; doc: string; label: string } & FrameOpts)
  | ({ t: "opts" } & FrameOpts)
  /** remote: the laptop's text. seen is the newest frame version the
   * page had; a frame that changed since keeps its text and sends it. */
  | { t: "remote"; text: string; seen: number }
  /** replace: the user chose this text (one undo step). */
  | { t: "replace"; text: string }
  | { t: "hl"; rows: HlRow[] }
  | { t: "undo" }
  | { t: "redo" }
  | { t: "focus" }
  | { t: "reveal"; line: number };

export type FromFrame =
  | { t: "ready" }
  /** text: the frame's text at version v; user says the user changed it
   * (typing, paste, undo), not the page. */
  | { t: "text"; text: string; v: number; user: boolean; canUndo: boolean; canRedo: boolean }
  | { t: "viewport"; first: number; last: number }
  | { t: "cursor"; line: number; col: number }
  | { t: "save" }
  /** key: a workbench key pressed in the editor (Ctrl/Cmd+P, +B, +J …). */
  | { t: "key"; key: string; code: string; ctrl: boolean; meta: boolean; shift: boolean; alt: boolean };

/** FRAME_PATH is where the gateway serves the frame. */
export const FRAME_PATH = "/assets/monaco.html";
