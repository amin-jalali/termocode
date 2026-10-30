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
import type { LspCompletion, LspHover, LspLocations } from "../../../../webproto/proto";
import type { HlRow } from "../editor/api";
import { md } from "./mdtext";
import type { FrameMarker, FrameOpts, FromFrame, LspKind, MenuCan, ToFrame } from "./protocol";
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
interface Uri {
  scheme: string;
  path: string;
}
interface Word {
  word: string;
  startColumn: number;
  endColumn: number;
}
interface Model {
  uri: Uri;
  getValue(): string;
  setValue(v: string): void;
  getLinesContent(): string[];
  getWordUntilPosition(p: Pos): Word;
  getWordAtPosition(p: Pos): Word | null;
  setEOL(eol: number): void;
  getPositionAt(offset: number): Pos;
  getLineCount(): number;
  getLineContent(line: number): string;
  getValueInRange(r: IRange): string;
  applyEdits(ops: EditOp[]): void;
  onDidChangeContent(cb: (e: { changes: Change[] }) => void): void;
}
interface Decoration {
  range: IRange;
  options: { inlineClassName?: string; className?: string; isWholeLine?: boolean; linesDecorationsClassName?: string };
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
  revealPositionInCenter(p: Pos): void;
  setPosition(p: Pos): void;
  getPosition(): Pos | null;
  addAction(a: { id: string; label: string; keybindings?: number[]; contextMenuGroupId?: string; contextMenuOrder?: number; run: () => void }): unknown;
  getVisibleRanges(): IRange[];
  createDecorationsCollection(d?: Decoration[]): Collection;
  onDidChangeCursorPosition(cb: (e: { position: Pos }) => void): void;
  onDidScrollChange(cb: () => void): void;
  onDidLayoutChange(cb: () => void): void;
  onKeyDown(cb: (e: KeyEvent) => void): void;
  addCommand(keys: number, run: () => void): unknown;
  getSelections(): IRange[] | null;
  getAction(id: string): { isSupported(): boolean; run(): Promise<void> } | null;
  getContainerDomNode(): HTMLElement;
}
interface LineChange {
  originalStartLineNumber: number;
  originalEndLineNumber: number;
  modifiedStartLineNumber: number;
  modifiedEndLineNumber: number;
}
interface DiffEditor {
  getModifiedEditor(): Editor;
  getOriginalEditor(): Editor;
  setModel(m: { original: Model; modified: Model }): void;
  updateOptions(o: Record<string, unknown>): void;
  goToDiff(target: "next" | "previous"): void;
  getLineChanges(): LineChange[] | null;
  onDidUpdateDiff(cb: () => void): void;
}
/** The editor's own diff computer (the code its worker runs), called here:
 * the frame runs no worker. */
