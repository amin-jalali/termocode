// The in-page editor (WEB-39): CodeMirror 6 in a shadow root, so its own
// styles go in as a constructed style sheet (the page's CSP allows no
// <style> tag) and the page's styles cannot leak in. The text follows the
// laptop's buffer (lib/live.ts sends the user's changes and brings the
// laptop's); colours are nvim's (buf.hl), undo is the page's own
// (history.ts). Native selection and touch handles: phone friendly.
import { Annotation, ChangeSet, EditorState, StateEffect, StateField, type Text, type Transaction } from "@codemirror/state";
import { Decoration, type DecorationSet, EditorView, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers } from "@codemirror/view";
import type { EditorHandle, EditorHost, HlRow } from "./api";
import { diff } from "../lib/textdiff";
import { History } from "./history";
import { HL_STYLES } from "./hlstyle";
import { moveKeys } from "./moves";

/** origin marks transactions that are not the user's typing. */
const origin = Annotation.define<"remote" | "undo" | "replace">();

const setHl = StateEffect.define<DecorationSet>();
const flashRows = StateEffect.define<{ from: number; to: number } | null>();

const hlField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(deco, tr) {
    deco = deco.map(tr.changes);
    for (const e of tr.effects) if (e.is(setHl)) deco = e.value;
    return deco;
  },
  provide: (f) => EditorView.decorations.from(f),
});

const flashLine = Decoration.line({ class: "cm-laptop" });

const flashField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(deco, tr) {
    deco = deco.map(tr.changes);
    for (const e of tr.effects) {
      if (!e.is(flashRows)) continue;
      if (!e.value) {
        deco = Decoration.none;
        continue;
      }
      const doc = tr.state.doc;
      const a = doc.lineAt(Math.min(e.value.from, doc.length)).number;
      const b = doc.lineAt(Math.min(e.value.to, doc.length)).number;
      const marks = [];
      for (let n = a; n <= b && n - a < 200; n++) marks.push(flashLine.range(doc.line(n).from));
      deco = Decoration.set(marks);
    }
    return deco;
  },
  provide: (f) => EditorView.decorations.from(f),
});

const theme = EditorView.theme({
  "&": { height: "100%", color: "var(--text)", backgroundColor: "var(--bg-panel)", fontSize: "var(--fs-edit, var(--fs-row))" },
  "&.cm-focused": { outline: "2px solid var(--focus)", outlineOffset: "-2px" },
  ".cm-scroller": { fontFamily: "var(--font-mono)", lineHeight: "var(--lh-row)", overflow: "auto" },
  ".cm-content": { caretColor: "var(--text)", padding: "var(--s2) 0" },
  ".cm-line": { padding: "0 var(--s3)" },
  ".cm-gutters": { backgroundColor: "var(--bg-2)", color: "var(--dim)", borderRight: "1px solid var(--border)" },
  ".cm-activeLine": { backgroundColor: "var(--bg-hover)" },
  ".cm-activeLineGutter": { backgroundColor: "var(--bg-hover)", color: "var(--text-2)" },
  ".cm-line.cm-laptop": { backgroundColor: "var(--sel)" },
  "::selection": { backgroundColor: "var(--sel)" },
  ...HL_STYLES,
});

