// term.js: the terminal engine of the phone's and the desktop's terminal
// (xterm.js, vendored), built alone and loaded only when a terminal
// opens (lib/xterm.ts). It imports no value of the page: the page gives
// it the rows to show, the look, and takes what is typed (onData, the
// bytes as a terminal sends them: keys, the phone keyboard's typing and
// its input methods, paste).
//
// The laptop's shell keeps its own width; the page sends its rows
// joined where the laptop wrapped them (state/term.ts reflow) and the
// engine wraps them again at its own width. show() writes only what
// changed: when the old lines are a prefix of the new ones (output came,
// the prompt line changed) it rewrites from the first changed line; else
// it draws everything again and keeps the reader's place.
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

/** XTheme is the engine's colours (its ITheme, the part the page sets). */
export interface XTheme {
  background?: string;
  foreground?: string;
  cursor?: string;
  cursorAccent?: string;
  selectionBackground?: string;
  extendedAnsi?: string[];
}

/** XLook is a run's look (lib/termcolors.ts Look). */
export interface XLook {
  color?: string;
  background?: string;
  fontWeight?: string;
  fontStyle?: string;
  textDecoration?: string;
}

/** XLine is one line: its first buffer row, its text, its coloured runs. */
export interface XLine {
  row: number;
  text: string;
  segs?: { text: string; look?: XLook }[] | null;
}

export interface XOpts {
  fontFamily: string;
  fontSize: number;
  lineHeight?: number;
  theme: XTheme;
  /** label is the input's name for a screen reader. */
  label: string;
  /** onData gets what was typed, as bytes for the shell. */
  onData: (data: string) => void;
  /** onPaste gets a paste of more than one line (the page asks first); none: it is typed. */
  onPaste?: (text: string) => void;
  /** onScroll says whether the view is at the end. */
  onScroll?: (atEnd: boolean) => void;
}

/** XTerm is one terminal on the page. */
export interface XTerm {
  /** show draws the lines; cursor: show the cursor after the last line's text. */
  show(lines: readonly XLine[], cursor: boolean): void;
  focus(): void;
  /** setInputMode sets the input's inputmode ("none": an on-screen keyboard of the page types). */
  setInputMode(mode: "text" | "none"): void;
  setLook(o: Pick<XOpts, "fontFamily" | "fontSize" | "lineHeight" | "theme">): void;
  fit(): void;
  atEnd(): boolean;
  toEnd(): void;
  /** lineAt is the text of the line shown at a page y (a long press). */
  lineAt(clientY: number): string;
  selection(): string;
  /** text is every line the terminal holds. */
  text(): string;
  readonly input: HTMLTextAreaElement | undefined;
  readonly cols: number;
  dispose(): void;
}

const HEX = /^#[0-9a-f]{6}$/i;

/** Palette gives each colour of the laptop's screen an entry of the
 * engine's 256 colours (16–255, theme.extendedAnsi): the engine then
 * paints with classes, never with a style attribute, which the page's
 * policy (style-src 'self') refuses. Past 240 colours the oldest entries
 * are used again. */
class Palette {
  ext: string[] = [];
  at = new Map<string, number>();
  changed = false;
  index(hex: string): number {
    const k = hex.toLowerCase();
    let n = this.at.get(k);
    if (n === undefined) {
      n = this.ext.length < 240 ? this.ext.length : this.at.size % 240;
      for (const [c, i] of this.at) if (i === n) this.at.delete(c);
      this.ext[n] = k;
      this.at.set(k, n);
      this.changed = true;
    }
    return 16 + n;
  }
}

function color(p: Palette, hex: string | undefined, base: number): string {
  return hex && HEX.test(hex) ? `;${base};5;${p.index(hex)}` : "";
}

/** sgr is a look as an SGR sequence. */
function sgr(p: Palette, l: XLook): string {
  let s = "0" + color(p, l.color, 38) + color(p, l.background, 48);
  if (l.fontWeight === "700") s += ";1";
  if (l.fontStyle === "italic") s += ";3";
  if (l.textDecoration?.includes("underline")) s += ";4";
  if (l.textDecoration?.includes("line-through")) s += ";9";
  return `\x1b[${s}m`;
}

/** clean drops control characters from a row: rows are text, never sequences. */
function clean(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/[\x00-\x1f\x7f]/g, " ");
}

/** encode is a line as what the engine is written. */
function encode(p: Palette, l: XLine): string {
  if (!l.segs) return clean(l.text);
  let out = "";
  for (const g of l.segs) out += g.look ? sgr(p, g.look) + clean(g.text) + "\x1b[0m" : clean(g.text);
  return out;
}