interface LinesDiff {
  changes: unknown[];
  moves: unknown[];
  hitTimeout: boolean;
}
interface DiffHost {
  linesDiffComputers: { getDefault(): { computeDiff(a: string[], b: string[], o: { ignoreTrimWhitespace: boolean; maxComputationTimeMs: number; computeMoves: boolean }): LinesDiff } };
}
interface EditorNS {
  create(el: HTMLElement, o: Record<string, unknown>): Editor;
  createDiffEditor(el: HTMLElement, o: Record<string, unknown>, services?: Record<string, object>): DiffEditor;
  getModel(uri: Uri): Model | null;
  createModel(text: string, lang?: string): Model;
  defineTheme(name: string, data: { base: string; inherit: boolean; rules: unknown[]; colors: Record<string, string> }): void;
  setTheme(name: string): void;
}
interface Token {
  isCancellationRequested: boolean;
}
interface MarkerData {
  severity: number;
  message: string;
  source?: string;
  code?: string;
  startLineNumber: number;
  startColumn: number;
  endLineNumber: number;
  endColumn: number;
}
interface Suggestion {
  label: string;
  kind: number;
  detail?: string;
  documentation?: string;
  insertText: string;
  insertTextRules?: number;
  sortText?: string;
  filterText?: string;
  range: IRange;
  additionalTextEdits?: { range: IRange; text: string }[];
}
interface Languages {
  registerHoverProvider(sel: string, p: { provideHover(m: Model, p: Pos, t: Token): Promise<{ contents: { value: string; isTrusted: boolean; supportHtml: boolean }[]; range?: IRange } | null> }): unknown;
  registerCompletionItemProvider(sel: string, p: { triggerCharacters: string[]; provideCompletionItems(m: Model, p: Pos, c: unknown, t: Token): Promise<{ suggestions: Suggestion[]; incomplete?: boolean }> }): unknown;
  registerDefinitionProvider(sel: string, p: { provideDefinition(m: Model, p: Pos, t: Token): Promise<{ uri: Uri; range: IRange }[] | null> }): unknown;
}
interface Api {
  editor: EditorNS & {
    setModelMarkers(m: Model, owner: string, markers: MarkerData[]): void;
    registerEditorOpener(o: { openCodeEditor(source: Editor, resource: Uri, sel?: IRange | Pos): boolean }): unknown;
  };
  languages: Languages;
  Uri: { from(c: { scheme: string; path: string }): Uri };
  KeyMod: { CtrlCmd: number; Shift: number; Alt: number };
  KeyCode: { KeyZ: number; KeyY: number; KeyS: number; F12: number; F7: number; F5: number };
}

const mod = (globalThis as unknown as { termocodeModule: (n: string) => unknown }).termocodeModule;
const api = mod("vs/editor-BdtEMBbM") as Api;

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
let gutterDecos: Collection | null = null;
let flashTimer: ReturnType<typeof setTimeout> | undefined;
let readOnly = false;
/** The diff editor (a diff tab), its older side and that side's colours. */
let diffEd: DiffEditor | null = null;
let origModel: Model | null = null;
let origDecos: Collection | null = null;
let lastHl: readonly HlRow[] = [];

/** workerless stands in for the editor's worker service (the frame runs
 * no worker, its policy says worker-src 'none'): the diff editor's diff
 * is computed here, with the editor's own algorithm (the code its worker
 * would run). Everything else the service offers answers "nothing". */
const workerless: object = new Proxy(
  {
    computeDiff(a: Uri, b: Uri, o: { ignoreTrimWhitespace: boolean; maxComputationTimeMs: number; computeMoves: boolean }) {
      const ma = api.editor.getModel(a), mb = api.editor.getModel(b);
      if (!ma || !mb) return Promise.resolve(null);
      const host = mod("vs/editorWorkerHost-B_VpI68A") as DiffHost;
      const r = host.linesDiffComputers.getDefault().computeDiff(ma.getLinesContent(), mb.getLinesContent(), {
        ignoreTrimWhitespace: o.ignoreTrimWhitespace,
        maxComputationTimeMs: o.maxComputationTimeMs,
        computeMoves: o.computeMoves,
      });
      return Promise.resolve({ identical: r.changes.length === 0, quitEarly: r.hitTimeout, changes: r.changes, moves: r.moves });
    },
  },
  {
    get(target, k) {
      if (k === "computeDiff") return Reflect.get(target, k);
      if (typeof k === "string" && k.startsWith("can")) return () => false;
      return () => Object.assign(Promise.resolve(null), { dispose() {} });
    },
  },
);

/** origHl colours the older side: a row whose text the newer side has
 * coloured gets the same colours (the hub colours the open buffer only). */