export function mount(parent: HTMLElement, host: EditorHost): EditorHandle {
  const shadow = parent.shadowRoot ?? parent.attachShadow({ mode: "open" });
  const box = document.createElement("div");
  box.className = "editor-box";
  box.style.height = "100%";
  shadow.replaceChildren(box);
  const hist = new History();
  let flashTimer: ReturnType<typeof setTimeout> | undefined;

  const record = (tr: Transaction) => {
    if (!tr.docChanged) return;
    const o = tr.annotation(origin);
    if (o === "remote") hist.remote(tr.changes);
    else if (o === undefined) hist.user(tr.changes, tr.startState.doc, Date.now());
    else if (o === "replace") hist.user(tr.changes, tr.startState.doc, Date.now(), false);
  };

  const run = (f: (doc: Text) => ChangeSet | null): boolean => {
    const c = f(view.state.doc);
    if (!c) return false;
    view.dispatch({ changes: c, annotations: origin.of("undo"), scrollIntoView: true, selection: { anchor: Math.min(c.mapPos(view.state.selection.main.head, 1), c.newLength) } });
    return true;
  };

  const indent = (v: EditorView) => {
    v.dispatch(v.state.replaceSelection(v.state.doc.toString().includes("\n\t") ? "\t" : "    "));
    return true;
  };

  const view: EditorView = new EditorView({
    root: shadow,
    parent: box,
    state: EditorState.create({
      doc: host.doc,
      extensions: [
        lineNumbers(),
        highlightActiveLine(),
        highlightActiveLineGutter(),
        EditorView.lineWrapping,
        EditorState.tabSize.of(4),
        hlField,
        flashField,
        theme,
        EditorView.contentAttributes.of({ autocapitalize: "off", autocorrect: "off", spellcheck: "false", "aria-label": host.label, dir: "ltr" }),
        keymap.of([
          { key: "Mod-z", run: () => run((d) => hist.undo(d)), preventDefault: true },
          { key: "Mod-y", run: () => run((d) => hist.redo(d)), preventDefault: true },
          { key: "Shift-Mod-z", run: () => run((d) => hist.redo(d)), preventDefault: true },
          { key: "Mod-s", run: () => (host.onSave(), true), preventDefault: true },
          { key: "Escape", run: () => (host.onDone(), true) },
          { key: "Tab", run: indent },
          ...moveKeys,
        ]),
        EditorView.updateListener.of((u) => {
          for (const tr of u.transactions) record(tr);
          if (u.docChanged && u.transactions.some((tr) => tr.docChanged && tr.annotation(origin) !== "remote")) host.onChange();
          if (u.viewportChanged || u.docChanged) {
            const d = u.state.doc;
            host.onViewport(d.lineAt(u.view.viewport.from).number - 1, d.lineAt(u.view.viewport.to).number);
          }
        }),
      ],
    }),
  });
  queueMicrotask(() => {
    const d = view.state.doc;
    host.onViewport(d.lineAt(view.viewport.from).number - 1, d.lineAt(view.viewport.to).number);
  });

  return {
    text: () => view.state.doc.toString(),
    remote(text) {
      const c = diff(view.state.doc.toString(), text);
      if (!c) return;
      view.dispatch({ changes: c, annotations: origin.of("remote") });
      view.dispatch({ effects: flashRows.of({ from: c.from, to: c.from + c.insert.length }) });
      clearTimeout(flashTimer);
      flashTimer = setTimeout(() => view.dispatch({ effects: flashRows.of(null) }), 1500);
    },
    replace(text) {
      const c = diff(view.state.doc.toString(), text);
      if (c) view.dispatch({ changes: c, annotations: origin.of("replace") });
    },
    setHl(rows: readonly HlRow[]) {
      const doc = view.state.doc;
      const marks = [];
      for (const r of rows) {
        if (r.row >= doc.lines) continue;
        const line = doc.line(r.row + 1);
        if (line.text !== r.text) continue;
        for (const s of r.segs) if (s.cls && s.to > s.from) marks.push(Decoration.mark({ class: "hl-" + s.cls }).range(line.from + s.from, line.from + Math.min(s.to, line.length)));
      }
      view.dispatch({ effects: setHl.of(Decoration.set(marks, true)) });
    },
    undo: () => run((d) => hist.undo(d)),
    redo: () => run((d) => hist.redo(d)),
    canUndo: () => hist.canUndo(),
    canRedo: () => hist.canRedo(),
    focus: () => view.focus(),
    destroy() {
      clearTimeout(flashTimer);
      view.destroy();
      shadow.replaceChildren();
    },
  };
}
