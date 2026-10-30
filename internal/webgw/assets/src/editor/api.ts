// The contract between the page and the in-page editor (WEB-39). The
// editor lives in its own file, editor.js (CodeMirror and this glue, no
// Preact), which the page loads only when the user starts editing; the
// page imports nothing from it but these types.

/** HlSeg is a coloured run of one row: UTF-16 columns and a class. */
export interface HlSeg {
  from: number;
  to: number;
  cls: string;
}

/** HlRow is the colours of a row, for the text they were made for. */
export interface HlRow {
  row: number;
  text: string;
  segs: readonly HlSeg[];
}

/** EditorHost is what the page gives the editor. */
export interface EditorHost {
  doc: string;
  /** label names the editor for screen readers. */
  label: string;
  /** onChange: the user changed the text (typing, paste, undo). */
  onChange(): void;
  /** onSave: Ctrl/Cmd+S. onDone: Esc. */
  onSave(): void;
  onDone(): void;
  /** onViewport hears which rows [first, last) are drawn. */
  onViewport(first: number, last: number): void;
}

/** EditorHandle is how the page drives a mounted editor. */
export interface EditorHandle {
  text(): string;
  /** remote shows the laptop's text: not an undo step; the page's own
   * undo steps follow it. The changed rows flash. */
  remote(text: string): void;
  /** replace sets the text as one undo step (the user chose it). */
  replace(text: string): void;
  setHl(rows: readonly HlRow[]): void;
  undo(): boolean;
  redo(): boolean;
  canUndo(): boolean;
  canRedo(): boolean;
  focus(): void;
  destroy(): void;
}

export interface EditorModule {
  mount(parent: HTMLElement, host: EditorHost): EditorHandle;
}
