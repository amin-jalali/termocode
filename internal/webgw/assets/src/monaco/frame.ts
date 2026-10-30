// The desktop editor's frame (monaco.html): the vendored editor over one
// text, driven by the page with messages (protocol.ts). The frame has no
// link and no token; it only shows the text, paints the colours the page
// sends, and tells the page what the user typed.
//
// Undo is the page's own, as in the phone editor (editor/history.ts):
// the user's changes are steps, the laptop's changes are not, and every
// step moves over them, so Ctrl/Cmd+Z takes back the user's own typing
// and never the laptop's.
import { ChangeSet, Text } from "@codemirror/state";
import { dict } from "../state/dict";
import { History } from "../editor/history";
import { diff } from "../lib/textdiff";
import type { FrameOpts, FromFrame, ToFrame } from "./protocol";
import "./frame.css";

// The small part of the editor's API this file uses.
interface Pos {
  lineNumber: number;
  column: number;
}
interface IRange {
  startLineNumber: number;
  startColumn: number;
  endLineNumber: number;
  endColumn: number;
}
interface Change {
  rangeOffset: number;
  rangeLength: number;
  text: string;
}
interface EditOp {
  range: IRange;
  text: string;
  forceMoveMarkers?: boolean;
}
interface Model {
  getValue(): string;
  setEOL(eol: number): void;
  getPositionAt(offset: number): Pos;
  getLineCount(): number;
  getLineContent(line: number): string;
  applyEdits(ops: EditOp[]): void;
  onDidChangeContent(cb: (e: { changes: Change[] }) => void): void;
}
interface Decoration {
  range: IRange;
  options: { inlineClassName?: string; className?: string; isWholeLine?: boolean };
}
interface Collection {
  set(d: Decoration[]): void;
  clear(): void;
}
interface KeyEvent {
  browserEvent: KeyboardEvent;
  preventDefault(): void;
  stopPropagation(): void;
}
interface Editor {
  getModel(): Model | null;
  executeEdits(source: string, ops: EditOp[]): boolean;
  pushUndoStop(): boolean;
  updateOptions(o: Record<string, unknown>): void;
  focus(): void;
  layout(): void;
  revealLineInCenter(line: number): void;
  setPosition(p: Pos): void;
  getVisibleRanges(): IRange[];
  createDecorationsCollection(d?: Decoration[]): Collection;
  onDidChangeCursorPosition(cb: (e: { position: Pos }) => void): void;
  onDidScrollChange(cb: () => void): void;
  onDidLayoutChange(cb: () => void): void;
  onKeyDown(cb: (e: KeyEvent) => void): void;
  addCommand(keys: number, run: () => void): unknown;
}
interface EditorNS {
  create(el: HTMLElement, o: Record<string, unknown>): Editor;
  defineTheme(name: string, data: { base: string; inherit: boolean; rules: unknown[]; colors: Record<string, string> }): void;
  setTheme(name: string): void;
}
interface Api {
  editor: EditorNS;
  KeyMod: { CtrlCmd: number; Shift: number };
  KeyCode: { KeyZ: number; KeyY: number; KeyS: number };
}

const api = (globalThis as unknown as { termocodeModule: (n: string) => Api }).termocodeModule("vs/editor-BdtEMBbM");

/** The page is the frame's parent, on the same origin. */
function post(m: FromFrame): void {
  parent.postMessage(m, location.origin);
}

let ed: Editor | null = null;
let model: Model | null = null;
let cur = Text.empty;
let v = 0;
const hist = new History();
/** applying is set while the frame itself changes the text. */
let applying: "remote" | "undo" | "replace" | null = null;
let hlDecos: Collection | null = null;
let flashDecos: Collection | null = null;
let flashTimer: ReturnType<typeof setTimeout> | undefined;

function sendText(user: boolean): void {
  post({ t: "text", text: cur.toString(), v, user, canUndo: hist.canUndo(), canRedo: hist.canRedo() });
}

/** ops turns a change set on the current text into editor edits. */
function ops(cs: ChangeSet): EditOp[] {
  const out: EditOp[] = [];
  const m = model;
  if (!m) return out;
  cs.iterChanges((fromA, toA, _fromB, _toB, ins) => {
    const a = m.getPositionAt(fromA), b = m.getPositionAt(toA);
    out.push({ range: { startLineNumber: a.lineNumber, startColumn: a.column, endLineNumber: b.lineNumber, endColumn: b.column }, text: ins.toString(), forceMoveMarkers: true });
  });
  return out;
}