const SCROLLBACK = 10000;

/** cspStyles lets the engine's own style blocks work under the page's
 * policy (style-src 'self', no inline styles): a <style> it makes is an
 * inert element whose text goes to a constructed style sheet the
 * document adopts (constructed sheets are not inline styles). Only the
 * engine makes style elements on this page. */
function cspStyles(doc: Document): void {
  const w = doc as Document & { termStyles?: boolean };
  if (w.termStyles || typeof CSSStyleSheet === "undefined" || !("adoptedStyleSheets" in doc)) return;
  w.termStyles = true;
  const make = doc.createElement.bind(doc) as (tag: string, o?: ElementCreationOptions) => HTMLElement;
  (doc as { createElement: unknown }).createElement = (tag: string, o?: ElementCreationOptions) => {
    if (String(tag).toLowerCase() !== "style") return make(tag, o);
    const el = make("termocode-style", o);
    el.hidden = true;
    const sheet = new CSSStyleSheet();
    let text = "";
    doc.adoptedStyleSheets = [...doc.adoptedStyleSheets, sheet];
    Object.defineProperty(el, "textContent", {
      get: () => text,
      set: (v: string | null) => {
        text = v ?? "";
        sheet.replaceSync(text);
      },
    });
    const drop = el.remove.bind(el);
    el.remove = () => {
      doc.adoptedStyleSheets = doc.adoptedStyleSheets.filter((s) => s !== sheet);
      drop();
    };
    return el;
  };
}

