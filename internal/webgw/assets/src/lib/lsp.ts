// The desktop editor's language features (workbench): the laptop's
// language servers through the hub, never one in the browser. Problems
// come from the diagnostics topic (squiggles in the editor, the PROBLEMS
// panel, the counts in the status bar).
import { effect, signal } from "@preact/signals";
import type { DiagFile, DiagSnap } from "../../../../webproto/proto";
import type { FrameMarker } from "../monaco/protocol";
import { store } from "../session";
import { route } from "./router";
import { panes, setPanes, showFile } from "./workbench";

/** PanelTab is what the bottom panel shows. */
export type PanelTab = "problems" | "terminal";
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