function apply(cs: ChangeSet, how: "remote" | "undo" | "replace"): void {
  if (!ed || !model || cs.empty) return;
  applying = how;
  try {
    if (how === "remote") model.applyEdits(ops(cs));
    else {
      ed.pushUndoStop();
      ed.executeEdits("termocode", ops(cs));
      ed.pushUndoStop();
    }
  } finally {
    applying = null;
  }
}

function flash(cs: ChangeSet): void {
  if (!ed || !flashDecos) return;
  let a = -1, b = -1;
  cs.iterChangedRanges((_fA, _tA, fB, tB) => {
    if (a < 0) a = fB;
    b = tB;
  });
  if (a < 0) return;
  const la = cur.lineAt(Math.min(a, cur.length)).number, lb = cur.lineAt(Math.min(b, cur.length)).number;
  flashDecos.set([{ range: { startLineNumber: la, startColumn: 1, endLineNumber: Math.min(lb, la + 200), endColumn: 1 }, options: { isWholeLine: true, className: "tc-laptop" } }]);
  clearTimeout(flashTimer);
  flashTimer = setTimeout(() => flashDecos?.clear(), 1500);
}

function textChange(text: string): ChangeSet | null {
  const d = diff(cur.toString(), text);
  if (!d) return null;
  return ChangeSet.of([{ from: d.from, to: d.to, insert: d.insert }], cur.length);
}

function undoRedo(redo: boolean): void {
  const cs = redo ? hist.redo(cur) : hist.undo(cur);
  if (!cs) return;
  apply(cs, "undo");
}

function viewport(): void {
  const r = ed?.getVisibleRanges() ?? [];
  if (!r.length) return;
  post({ t: "viewport", first: r[0].startLineNumber - 1, last: r[r.length - 1].endLineNumber });
}

// The workbench keys go to the page; the editor keeps its own (find,
// go to line, multi-cursor …).
function workbenchKey(e: KeyboardEvent): boolean {
  const mod = e.ctrlKey || e.metaKey;
  if (!mod) return false;
  const k = e.key.toLowerCase();
  if (e.shiftKey) return k === "p" || k === "e" || k === "g" || k === "f";
  return ["p", "b", "j", "w", "`", "tab", "k", ","].includes(k) || (e.ctrlKey && k === "pagedown") || (e.ctrlKey && k === "pageup");
}

function colors(c: Record<string, string>): Record<string, string> {
  const g = (k: string, def: string) => c[k] ?? def;
  return {
    "editor.background": g("--wb-editor-bg", "#1e1e1e"),
    "editor.foreground": g("--wb-editor-fg", "#d4d4d4"),
    "editorLineNumber.foreground": g("--wb-line-num", "#858585"),
    "editorLineNumber.activeForeground": g("--wb-line-num-active", "#c6c6c6"),
    "editor.lineHighlightBorder": g("--wb-line-hl", "#282828"),
    "editorIndentGuide.background1": g("--wb-indent", "#404040"),
    "editor.selectionBackground": g("--wb-sel", "#264f78"),
    "editorWidget.background": g("--wb-side-bg", "#252526"),
    "focusBorder": g("--wb-focus", "#007fd4"),
    "scrollbarSlider.background": g("--wb-scroll", "#79797966"),
    "scrollbarSlider.hoverBackground": g("--wb-scroll-hover", "#646464b3"),
  };
}

function setOpts(o: FrameOpts): void {
  if (o.colors) {
    for (const [k, val] of Object.entries(o.colors)) document.documentElement.style.setProperty(k, val);
    api.editor.defineTheme("termocode", { base: o.dark === false ? "vs" : "vs-dark", inherit: true, rules: [], colors: colors(o.colors) });
    api.editor.setTheme("termocode");
  }
  if (!ed) return;
  const u = dict<unknown>();
  if (o.readOnly !== undefined) u.readOnly = o.readOnly;
  if (o.wrap !== undefined) u.wordWrap = o.wrap ? "on" : "off";
  if (o.tabSize !== undefined && o.tabSize > 0) u.tabSize = o.tabSize;
  if (o.insertSpaces !== undefined) u.insertSpaces = o.insertSpaces;
  ed.updateOptions(u);
}