function origHl(): void {
  if (!origDecos || !origModel) return;
  const byText = new Map<string, HlRow["segs"]>();
  for (const r of lastHl) if (!byText.has(r.text)) byText.set(r.text, r.segs);
  const out: Decoration[] = [];
  const n = origModel.getLineCount();
  for (let i = 1; i <= n; i++) {
    const segs = byText.get(origModel.getLineContent(i));
    if (!segs) continue;
    for (const g of segs) if (g.cls && g.to > g.from) out.push({ range: oneLineAt(i, g.from + 1, g.to + 1), options: { inlineClassName: "hl-" + g.cls } });
  }
  origDecos.set(out);
}

function oneLineAt(line: number, a: number, b: number): IRange {
  return { startLineNumber: line, startColumn: a, endLineNumber: line, endColumn: b };
}

/** diffCount tells the page how many changes the diff has. */
function diffCount(): void {
  const ch = diffEd?.getLineChanges() ?? [];
  let added = 0, removed = 0;
  for (const c of ch) {
    if (c.modifiedEndLineNumber > 0) added += c.modifiedEndLineNumber - c.modifiedStartLineNumber + 1;
    if (c.originalEndLineNumber > 0) removed += c.originalEndLineNumber - c.originalStartLineNumber + 1;
  }
  post({ t: "diff", changes: ch.length, added, removed });
}

/** MENU_SEL_MAX caps the selected text a right click hands the page. */
const MENU_SEL_MAX = 1 << 20;

/** FORMAT is the editor's own action the context menu may run. */
const FORMAT = "editor.action.formatDocument";

/** can: Go to Definition and Find All References work while the editor
 * asks the laptop's language servers; Format when the editor can format. */
function can(): MenuCan {
  return { def: lspOn, refs: lspOn, format: !readOnly && ed?.getAction(FORMAT)?.isSupported() === true };
}

/** caretRefs: the page's references list for the word at the caret. */
function caretRefs(): void {
  const p = ed?.getPosition();
  if (p) post({ t: "refs", line: p.lineNumber - 1, col: p.column - 1 });
}

/** goDef: the menu's Go to Definition, the same lsp.definition read as
 * F12: a place in this file gets the caret, one in another file opens
 * there (the page opens its tab). */
async function goDef(): Promise<void> {
  const p = ed?.getPosition();
  if (!p || !lspOn) return;
  const d = (await ask("definition", p)) as LspLocations | null;
  const l = d && Array.isArray(d.locs) ? d.locs.find((x) => !x.outside && x.path) : undefined;
  if (!l?.path || !ed) return;
  if (l.path !== myPath) {
    post({ t: "open", path: l.path, line: l.line, col: l.col });
    return;
  }
  const at = { lineNumber: l.line + 1, column: l.col + 1 };
  ed.setPosition(at);
  ed.revealPositionInCenter(at);
  ed.focus();
}

/** onMenu: the page draws the context menu (the editor's own is off). */
function onMenu(e: MouseEvent): void {
  e.preventDefault();
  e.stopPropagation();
  const sel = (ed?.getSelections() ?? []).map((r) => model?.getValueInRange(r) ?? "").join("\n");
  post({ t: "menu", x: e.clientX, y: e.clientY, sel: sel.length > MENU_SEL_MAX ? "" : sel, readOnly, can: can() });
}

/** menuEdit runs a context menu choice. */
function menuEdit(m: Extract<ToFrame, { t: "edit" }>): void {
  if (!ed) return;
  if (m.op === "def" || m.op === "refs" || m.op === "format") {
    ed.focus();
    if (m.op === "def") void goDef();
    else if (m.op === "refs") caretRefs();
    else void ed.getAction(FORMAT)?.run();
    return;
  }
  if (readOnly) return;
  ed.pushUndoStop();
  ed.executeEdits("menu", (ed.getSelections() ?? []).map((range) => ({ range, text: "", forceMoveMarkers: true })));
  ed.pushUndoStop();
  ed.focus();
}

// Language features: the frame asks the page (which asks the laptop's
// language servers through the hub) and waits for the answer by id.
let myPath = "";
let lspOn = false;
let lspReady = false;
let nextAsk = 0;
const asks = new Map<number, (d: unknown) => void>();