export function open(el: HTMLElement, o: XOpts): XTerm {
  cspStyles(el.ownerDocument);
  const pal = new Palette();
  let theme = o.theme;
  const term = new Terminal({
    fontFamily: o.fontFamily,
    fontSize: o.fontSize,
    lineHeight: o.lineHeight ?? 1.2,
    theme,
    scrollback: SCROLLBACK,
    cursorBlink: true,
    convertEol: false,
    allowProposedApi: false,
    screenReaderMode: false,
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(el);
  const ta = term.textarea;
  if (ta) {
    ta.setAttribute("aria-label", o.label);
    ta.setAttribute("autocorrect", "off");
    ta.setAttribute("autocapitalize", "off");
    ta.setAttribute("autocomplete", "off");
    ta.setAttribute("spellcheck", "false");
    ta.setAttribute("enterkeyhint", "enter");
  }
  term.onData((d) => o.onData(d));
  const onPaste = (e: ClipboardEvent) => {
    const text = e.clipboardData?.getData("text/plain") ?? "";
    if (!o.onPaste || !/\r|\n/.test(text.replace(/(\r\n?|\n)$/, ""))) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    o.onPaste(text);
  };
  el.addEventListener("paste", onPaste, true);
  const atEnd = () => term.buffer.active.viewportY >= term.buffer.active.baseY;
  term.onScroll(() => o.onScroll?.(atEnd()));
  // viewport scroll by the reader (wheel, touch) also moves the end mark
  const vp = el.querySelector(".xterm-viewport, .xterm-scrollable-element");
  const onVP = () => o.onScroll?.(atEnd());
  vp?.addEventListener("scroll", onVP, { passive: true });
  // A finger drags the rows: older rows come in (the engine's own
  // scrolling is a mouse wheel's). A long press stays the page's.
  let ty: number | null = null;
  let acc = 0;
  const onStart = (e: TouchEvent) => {
    ty = e.touches.length === 1 ? e.touches[0].clientY : null;
    acc = 0;
  };
  const onMove = (e: TouchEvent) => {
    if (ty === null || e.touches.length !== 1) return;
    const y = e.touches[0].clientY;
    acc += ty - y;
    ty = y;
    const cell = Math.max(1, (el.querySelector(".xterm-screen")?.clientHeight ?? el.clientHeight) / Math.max(1, term.rows));
    const n = Math.trunc(acc / cell);
    if (n !== 0) {
      term.scrollLines(n);
      acc -= n * cell;
    }
    if (e.cancelable) e.preventDefault();
  };
  el.addEventListener("touchstart", onStart, { passive: true, capture: true });
  el.addEventListener("touchmove", onMove, { passive: false, capture: true });
  let ro: ResizeObserver | null = null;
  const refit = () => {
    if (el.clientWidth > 0 && el.clientHeight > 0) fit.fit();
  };
  if (typeof ResizeObserver !== "undefined") {
    ro = new ResizeObserver(refit);
    ro.observe(el);
  }
  refit();

  let prev: { row: number; enc: string }[] = [];
  let starts: number[] = [];
  let shownCursor = true;
  const abs = () => term.buffer.active.baseY + term.buffer.active.cursorY;

  const writeFrom = (lines: { row: number; enc: string }[], from: number) => {
    for (let i = from; i < lines.length; i++) {
      term.write(i === 0 ? "" : "\r\n", () => (starts[i] = abs()));
      term.write(lines[i].enc);
    }
  };

  const show = (lines: readonly XLine[], cursor: boolean) => {
    let end = lines.length;
    while (end > 1 && lines[end - 1].text.trim() === "") end--; // the cursor sits after the last text
    const next = lines.slice(0, end).map((l) => ({ row: l.row, enc: encode(pal, l) }));
    if (pal.changed) {
      pal.changed = false;
      term.options.theme = { ...theme, extendedAnsi: pal.ext };
    }
    if (cursor !== shownCursor) {
      term.write(cursor ? "\x1b[?25h" : "\x1b[?25l");
      shownCursor = cursor;
    }
    const off = next.length ? prev.findIndex((p) => p.row === next[0].row) : -1;
    let d = 0;
    if (off >= 0) {
      while (d < next.length && d + off < prev.length && prev[d + off].row === next[d].row && prev[d + off].enc === next[d].enc) d++;
      if (d === next.length && d + off === prev.length) return; // nothing new
    }
    const b = term.buffer.active;
    const start = off >= 0 && d < next.length ? starts[d + off] : undefined;
    if (off >= 0 && d + off <= prev.length && d > 0 && start !== undefined && start >= b.baseY) {
      starts = starts.slice(off);
      prev = next;
      // Back to the first changed line, clear below, write the rest.
      term.write(`\x1b[${start - b.baseY + 1};1H\x1b[J`);
      writeFrom(next, d);
      return;
    }
    if (off >= 0 && d + off === prev.length && d > 0 && d < next.length) {
      // Only new lines below the old ones.
      starts = starts.slice(off);
      prev = next;
      writeFrom(next, d);
      return;
    }
    const keepY = atEnd() ? -1 : b.viewportY;
    term.reset();
    if (!cursor) term.write("\x1b[?25l");
    starts = [];
    prev = next;
    writeFrom(next, 0);
    term.write("", () => (keepY >= 0 ? term.scrollToLine(keepY) : term.scrollToBottom()));
  };

  const lineText = (y: number) => {
    const b = term.buffer.active;
    let top = y;
    while (top > 0 && b.getLine(top)?.isWrapped) top--;
    let s = "";
    for (let i = top; i < b.length; i++) {
      const l = b.getLine(i);
      if (!l || (i > top && !l.isWrapped)) break;
      s += l.translateToString(true);
    }
    return s;
  };

  return {
    show,
    focus: () => term.focus(),
    setInputMode: (mode) => term.textarea?.setAttribute("inputmode", mode),
    setLook: (l) => {
      term.options.fontFamily = l.fontFamily;
      term.options.fontSize = l.fontSize;
      if (l.lineHeight) term.options.lineHeight = l.lineHeight;
      theme = l.theme;
      term.options.theme = { ...theme, extendedAnsi: pal.ext };
      refit();
    },
    fit: refit,
    atEnd,
    toEnd: () => term.scrollToBottom(),
    lineAt: (clientY) => {
      const screen = el.querySelector(".xterm-screen") ?? el;
      const r = screen.getBoundingClientRect();
      const y = Math.floor(((clientY - r.top) / Math.max(1, r.height)) * term.rows);
      return lineText(term.buffer.active.viewportY + Math.max(0, Math.min(term.rows - 1, y)));
    },
    selection: () => term.getSelection(),
    text: () => {
      const b = term.buffer.active;
      const out: string[] = [];
      for (let i = 0; i < b.length; i++) {
        const l = b.getLine(i);
        if (!l) continue;
        if (l.isWrapped && out.length) out[out.length - 1] += l.translateToString(true);
        else out.push(l.translateToString(true));
      }
      while (out.length && out[out.length - 1] === "") out.pop();
      return out.join("\n");
    },
    get input() {
      return term.textarea;
    },
    get cols() {
      return term.cols;
    },
    dispose: () => {
      ro?.disconnect();
      el.removeEventListener("paste", onPaste, true);
      el.removeEventListener("touchstart", onStart, true);
      el.removeEventListener("touchmove", onMove, true);
      vp?.removeEventListener("scroll", onVP);
      term.dispose();
    },
  };
}
