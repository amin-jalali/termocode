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
import type { AICompletion, AINextEdit, LspCompletion, LspHover, LspLocations } from "../../../../webproto/proto";
import type { HlRow } from "../editor/api";
import { esc, md } from "./mdtext";
import type { AiAt, AiKind, FrameBlame, FrameGlyph, FrameMarker, FrameOpts, FromFrame, LspKind, MenuCan, ToFrame } from "./protocol";
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
  getLineMaxColumn(line: number): number;
  getValueInRange(r: IRange): string;
  applyEdits(ops: EditOp[]): void;
  onDidChangeContent(cb: (e: { changes: Change[] }) => void): void;
}
interface Decoration {
  range: IRange;
  options: {
    inlineClassName?: string;
    className?: string;
    isWholeLine?: boolean;
    linesDecorationsClassName?: string;
    glyphMarginClassName?: string;
    glyphMarginHoverMessage?: { value: string };
    stickiness?: number;
    after?: { content: string; inlineClassName?: string };
    showIfCollapsed?: boolean;
  };
}
/** MouseTarget is where a mouse event of the editor landed (type 2:
 * the glyph margin). */
interface MouseTarget {
  type: number;
  position: Pos | null;
}
interface EditorMouse {
  target: MouseTarget;
  event: { rightButton: boolean; leftButton: boolean; browserEvent: MouseEvent };
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
  onMouseDown(cb: (e: EditorMouse) => void): void;
  onMouseMove(cb: (e: EditorMouse) => void): void;
  onMouseLeave(cb: () => void): void;
  onDidScrollChange(cb: () => void): void;
  onDidLayoutChange(cb: () => void): void;
  onKeyDown(cb: (e: KeyEvent) => void): void;
  addCommand(keys: number, run: () => void, when?: string): unknown;
  createContextKey(key: string, def: boolean): { set(v: boolean): void };
  changeViewZones(cb: (a: { addZone(z: { afterLineNumber: number; heightInLines: number; domNode: HTMLElement }): string; removeZone(id: string): void }) => void): void;
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
interface InlineItem {
  insertText: string;
  range: IRange;
  additionalTextEdits?: { range: IRange; text: string }[];
}
interface Languages {
  register(l: { id: string }): void;
  setMonarchTokensProvider(id: string, def: unknown): unknown;
  registerInlineCompletionsProvider(sel: string, p: { provideInlineCompletions(m: Model, p: Pos, c: { triggerKind: number }, t: Token): Promise<{ items: InlineItem[] }>; freeInlineCompletions(): void; disposeInlineCompletions(): void }): unknown;
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
  KeyCode: { KeyZ: number; KeyY: number; KeyS: number; KeyI: number; F12: number; F7: number; F5: number; Tab: number; Escape: number };
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
let lineDecos: Collection | null = null;
let flashTimer: ReturnType<typeof setTimeout> | undefined;
let readOnly = false;
/** The diff editor (a diff tab), its older side and that side's colours. */
let diffEd: DiffEditor | null = null;
let origModel: Model | null = null;
let origDecos: Collection | null = null;
let lastHl: readonly HlRow[] = [];
/** The glyph margin (breakpoints, tests), the debugger's stopped row and
 * the faint breakpoint under the pointer. */
let glyphDecos: Collection | null = null;
let hintDecos: Collection | null = null;
let glyphRows = new Set<number>();
/** glyphMenu: a right click in the glyph margin opens the page's menu
 * for that row instead of the editor's. */
let glyphMenu = 0;
/** lastStop: the editor shows a new stopped row once, then the user scrolls. */
let lastStop = 0;
const GLYPH_MARGIN = 2;

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
  if (glyphMenu) {
    post({ t: "glyph", line: glyphMenu, menu: true, x: e.clientX, y: e.clientY });
    glyphMenu = 0;
    return;
  }
  const sel = (ed?.getSelections() ?? []).map((r) => model?.getValueInRange(r) ?? "").join("\n");
  post({ t: "menu", x: e.clientX, y: e.clientY, sel: sel.length > MENU_SEL_MAX ? "" : sel, readOnly, can: can(), at: aiAt() });
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

/** Blame: each line's blame (null: off), shown at the cursor's line. */
let blameRows: (FrameBlame | null)[] | null = null;
let blameDecos: Collection | null = null;
let blameHover = false;

/** blameAt is the blame of the cursor's line, if any. */
function blameAt(line: number): FrameBlame | null {
  return blameRows?.[line - 1] ?? null;
}

/** blamePaint puts the cursor line's blame at its end, faded. */
function blamePaint(): void {
  if (!blameDecos || !ed || !model) return;
  const p = ed.getPosition();
  const b = p ? blameAt(p.lineNumber) : null;
  if (!p || !b) {
    blameDecos.clear();
    return;
  }
  const end = model.getLineMaxColumn(p.lineNumber);
  blameDecos.set([{ range: oneLineAt(p.lineNumber, end, end), options: { showIfCollapsed: true, after: { content: "\u2003" + b.text, inlineClassName: "tc-blame" } } }]);
}

/** startBlameHover shows the commit when the pointer is on the blame. */
function startBlameHover(): void {
  if (blameHover) return;
  blameHover = true;
  api.languages.registerHoverProvider("*", {
    async provideHover(m, p) {
      if (m !== model || !ed) return null;
      const cur = ed.getPosition();
      const b = blameAt(p.lineNumber);
      const end = m.getLineMaxColumn(p.lineNumber);
      if (!b || !cur || cur.lineNumber !== p.lineNumber || p.column < end) return null;
      const head = /^[0-9a-f]{4,64}$/.test(b.hash) ? "`" + b.hash + "` " : "";
      return { contents: [{ value: `${head}**${esc(b.author)}**, ${esc(b.when)}\n\n${esc(b.subject)}`, isTrusted: false, supportHtml: false }], range: oneLineAt(p.lineNumber, end, end) };
    },
  });
}
let nextAsk = 0;
const asks = new Map<number, (d: unknown) => void>();

/** COMPLETE_MS: completion asks once typing pauses this long. */
const COMPLETE_MS = 80;
/** ASK_MS: a question the page does not answer in time gets nothing. */
const ASK_MS = 10_000;
/** AI_ASK_MS: the laptop's model may take longer (its own cap is 15 s). */
const AI_ASK_MS = 20_000;
/** GHOST_MS: ghost text asks once typing pauses this long (the laptop's
 * debounce); NES_MS: the next edit after this pause. */
const GHOST_MS = 300;
const NES_MS = 400;
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

/** askAI asks the laptop's AI through the page (the same answer path as
 * ask); null: nothing. */
function askAI(kind: AiKind, p: Pos, manual = false): Promise<unknown> {
  const id = ++nextAsk;
  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      asks.delete(id);
      resolve(null);
    }, AI_ASK_MS);
    asks.set(id, (d) => {
      clearTimeout(timer);
      asks.delete(id);
      resolve(d);
    });
    post({ t: "ai", id, kind, line: p.lineNumber - 1, col: p.column - 1, manual: manual || undefined });
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
      if (!tk.isCancellationRequested && !d?.items?.length) setTimeout(ghostAgain, 0);
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

// The laptop's AI (WEBP-3): ghost text while typing (Tab takes it, Esc
// drops it, Alt+\ asks now) and the next edit suggestion after a pause:
// the rows it would change are marked and the new text shows under them
// (Tab takes it, Esc drops it). The page asks the laptop; the frame only
// shows the answer when the text is still the one asked about.
let aiOn = { inline: false, nes: false };
let aiReady = false;
let nes: { start: number; end: number; lines: string[]; zone: string } | null = null;
let nesDecos: Collection | null = null;
let nesKey: { set(v: boolean): void } | null = null;
let nesTimer: ReturnType<typeof setTimeout> | undefined;

/** aiAt is the selection's rows (a selection that ends at the start of
 * a row does not take that row) and the caret's row, 0-based. */
function aiAt(): AiAt {
  const p = ed?.getPosition();
  const line = p ? p.lineNumber - 1 : 0;
  const r = ed?.getSelections()?.[0];
  if (!r || (r.startLineNumber === r.endLineNumber && r.startColumn === r.endColumn)) return { start: line, end: line, line };
  let end = r.endLineNumber;
  if (r.endColumn === 1 && end > r.startLineNumber) end--;
  return { start: r.startLineNumber - 1, end, line };
}

/** startAI registers the ghost text provider once (it serves this
 * frame's model, and only while the laptop's ai_inline is on, unless the
 * user asks). */
function startAI(): void {
  if (aiReady) return;
  aiReady = true;
  api.languages.registerInlineCompletionsProvider("*", {
    async provideInlineCompletions(m, p, c, tk) {
      const none = { items: [] };
      const manual = c.triggerKind === 1;
      if (m !== model || readOnly || diffEd || (!aiOn.inline && !manual)) return none;
      if (!manual) {
        await sleep(GHOST_MS);
        if (tk.isCancellationRequested) return none;
      }
      const d = (await askAI("complete", p, manual)) as AICompletion | null;
      if (tk.isCancellationRequested || !d || d.none || d.line !== p.lineNumber - 1 || d.to !== p.column - 1 || !d.text) return none;
      const item: InlineItem = { insertText: d.text, range: oneLine(p.lineNumber, d.from + 1, d.to + 1) };
      if (d.imports?.length) item.additionalTextEdits = [{ range: oneLine((d.importAt ?? 0) + 1, 1, 1), text: d.imports.join("\n") + "\n" }];
      return { items: [item] };
    },
    freeInlineCompletions() {},
    disposeInlineCompletions() {},
  });
}

/** ghostAgain asks for ghost text again (still as typing does): while
 * the language server's list was being asked for, the editor held its
 * own ghost text request back, and an empty list does not resume it. */
function ghostAgain(): void {
  if (!aiOn.inline || !ed || readOnly || diffEd) return;
  const c = (ed as unknown as { getContribution(id: string): { model?: { get(): { trigger(): unknown } | undefined } } | null }).getContribution("editor.contrib.inlineCompletionsController");
  void c?.model?.get()?.trigger();
}

/** nesClear takes the shown next edit away. */
function nesClear(): void {
  if (!nes) return;
  const id = nes.zone;
  ed?.changeViewZones((a) => a.removeZone(id));
  nesDecos?.clear();
  nesKey?.set(false);
  nes = null;
}

/** nesShow shows a next edit when its rows still hold the text the model
 * saw. */
function nesShow(d: AINextEdit): void {
  if (!ed || !model || d.none || d.end < d.start || d.end >= model.getLineCount()) return;
  for (let i = d.start; i <= d.end; i++) if (model.getLineContent(i + 1) !== d.old[i - d.start]) return;
  nesClear();
  const box = document.createElement("div");
  box.className = "tc-nes";
  // The new rows in the editor's own font and row height.
  const row0 = document.querySelector<HTMLElement>(".view-lines .view-line");
  if (row0) {
    const cs = getComputedStyle(row0);
    Object.assign(box.style, { fontFamily: cs.fontFamily, fontSize: cs.fontSize, lineHeight: row0.offsetHeight + "px", tabSize: String(tabWidth) });
  }
  for (const l of d.lines) {
    const row = document.createElement("div");
    row.className = "tc-nes-row";
    row.textContent = l || "\u00a0";
    box.append(row);
  }
  const hint = document.createElement("div");
  hint.className = "tc-nes-hint";
  hint.textContent = nesHint;
  box.append(hint);
  let zone = "";
  ed.changeViewZones((a) => (zone = a.addZone({ afterLineNumber: d.end + 1, heightInLines: d.lines.length + 1, domNode: box })));
  nesDecos?.set([{ range: { startLineNumber: d.start + 1, startColumn: 1, endLineNumber: d.end + 1, endColumn: 1 }, options: { isWholeLine: true, className: "tc-nes-old" } }]);
  nes = { start: d.start, end: d.end, lines: d.lines, zone };
  nesKey?.set(true);
}

/** nesTake writes the shown next edit (one undo step). */
function nesTake(): void {
  if (!ed || !model || !nes) return;
  const { start, end, lines } = nes;
  nesClear();
  const range = { startLineNumber: start + 1, startColumn: 1, endLineNumber: end + 1, endColumn: model.getLineMaxColumn(end + 1) };
  ed.pushUndoStop();
  ed.executeEdits("next-edit", [{ range, text: lines.join("\n"), forceMoveMarkers: true }]);
  ed.pushUndoStop();
  ed.setPosition({ lineNumber: start + lines.length, column: 1 });
}

/** nesLater asks for the next edit once typing pauses. */
function nesLater(): void {
  clearTimeout(nesTimer);
  if (!aiOn.nes || readOnly || diffEd) return;
  const at = v;
  nesTimer = setTimeout(() => {
    const p = ed?.getPosition();
    if (!p) return;
    void askAI("next_edit", p).then((d) => {
      if (d && v === at) nesShow(d as AINextEdit);
    });
  }, NES_MS);
}

/** nesHint is the next edit's key hint (the page's words); tabWidth the
 * editor's tab size (the new rows line up with the text). */
let nesHint = "Tab";
let tabWidth = 4;

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

let sqlReady = false;
/** SQL_WORDS are the words the query editor colours as keywords. */
const SQL_WORDS =
  "select from where and or not null is in like between join left right inner outer full cross on as group by order having limit offset distinct union all insert into values update set delete create table view index drop alter add column primary key foreign references default case when then else end exists with asc desc true false count sum avg min max cast returning explain";

/** sqlLang teaches the editor to colour SQL (keywords, strings, numbers, comments). */
function sqlLang(): void {
  if (sqlReady) return;
  sqlReady = true;
  api.languages.register({ id: "sql" });
  api.languages.setMonarchTokensProvider("sql", {
    ignoreCase: true,
    keywords: SQL_WORDS.split(" "),
    tokenizer: {
      root: [
        [/--.*$/, "comment"],
        [/\/\*/, "comment", "@comment"],
        [/'([^']|'')*'/, "string"],
        [/"([^"]|"")*"/, "identifier"],
        [/\d+(\.\d+)?/, "number"],
        [/[a-zA-Z_][\w$]*/, { cases: { "@keywords": "keyword", "@default": "identifier" } }],
        [/[;,.()]/, "delimiter"],
        [/[<>=!*+\-/%|]+/, "operator"],
      ],
      comment: [
        [/[^*/]+/, "comment"],
        [/\*\//, "comment", "@pop"],
        [/[*/]/, "comment"],
      ],
    },
  });
}
let lang = "";