/** COMPLETE_MS: completion asks once typing pauses this long. */
const COMPLETE_MS = 80;
/** ASK_MS: a question the page does not answer in time gets nothing. */
const ASK_MS = 10_000;
const OPEN_SCHEME = "termocode-file";

/** SEV maps a diagnostic's severity (1 error … 4 hint) to the editor's. */
const SEV: Readonly<Record<number, number>> = { 1: 8, 2: 4, 3: 2, 4: 1 };
/** KINDS maps the protocol's CompletionItemKind (index) to the editor's. */
const KINDS = [18, 18, 0, 1, 2, 3, 4, 5, 7, 8, 9, 12, 13, 15, 17, 27, 19, 20, 21, 23, 16, 14, 6, 10, 11, 24];

function ask(kind: LspKind, p: Pos): Promise<unknown> {
  const id = ++nextAsk;
  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      asks.delete(id);
      resolve(null);
    }, ASK_MS);
    asks.set(id, (d) => {
      clearTimeout(timer);
      asks.delete(id);
      resolve(d);
    });
    post({ t: "lsp", id, kind, line: p.lineNumber - 1, col: p.column - 1 });
  });
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

function oneLine(line: number, a: number, b: number): IRange {
  return { startLineNumber: line, startColumn: a, endLineNumber: line, endColumn: b };
}

/** markers paints the file's diagnostics (squiggles, ruler, hover). */
function markers(items: readonly FrameMarker[]): void {
  if (!model) return;
  api.editor.setModelMarkers(
    model,
    "termocode",
    items.map((d) => ({
      severity: SEV[d.sev] ?? 8,
      message: d.message,
      source: d.source || undefined,
      code: d.code || undefined,
      startLineNumber: d.line,
      startColumn: d.col,
      endLineNumber: d.endLine,
      endColumn: d.endCol,
    })),
  );
}