function init(m: Extract<ToFrame, { t: "init" }>): void {
  setOpts(m);
  const box = document.getElementById("editor");
  if (!box || ed) return;
  cur = Text.of(m.doc.split("\n"));
  ed = api.editor.create(box, {
    value: m.doc,
    language: "plaintext",
    theme: "termocode",
    readOnly: m.readOnly === true,
    wordWrap: m.wrap ? "on" : "off",
    tabSize: m.tabSize && m.tabSize > 0 ? m.tabSize : 4,
    insertSpaces: m.insertSpaces === true,
    detectIndentation: false,
    automaticLayout: true,
    minimap: { enabled: true },
    scrollBeyondLastLine: true,
    renderLineHighlight: "line",
    contextmenu: true,
    fixedOverflowWidgets: true,
    ariaLabel: m.label,
    unicodeHighlight: { ambiguousCharacters: false },
    links: false,
    quickSuggestions: false,
    wordBasedSuggestions: "off",
    parameterHints: { enabled: false },
    suggestOnTriggerCharacters: false,
    occurrencesHighlight: "singleFile",
    bracketPairColorization: { enabled: true },
    guides: { indentation: true, bracketPairs: false },
  });
  model = ed.getModel();
  model?.setEOL(0);
  hlDecos = ed.createDecorationsCollection();
  flashDecos = ed.createDecorationsCollection();
  model?.onDidChangeContent((e) => {
    const specs: { from: number; to: number; insert: string }[] = e.changes.map((c) => ({ from: c.rangeOffset, to: c.rangeOffset + c.rangeLength, insert: c.text }));
    const cs = ChangeSet.of(specs, cur.length);
    const before = cur;
    cur = cs.apply(cur);
    if (applying === "remote") {
      hist.remote(cs);
      flash(cs);
      return; // the page knows this text
    }
    v++; // a change the page has not seen yet (the laptop's are not counted)
    if (applying === null) hist.user(cs, before, Date.now());
    else if (applying === "replace") hist.user(cs, before, Date.now(), false);
    sendText(true);
  });
  const { KeyMod, KeyCode } = api;
  ed.addCommand(KeyMod.CtrlCmd | KeyCode.KeyZ, () => undoRedo(false));
  ed.addCommand(KeyMod.CtrlCmd | KeyMod.Shift | KeyCode.KeyZ, () => undoRedo(true));
  ed.addCommand(KeyMod.CtrlCmd | KeyCode.KeyY, () => undoRedo(true));
  ed.addCommand(KeyMod.CtrlCmd | KeyCode.KeyS, () => post({ t: "save" }));
  ed.onKeyDown((e) => {
    const b = e.browserEvent;
    if (!workbenchKey(b)) return;
    e.preventDefault();
    e.stopPropagation();
    post({ t: "key", key: b.key, code: b.code, ctrl: b.ctrlKey, meta: b.metaKey, shift: b.shiftKey, alt: b.altKey });
  });
  ed.onDidChangeCursorPosition((e) => post({ t: "cursor", line: e.position.lineNumber, col: e.position.column }));
  ed.onDidScrollChange(viewport);
  ed.onDidLayoutChange(viewport);
  viewport();
  sendText(false);
}

function onMessage(e: MessageEvent): void {
  if (e.source !== parent || e.origin !== location.origin) return;
  const m = e.data as ToFrame;
  if (typeof m !== "object" || m === null || typeof m.t !== "string") return;
  switch (m.t) {
    case "init":
      init(m);
      return;
    case "opts":
      setOpts(m);
      return;
    case "remote": {
      if (m.seen < v) {
        sendText(true); // the user typed since: the page sees it first
        return;
      }
      const cs = textChange(m.text);
      if (cs) apply(cs, "remote");
      return;
    }
    case "replace": {
      const cs = textChange(m.text);
      if (cs) apply(cs, "replace");
      return;
    }
    case "hl": {
      if (!hlDecos || !model) return;
      const out: Decoration[] = [];
      const n = model.getLineCount();
      for (const r of m.rows) {
        if (r.row >= n || model.getLineContent(r.row + 1) !== r.text) continue;
        for (const s of r.segs) {
          if (s.cls && s.to > s.from) out.push({ range: { startLineNumber: r.row + 1, startColumn: s.from + 1, endLineNumber: r.row + 1, endColumn: s.to + 1 }, options: { inlineClassName: "hl-" + s.cls } });
        }
      }
      hlDecos.set(out);
      return;
    }
    case "undo":
      undoRedo(false);
      return;
    case "redo":
      undoRedo(true);
      return;
    case "focus":
      ed?.focus();
      return;
    case "reveal":
      ed?.revealLineInCenter(m.line);
      ed?.setPosition({ lineNumber: m.line, column: 1 });
      ed?.focus();
      return;
  }
}

addEventListener("message", onMessage);
post({ t: "ready" });