// The workbench keys go to the page; the editor keeps its own (find,
// go to line, multi-cursor …).
function workbenchKey(e: KeyboardEvent): boolean {
  const mod = e.ctrlKey || e.metaKey;
  if (!mod) return false;
  const k = e.key.toLowerCase();
  if (lang === "sql" && k === "enter") return true; // run the query
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
  if (o.tabSize !== undefined && o.tabSize > 0) u.tabSize = tabWidth = o.tabSize;
  if (o.insertSpaces !== undefined) u.insertSpaces = o.insertSpaces;
  if (o.ai) setAI(o.ai);
  ed.updateOptions(u);
}

/** setAI follows the laptop's AI settings; next edits start from the
 * text as it is when they are turned on. */
function setAI(o: NonNullable<FrameOpts["ai"]>): void {
  const wasNes = aiOn.nes;
  aiOn = { inline: o.inline, nes: o.nes };
  if (o.hint) nesHint = o.hint;
  if (!aiReady) return;
  if (!o.nes) {
    clearTimeout(nesTimer);
    nesClear();
  }
  if (o.nes && !wasNes && !readOnly && !diffEd) {
    const p = ed?.getPosition();
    if (p) void askAI("seed", p);
  }
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
    tabSize: (tabWidth = m.tabSize && m.tabSize > 0 ? m.tabSize : 4),
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
    // Breakpoints and tests in the margin left of the row numbers.
    glyphMargin: !m.diff,
  };
  if (m.lang === "sql") {
    sqlLang();
    lang = "sql";
    opts.language = "sql";
  }
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
      renderIndicators: true,
      hideUnchangedRegions: { enabled: m.diff.collapse !== false },
      renderMarginRevertIcon: m.diff.revert === true,
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
  glyphDecos = ed.createDecorationsCollection();
  hintDecos = ed.createDecorationsCollection();
  if (!m.diff) watchGlyphs(ed);
  lineDecos = ed.createDecorationsCollection();
  blameDecos = ed.createDecorationsCollection();
  model?.onDidChangeContent((e) => {
    const specs: { from: number; to: number; insert: string }[] = e.changes.map((c) => ({ from: c.rangeOffset, to: c.rangeOffset + c.rangeLength, insert: c.text }));
    const cs = ChangeSet.of(specs, cur.length);
    const before = cur;
    cur = cs.apply(cur);
    nesClear();
    if (applying === "remote") {
      hist.remote(cs);
      flash(cs);
      return; // the page knows this text
    }
    v++; // a change the page has not seen yet (the laptop's are not counted)
    if (applying === null) hist.user(cs, before, Date.now());
    else if (applying === "replace") hist.user(cs, before, Date.now(), false);
    sendText(true);
    if (applying === null) nesLater(); // after the user's own edit only
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
  if (m.ai && !m.diff && !readOnly) {
    aiOn = { inline: m.ai.inline, nes: m.ai.nes };
    if (m.ai.hint) nesHint = m.ai.hint;
    startAI();
    ed.updateOptions({ inlineSuggest: { enabled: true, showToolbar: "onHover" } });
    nesDecos = ed.createDecorationsCollection();
    nesKey = ed.createContextKey("tcNes", false);
    ed.addCommand(KeyCode.Tab, nesTake, "tcNes && !inlineSuggestionVisible && !suggestWidgetVisible");
    ed.addCommand(KeyCode.Escape, nesClear, "tcNes");
    ed.addCommand(KeyMod.CtrlCmd | KeyCode.KeyI, () => post({ t: "aiAct", kind: "edit", at: aiAt() }));
    if (aiOn.nes) void askAI("seed", { lineNumber: 1, column: 1 });
  }
  ed.onKeyDown((e) => {
    const b = e.browserEvent;
    if (!workbenchKey(b)) return;
    e.preventDefault();
    e.stopPropagation();
    post({ t: "key", key: b.key, code: b.code, ctrl: b.ctrlKey, meta: b.metaKey, shift: b.shiftKey, alt: b.altKey });
  });
  ed.onDidChangeCursorPosition((e) => {
    post({ t: "cursor", line: e.position.lineNumber, col: e.position.column });
    blamePaint();
  });
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
    case "insert": {
      if (!ed || readOnly) return;
      ed.pushUndoStop();
      ed.executeEdits("insert", (ed.getSelections() ?? []).map((range) => ({ range, text: m.text, forceMoveMarkers: true })));
      ed.pushUndoStop();
      ed.focus();
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
    case "lines":
      lineDecos?.set(
        m.items.map((x) => ({
          range: { startLineNumber: x.from, startColumn: 1, endLineNumber: x.to, endColumn: 1 },
          options: { isWholeLine: true, className: "tc-mg tc-mg-" + x.kind, linesDecorationsClassName: "tc-mgg tc-mgg-" + x.kind },
        })),
      );
      return;
    case "blame":
      blameRows = m.rows;
      if (blameRows) startBlameHover();
      blamePaint();
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
    case "glyphs":
      setGlyphs(m.items, m.stop);
      return;
    case "lspRes":
      asks.get(m.id)?.(m.data);
      return;
    case "aiAct":
      if (ed && !diffEd) post({ t: "aiAct", kind: m.kind, at: aiAt() });
      return;
  }
}

/** setGlyphs draws the glyph margin's marks and the stopped row. */
function setGlyphs(items: readonly FrameGlyph[], stop: number): void {
  glyphRows = new Set(items.filter((g) => g.kind.startsWith("bp")).map((g) => g.line));
  const decos: Decoration[] = items.map((g) => ({
    range: { startLineNumber: g.line, startColumn: 1, endLineNumber: g.line, endColumn: 1 },
    options: { glyphMarginClassName: "tc-g tc-g-" + g.kind, glyphMarginHoverMessage: g.title ? { value: g.title } : undefined, stickiness: 1 },
  }));
  if (stop > 0) {
    decos.push({
      range: { startLineNumber: stop, startColumn: 1, endLineNumber: stop, endColumn: 1 },
      options: { isWholeLine: true, className: "tc-stop-line", glyphMarginClassName: "tc-g tc-g-stop", stickiness: 1 },
    });
    if (stop !== lastStop) ed?.revealLineInCenter(stop);
  }
  lastStop = stop;
  glyphDecos?.set(decos);
}

/** watchGlyphs sends clicks in the glyph margin to the page and shows a
 * faint breakpoint under the pointer, as the laptop's editor does. */
function watchGlyphs(e: Editor): void {
  e.onMouseDown((ev) => {
    const line = ev.target.type === GLYPH_MARGIN ? (ev.target.position?.lineNumber ?? 0) : 0;
    glyphMenu = ev.event.rightButton ? line : 0;
    if (line && ev.event.leftButton) post({ t: "glyph", line, menu: false, x: 0, y: 0 });
  });
  e.onMouseMove((ev) => {
    const line = ev.target.type === GLYPH_MARGIN ? (ev.target.position?.lineNumber ?? 0) : 0;
    if (!line || glyphRows.has(line) || readOnly) return hintDecos?.clear();
    hintDecos?.set([{ range: { startLineNumber: line, startColumn: 1, endLineNumber: line, endColumn: 1 }, options: { glyphMarginClassName: "tc-g tc-g-hint" } }]);
  });
  e.onMouseLeave(() => hintDecos?.clear());
}

addEventListener("message", onMessage);
// A newer ghost text request cancels the editor's older one; the editor
// rejects the old promise with "Canceled", which is not an error.
addEventListener("unhandledrejection", (e) => {
  const r: unknown = e.reason;
  if (r instanceof Error && r.name === "Canceled") e.preventDefault();
});
post({ t: "ready" });