/** startLsp registers the providers once (they serve this frame's model). */
function startLsp(): void {
  if (lspReady) return;
  lspReady = true;
  const L = api.languages;
  L.registerHoverProvider("*", {
    async provideHover(m, p, tk) {
      if (!lspOn || m !== model) return null;
      const d = (await ask("hover", p)) as LspHover | null;
      if (tk.isCancellationRequested || !d || !d.md) return null;
      const w = m.getWordAtPosition(p);
      return { contents: [{ value: md(d.md), isTrusted: false, supportHtml: false }], range: w ? oneLine(p.lineNumber, w.startColumn, w.endColumn) : undefined };
    },
  });
  L.registerCompletionItemProvider("*", {
    triggerCharacters: ["."],
    async provideCompletionItems(m, p, _c, tk) {
      const none = { suggestions: [] };
      if (!lspOn || m !== model) return none;
      await sleep(COMPLETE_MS);
      if (tk.isCancellationRequested) return none;
      const d = (await ask("complete", p)) as LspCompletion | null;
      if (tk.isCancellationRequested || !d || !Array.isArray(d.items)) return none;
      const w = m.getWordUntilPosition(p);
      const def = oneLine(p.lineNumber, w.startColumn, p.column);
      return {
        incomplete: d.incomplete === true,
        suggestions: d.items.map((it) => ({
          label: it.label,
          kind: KINDS[it.kind ?? 1] ?? 18,
          detail: it.detail || undefined,
          documentation: it.doc || undefined,
          insertText: it.insert,
          insertTextRules: it.snippet ? 4 : undefined,
          sortText: it.sort || undefined,
          filterText: it.filter || undefined,
          range: it.from >= 0 && it.to >= it.from ? oneLine(p.lineNumber, it.from + 1, it.to + 1) : def,
          additionalTextEdits: it.edits?.map((e) => ({ range: { startLineNumber: e.line + 1, startColumn: e.col + 1, endLineNumber: e.endLine + 1, endColumn: e.endCol + 1 }, text: e.text })),
        })),
      };
    },
  });
  L.registerDefinitionProvider("*", {
    async provideDefinition(m, p, tk) {
      if (!lspOn || m !== model) return null;
      const d = (await ask("definition", p)) as LspLocations | null;
      if (tk.isCancellationRequested || !d || !Array.isArray(d.locs)) return null;
      const out: { uri: Uri; range: IRange }[] = [];
      for (const l of d.locs) {
        if (l.outside || !l.path) continue;
        const uri = l.path === myPath ? m.uri : api.Uri.from({ scheme: OPEN_SCHEME, path: "/" + l.path });
        out.push({ uri, range: { startLineNumber: l.line + 1, startColumn: l.col + 1, endLineNumber: l.endLine + 1, endColumn: l.endCol + 1 } });
      }
      return out.length ? out : null;
    },
  });
  // A definition in another file opens there: the page opens its tab.
  api.editor.registerEditorOpener({
    openCodeEditor(_src, r, sel) {
      if (r.scheme !== OPEN_SCHEME) return false;
      const at = sel && "startLineNumber" in sel ? { lineNumber: sel.startLineNumber, column: sel.startColumn } : (sel ?? { lineNumber: 1, column: 1 });
      post({ t: "open", path: r.path.replace(/^\//, ""), line: at.lineNumber - 1, col: at.column - 1 });
      return true;
    },
  });
}

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
  if (e.shiftKey) return k === "p" || k === "e" || k === "g" || k === "f" || k === "m";
  return ["p", "b", "j", "w", "`", "tab", "k", ",", "\\", "1", "2"].includes(k) || (e.ctrlKey && k === "pagedown") || (e.ctrlKey && k === "pageup");
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
  if (o.readOnly !== undefined) {
    u.readOnly = o.readOnly;
    readOnly = o.readOnly;
  }
  if (o.wrap !== undefined) u.wordWrap = o.wrap ? "on" : "off";
  if (o.tabSize !== undefined && o.tabSize > 0) u.tabSize = o.tabSize;
  if (o.insertSpaces !== undefined) u.insertSpaces = o.insertSpaces;
  ed.updateOptions(u);
}

function init(m: Extract<ToFrame, { t: "init" }>): void {
  // The editor's services start with the first editor: the diff editor
  // brings its own worker service, so the theme waits until after it.
  if (!m.diff) setOpts(m);
  const box = document.getElementById("editor");
  if (!box || ed) return;
  cur = Text.of(m.doc.split("\n"));
  const opts = {
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
    contextmenu: false, // the page draws it (onMenu)
    fixedOverflowWidgets: true,
    ariaLabel: m.label,
    // The frame runs no worker (its policy says worker-src 'none'): the
    // features that would start the editor's worker stay off.
    unicodeHighlight: { ambiguousCharacters: false, invisibleCharacters: false, nonBasicASCII: false },
    colorDecorators: false,
    links: false,
    wordBasedSuggestions: "off",
    parameterHints: { enabled: false },
    occurrencesHighlight: "singleFile",
    bracketPairColorization: { enabled: true },
    guides: { indentation: true, bracketPairs: false },
    // With a language server on the laptop: completion while typing
    // (asked once typing pauses) and after ".", like the laptop's editor.
    quickSuggestions: m.lsp ? { other: "on", comments: "off", strings: "off" } : false,
    suggestOnTriggerCharacters: m.lsp === true,
    gotoLocation: { multiple: "goto" },
    definitionLinkOpensInPeek: false,
  };
  if (m.diff) {
    // A diff tab: the older text on the left (read only), this text on
    // the right; the right side is the editor every message below drives.
    diffEd = api.editor.createDiffEditor(box, {
      ...opts,
      minimap: { enabled: false },
      originalEditable: false,
      renderSideBySide: m.diff.inline !== true,
      useInlineViewWhenSpaceIsLimited: true,
      enableSplitViewResizing: true,
      renderOverviewRuler: true,
      ignoreTrimWhitespace: true,
      renderMarginRevertIcon: false,
      renderIndicators: true,
      hideUnchangedRegions: { enabled: m.diff.collapse !== false },
    }, { editorWorkerService: workerless });
    origModel = api.editor.createModel(m.diff.original, "plaintext");
    origModel.setEOL(0);
    const modModel = api.editor.createModel(m.doc, "plaintext");
    diffEd.setModel({ original: origModel, modified: modModel });
    ed = diffEd.getModifiedEditor();
    origDecos = diffEd.getOriginalEditor().createDecorationsCollection();
    diffEd.onDidUpdateDiff(diffCount);
    for (const side of [ed, diffEd.getOriginalEditor()]) side.getContainerDomNode().addEventListener("contextmenu", (e) => e.preventDefault(), true);
  } else ed = api.editor.create(box, { ...opts, value: m.doc });
  readOnly = m.readOnly === true;
  if (m.diff) setOpts(m);
  ed.getContainerDomNode().addEventListener("contextmenu", onMenu, true);
  model = ed.getModel();
  model?.setEOL(0);
  hlDecos = ed.createDecorationsCollection();
  flashDecos = ed.createDecorationsCollection();
  gutterDecos = ed.createDecorationsCollection();
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
  if (diffEd) {
    const d = diffEd;
    for (const side of [ed, d.getOriginalEditor()]) {
      side.addCommand(KeyCode.F7, () => d.goToDiff("next"));
      side.addCommand(KeyMod.Shift | KeyCode.F7, () => d.goToDiff("previous"));
      side.addCommand(KeyMod.Alt | KeyCode.F5, () => d.goToDiff("next"));
      side.addCommand(KeyMod.Shift | KeyMod.Alt | KeyCode.F5, () => d.goToDiff("previous"));
    }
  }
  myPath = m.path ?? "";
  lspOn = m.lsp === true;
  if (lspOn) {
    startLsp();
    ed.addAction({
      id: "termocode.references",
      label: m.refsLabel ?? "References",
      keybindings: [KeyMod.Shift | KeyCode.F12],
      contextMenuGroupId: "navigation",
      contextMenuOrder: 1.5,
      run: caretRefs,
    });
  }
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
      lastHl = m.rows;
      origHl();
      return;
    }
    case "original":
      if (origModel && origModel.getValue() !== m.text) {
        origModel.setValue(m.text);
        origHl();
      }
      return;
    case "diffView": {
      const u = dict<unknown>();
      if (m.inline !== undefined) u.renderSideBySide = !m.inline;
      if (m.collapse !== undefined) u.hideUnchangedRegions = { enabled: m.collapse };
      diffEd?.updateOptions(u);
      return;
    }
    case "diffGo":
      diffEd?.goToDiff(m.dir > 0 ? "next" : "previous");
      ed?.focus();
      return;
    case "gutter":
      gutterDecos?.set(
        m.marks.map((g) => ({
          range: { startLineNumber: g.from, startColumn: 1, endLineNumber: g.to, endColumn: 1 },
          options: { isWholeLine: true, linesDecorationsClassName: "tc-gm tc-gm-" + g.kind },
        })),
      );
      return;
    case "undo":
      undoRedo(false);
      return;
    case "redo":
      undoRedo(true);
      return;
    case "focus":
      ed?.focus();
      return;
    case "edit":
      menuEdit(m);
      return;
    case "reveal": {
      const at = { lineNumber: m.line, column: m.col ?? 1 };
      ed?.setPosition(at);
      ed?.revealPositionInCenter(at);
      ed?.focus();
      return;
    }
    case "markers":
      markers(m.items);
      return;
    case "lspRes":
      asks.get(m.id)?.(m.data);
      return;
  }
}

addEventListener("message", onMessage);
post({ t: "ready" });
