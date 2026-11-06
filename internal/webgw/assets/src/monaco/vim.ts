// Vim mode of the desktop editor (WEBP-1): a small keymap of our own over
// the frame's text (no library). Normal, insert, visual and visual-line
// modes; counts; the motions h j k l w b e W B E 0 ^ $ gg G f F t T { };
// the operators d c y (with a motion, doubled for whole lines, or the
// text objects iw aw); x X D C s S Y p P J r ~ i a I A o O; u and Ctrl+R;
// named registers ("a … "z). Every change goes through the host as one
// edit, so the page's undo and the laptop's buffer see it like typing.
// Not here (yet): macros, marks, search (/ ? n), the . repeat, ex
// commands (:w; Ctrl/Cmd+S saves).
import type { Text } from "@codemirror/state";

export type VimMode = "normal" | "insert" | "visual" | "vline";

/** VimHost is what the keymap needs of the editor; offsets index the
 * text (UTF-16), as the frame's own Text does. */
export interface VimHost {
  doc(): Text;
  caret(): number;
  /** place puts the caret at off with no selection. */
  place(off: number): void;
  /** select shows a selection from anchor to head (head is the caret). */
  select(anchor: number, head: number): void;
  /** edit replaces [from, to) with insert as one user change and puts
   * the caret at caret (an offset in the new text). */
  edit(from: number, to: number, insert: string, caret: number): void;
  undo(): void;
  redo(): void;
  readOnly(): boolean;
  /** show tells the page the mode and the keys typed so far. */
  show(mode: VimMode, pending: string): void;
}

/** VimKey is the part of a keydown the keymap reads. */
export interface VimKey {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
}

interface Reg {
  text: string;
  line: boolean;
}

interface Range {
  from: number;
  to: number;
  line: boolean;
}

/** Motion is where a motion goes: to, and whether an operator over it
 * takes whole lines or the char at to too. */
interface Motion {
  to: number;
  line?: boolean;
  incl?: boolean;
}

const MOTIONS = new Set(["h", "j", "k", "l", "w", "b", "e", "W", "B", "E", "0", "^", "$", "G", "gg", "{", "}", " ", "Backspace", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End"]);
const CHAR_MOTIONS = new Set(["f", "F", "t", "T"]);

type Kind = 0 | 1 | 2; // blank, word, punctuation

function kindOf(c: string, big: boolean): Kind {
  if (c === "" || /\s/.test(c)) return 0;
  if (big) return 1;
  return /[\p{L}\p{N}_]/u.test(c) ? 1 : 2;
}

export class Vim {
  mode: VimMode = "normal";
  private pending = "";
  private regs = new Map<string, Reg>();
  private anchor = 0;
  /** vhead is the visual mode's head: the char it is on. */
  private vhead = 0;
  /** want is the column j and k keep to (-1: the line's end). */
  private want: number | null = null;

  constructor(private readonly h: VimHost) {
    this.normal();
  }

  /** key handles one keydown: true when Vim took it (the editor must
   * not see it). */
  key(e: VimKey): boolean {
    if (e.metaKey || e.altKey) return false;
    const k = e.key;
    if (this.mode === "insert") {
      if (k === "Escape" || (e.ctrlKey && k === "[")) {
        const at = this.pos();
        const ls = this.lineStart(at);
        this.mode = "normal";
        this.h.place(Math.max(ls, at - 1));
        this.normal();
        return true;
      }
      return false;
    }
    if (e.ctrlKey) {
      if (k === "[") return this.escape();
      if (k === "r" && this.mode === "normal") {
        this.pending = "";
        this.h.redo();
        this.after();
        return true;
      }
      return false; // the workbench's keys, the editor's own
    }
    if (k === "Escape") return this.escape();
    if (k === "Shift" || k === "Control" || k === "Alt" || k === "Meta" || k === "CapsLock") return false;
    if (k.length > 1 && !MOTIONS.has(k) && k !== "Enter" && k !== "Delete" && k !== "Tab") return false; // F-keys, PageUp …
    this.pending += k === "Enter" ? "\n" : k === "Delete" ? "\x7f" : k === "Tab" ? "" : k.length > 1 ? "\x00" + k + "\x00" : k;
    this.run();
    return true;
  }

  private escape(): boolean {
    this.pending = "";
    if (this.mode === "visual" || this.mode === "vline") {
      const at = this.vhead;
      this.mode = "normal";
      this.h.place(this.clamp(at));
    }
    this.normal();
    return true;
  }

  /** normal clamps the caret onto a char and shows the mode. */
  private normal(): void {
    if (this.mode === "normal") {
      const at = this.pos();
      const c = this.clamp(at);
      if (c !== at) this.h.place(c);
    }
    this.h.show(this.mode, this.pending);
  }

  private after(): void {
    this.normal();
  }

  /** pos is where the commands work from: the caret, or in visual mode
   * the char the selection's head is on. */
  private pos(): number {
    return this.mode === "visual" || this.mode === "vline" ? this.vhead : this.h.caret();
  }

  // ---- text helpers ----

  private get d(): Text {
    return this.h.doc();
  }

  private lineStart(at: number): number {
    return this.d.lineAt(Math.min(at, this.d.length)).from;
  }

  private lineEnd(at: number): number {
    return this.d.lineAt(Math.min(at, this.d.length)).to;
  }

  /** clamp: in normal mode the caret sits on a char, not after the last. */
  private clamp(at: number): number {
    const l = this.d.lineAt(Math.min(at, this.d.length));
    return l.to > l.from ? Math.min(Math.max(at, l.from), l.to - 1) : l.from;
  }

  private ch(at: number): string {
    return at >= 0 && at < this.d.length ? this.d.sliceString(at, at + 1) : "";
  }

  private firstNonBlank(lineNo: number): number {
    const l = this.d.line(lineNo);
    const m = /^\s*/.exec(l.text);
    return l.from + Math.min(m ? m[0].length : 0, Math.max(0, l.length - 1));
  }

  private lineCol(at: number): { n: number; col: number } {
    const l = this.d.lineAt(at);
    return { n: l.number, col: at - l.from };
  }

  // ---- parsing ----

  private run(): void {
    const p = this.pending;
    const m = /^(?:"([a-z"]))?([1-9]\d*)?(.*)$/s.exec(p);
    if (!m) return this.reset();
    const reg = m[1] ?? '"';
    if (p.startsWith('"') && m[1] === undefined) {
      if (p.length < 2) return this.h.show(this.mode, p);
      return this.reset();
    }
    const n1 = m[2] ? parseInt(m[2], 10) : 0;
    const rest = m[3];
    if (rest === "") return this.h.show(this.mode, p);
    const done = this.command(reg, n1, rest);
    if (done === "more") return this.h.show(this.mode, p);
    this.pending = "";
    this.after();
  }

  private reset(): void {
    this.pending = "";
    this.after();
  }

  /** command runs rest (after register and count); "more": wait for keys. */
  private command(reg: string, n1: number, rest: string): "more" | "done" {
    const visual = this.mode === "visual" || this.mode === "vline";
    const count = Math.max(1, n1);
    const c0 = rest[0];
    // Motions alone.
    const mv = this.parseMotion(rest, n1);
    if (mv === "more") return "more";
    if (mv) {
      this.moveTo(mv.m, rest);
      return "done";
    }
    if (visual) return this.visualCommand(reg, rest);
    switch (c0) {
      case "i":
      case "a":
      case "I":
      case "A":
      case "o":
      case "O":
        return this.insertCommand(c0), "done";
      case "v":
      case "V":
        this.anchor = this.pos();
        this.mode = c0 === "v" ? "visual" : "vline";
        this.paintVisual(this.anchor);
        return "done";
      case "u":
        for (let i = 0; i < count; i++) this.h.undo();
        return "done";
      case "x":
      case "\x7f":
        return this.operate("d", reg, this.charRange(count, 1)), "done";
      case "X":
        return this.operate("d", reg, this.charRange(count, -1)), "done";
      case "D":
        return this.operate("d", reg, this.toLineEnd(count)), "done";
      case "C":
        return this.operate("c", reg, this.toLineEnd(count)), "done";
      case "s":
        return this.operate("c", reg, this.charRange(count, 1)), "done";
      case "S":
        return this.operate("c", reg, this.lines(count)), "done";
      case "Y":
        return this.operate("y", reg, this.lines(count)), "done";
      case "p":
      case "P":
        return this.put(reg, c0 === "P", count), "done";
      case "J":
        return this.join(Math.max(2, count)), "done";
      case "~":
        return this.toggleCase(count), "done";
      case "r":
        if (rest.length < 2) return "more";
        return this.replaceChars(rest[1], count), "done";
      case "d":
      case "c":
      case "y":
        return this.operator(c0, reg, n1, rest.slice(1));
      case "g":
        return rest.length < 2 ? "more" : "done";
    }
    return "done"; // unknown: dropped
  }

  /** parseMotion reads a motion at the start of s (with the count before
   * it): null when s is no motion. */
  private parseMotion(s: string, n: number): { m: Motion; len: number } | "more" | null {
    if (s === "g") return "more";
    let name = s[0];
    let len = 1;
    if (s.startsWith("gg")) {
      name = "gg";
      len = 2;
    } else if (s[0] === "\x00") {
      const end = s.indexOf("\x00", 1);
      if (end < 0) return "more";
      name = s.slice(1, end);
      len = end + 1;
    } else if (s[0] === "\n") name = "Enter";
    if (CHAR_MOTIONS.has(name)) {
      if (s.length < 2) return "more";
      const m = this.findChar(name, s[1], Math.max(1, n));
      return m ? { m, len: 2 } : { m: { to: this.pos() }, len: 2 };
    }
    if (name === "Enter") return { m: this.vertical(Math.max(1, n), true), len: 1 };
    if (!MOTIONS.has(name)) return null;
    return { m: this.motion(name, n), len };
  }

  private motion(name: string, n: number): Motion {
    const at = this.pos();
    const count = Math.max(1, n);
    const d = this.d;
    switch (name) {
      case "h":
      case "Backspace":
      case "ArrowLeft":
        return { to: Math.max(this.lineStart(at), at - count) };
      case "l":
      case " ":
      case "ArrowRight": {
        const end = this.lineEnd(at);
        return { to: Math.min(end, at + count) };
      }
      case "j":
      case "ArrowDown":
        return this.vertical(count, false);
      case "k":
      case "ArrowUp":
        return this.vertical(-count, false);
      case "0":
      case "Home":
        return { to: this.lineStart(at) };
      case "^":
        return { to: this.firstNonBlank(d.lineAt(at).number) };
      case "$":
      case "End": {
        const ln = Math.min(d.lines, d.lineAt(at).number + count - 1);
        const l = d.line(ln);
        this.want = -1;
        return { to: Math.max(l.from, l.to - 1), incl: true };
      }
      case "G":
      case "gg": {
        const ln = n > 0 ? Math.min(n, d.lines) : name === "G" ? d.lines : 1;
        return { to: this.firstNonBlank(ln), line: true };
      }
      case "w":
      case "W":
        return { to: this.wordForward(at, count, name === "W") };
      case "b":
      case "B":
        return { to: this.wordBack(at, count, name === "B") };
      case "e":
      case "E":
        return { to: this.wordEnd(at, count, name === "E"), incl: true };
      case "}":
      case "{":
        return { to: this.paragraph(at, count, name === "}") };
    }
    return { to: at };
  }

  private vertical(dn: number, first: boolean): Motion {
    const d = this.d;
    const at = this.pos();
    const { n, col } = this.lineCol(at);
    const ln = Math.min(d.lines, Math.max(1, n + dn));
    if (this.want === null) this.want = col;
    const l = d.line(ln);
    if (first) return { to: this.firstNonBlank(ln), line: true };
    const w = this.want < 0 ? l.length : this.want;
    return { to: l.from + Math.min(w, Math.max(0, l.length - (this.mode === "insert" ? 0 : 1))), line: true };
  }

  private wordForward(at: number, count: number, big: boolean): number {
    const d = this.d;
    let p = at;
    for (let i = 0; i < count; i++) {
      const k = kindOf(this.ch(p), big);
      // Over this word, then over blanks (an empty line is a word).
      if (k !== 0) while (p < d.length && kindOf(this.ch(p), big) === k) p++;
      while (p < d.length) {
        const c = this.ch(p);
        if (c === "\n") {
          p++;
          if (this.ch(p) === "\n" || p >= d.length) break; // an empty line stops
          continue;
        }
        if (kindOf(c, big) !== 0) break;
        p++;
      }
    }
    return Math.min(p, d.length);
  }

  private wordBack(at: number, count: number, big: boolean): number {
    let p = at;
    for (let i = 0; i < count; i++) {
      if (p > 0) p--;
      while (p > 0 && kindOf(this.ch(p), big) === 0 && !(this.ch(p) === "\n" && this.ch(p - 1) === "\n")) p--;
      const k = kindOf(this.ch(p), big);
      if (k !== 0) while (p > 0 && kindOf(this.ch(p - 1), big) === k) p--;
    }
    return p;
  }

  private wordEnd(at: number, count: number, big: boolean): number {
    const d = this.d;
    let p = at;
    for (let i = 0; i < count; i++) {
      if (p < d.length - 1) p++;
      while (p < d.length - 1 && kindOf(this.ch(p), big) === 0) p++;
      const k = kindOf(this.ch(p), big);
      while (p < d.length - 1 && kindOf(this.ch(p + 1), big) === k && k !== 0) p++;
    }
    return p;
  }

  private paragraph(at: number, count: number, down: boolean): number {
    const d = this.d;
    let ln = d.lineAt(at).number;
    for (let i = 0; i < count; i++) {
      // Skip blank lines, then go to the next blank one.
      while (ln > 1 && ln < d.lines && d.line(ln).length === 0) ln += down ? 1 : -1;
      while (ln >= 1 && ln <= d.lines && d.line(ln).length > 0) ln += down ? 1 : -1;
      ln = Math.min(d.lines, Math.max(1, ln));
    }
    return d.line(ln).from;
  }

  private findChar(kind: string, c: string, count: number): Motion | null {
    const at = this.pos();
    const l = this.d.lineAt(at);
    const text = l.text;
    let col = at - l.from;
    const fwd = kind === "f" || kind === "t";
    for (let i = 0; i < count; i++) {
      const from = fwd ? col + 1 + (kind === "t" && i === 0 ? 1 : 0) : col - 1 - (kind === "T" && i === 0 ? 1 : 0);
      const j = fwd ? text.indexOf(c, from) : from >= 0 ? text.lastIndexOf(c, from) : -1;
      if (j < 0) return null;
      col = j;
    }
    if (kind === "t") col--;
    if (kind === "T") col++;
    return { to: l.from + col, incl: fwd };
  }

  // ---- moving ----

  private moveTo(m: Motion, name: string): void {
    if (!/^[jk]|^\x00Arrow(Up|Down)|^\n/.test(name) && !name.startsWith("$")) this.want = null;
    if (this.mode === "visual" || this.mode === "vline") return this.paintVisual(m.to);
    this.h.place(this.clamp(m.to));
  }

  private paintVisual(head: number): void {
    this.vhead = head;
    const a = this.anchor;
    if (this.mode === "vline") {
      const [lo, hi] = a <= head ? [a, head] : [head, a];
      const from = this.lineStart(lo), to = this.lineEnd(hi);
      if (a <= head) this.h.select(from, to);
      else this.h.select(to, from);
      return;
    }
    // Inclusive: the char under the caret is in the selection.
    if (head >= a) this.h.select(a, Math.min(this.d.length, head + 1));
    else this.h.select(Math.min(this.d.length, a + 1), head);
  }

  /** visualRange is what visual mode covers now. */
  private visualRange(): Range {
    const lo = Math.min(this.anchor, this.vhead), hi = Math.max(this.anchor, this.vhead);
    if (this.mode === "vline") return this.lineRange(this.d.lineAt(lo).number, this.d.lineAt(hi).number);
    return { from: lo, to: Math.min(this.d.length, hi + 1), line: false };
  }

  private visualCommand(reg: string, rest: string): "more" | "done" {
    const c = rest[0];
    if (c === "v" || c === "V") {
      const want: VimMode = c === "v" ? "visual" : "vline";
      if (this.mode === want) {
        this.mode = "normal";
        this.h.place(this.clamp(this.headChar()));
      } else {
        this.mode = want;
        this.paintVisual(this.headChar());
      }
      return "done";
    }
    if (c === "o") {
      const head = this.headChar();
      const a = this.anchor;
      this.anchor = head;
      this.paintVisual(a);
      return "done";
    }
    const r = this.visualRange();
    const done = (): "done" => ((this.mode = "normal"), "done");
    switch (c) {
      case "d":
      case "x":
      case "\x7f":
        done();
        this.operate("d", reg, r);
        return "done";
      case "c":
      case "s":
        done();
        this.operate("c", reg, r);
        return "done";
      case "y":
        done();
        this.operate("y", reg, r);
        return "done";
      case "~":
        done();
        this.caseRange(r);
        return "done";
      case "J": {
        done();
        const lines = this.d.lineAt(Math.max(r.from, r.to - 1)).number - this.d.lineAt(r.from).number + 1;
        this.h.place(r.from);
        this.join(Math.max(2, lines));
        return "done";
      }
    }
    return "done"; // anything else: still visual
  }

  /** headChar is the char the visual caret is on (the host's caret is
   * after it when the selection runs forward). */
  private headChar(): number {
    return this.vhead;
  }

  // ---- operators ----

  private operator(op: "d" | "c" | "y", reg: string, n1: number, rest: string): "more" | "done" {
    if (rest === "") return "more";
    const m = /^([1-9]\d*)?(.*)$/s.exec(rest);
    const n2 = m && m[1] ? parseInt(m[1], 10) : 0;
    const tail = m ? m[2] : rest;
    if (tail === "") return "more";
    const n = Math.max(1, n1) * Math.max(1, n2);
    if (tail[0] === op) return this.operate(op, reg, this.lines(n)), "done";
    if (tail[0] === "i" || tail[0] === "a") {
      if (tail.length < 2) return "more";
      const r = this.textObject(tail[0] === "a", tail[1]);
      if (r) this.operate(op, reg, r);
      return "done";
    }
    // cw on a word changes to its end, as in Vim.
    const at = this.pos();
    const big = tail[0] === "W";
    if (op === "c" && (tail[0] === "w" || big) && kindOf(this.ch(at), big) !== 0) {
      const same = kindOf(this.ch(at + 1), big) === kindOf(this.ch(at), big);
      const end = same ? this.wordEnd(at, n, big) : n > 1 ? this.wordEnd(at, n - 1, big) : at;
      return this.operate("c", reg, { from: at, to: end + 1, line: false }), "done";
    }
    const pm = this.parseMotion(tail, n1 || n2 ? n : 0);
    if (pm === "more") return "more";
    if (!pm) return "done";
    const mv = pm.m;
    if (mv.line) {
      const a = this.d.lineAt(at).number, b = this.d.lineAt(Math.min(mv.to, this.d.length)).number;
      return this.operate(op, reg, this.lineRange(Math.min(a, b), Math.max(a, b))), "done";
    }
    let from = Math.min(at, mv.to), to = Math.max(at, mv.to);
    if (mv.incl) to = Math.min(this.d.length, to + 1);
    // dw at a line's last word stays on that line.
    if ((tail[0] === "w" || tail[0] === "W") && to > from) {
      const le = this.lineEnd(from);
      if (to > le) to = le === from ? Math.min(this.d.length, le + 1) : le;
    }
    if (to > from) this.operate(op, reg, { from, to, line: false });
    return "done";
  }

  private textObject(around: boolean, c: string): Range | null {
    if (c !== "w" && c !== "W") return null;
    const big = c === "W";
    const at = this.pos();
    const k = kindOf(this.ch(at), big);
    let from = at, to = at;
    while (from > this.lineStart(at) && kindOf(this.ch(from - 1), big) === k) from--;
    while (to < this.lineEnd(at) && kindOf(this.ch(to), big) === k) to++;
    if (around) while (to < this.lineEnd(at) && kindOf(this.ch(to), big) === 0) to++;
    return to > from ? { from, to, line: false } : null;
  }

  private charRange(count: number, dir: 1 | -1): Range {
    const at = this.pos();
    if (dir > 0) return { from: at, to: Math.min(this.lineEnd(at), at + count), line: false };
    return { from: Math.max(this.lineStart(at), at - count), to: at, line: false };
  }

  private toLineEnd(count: number): Range {
    const at = this.pos();
    const ln = Math.min(this.d.lines, this.d.lineAt(at).number + count - 1);
    return { from: at, to: this.d.line(ln).to, line: false };
  }

  /** lines are count whole lines from the caret's. */
  private lines(count: number): Range {
    const a = this.d.lineAt(this.pos()).number;
    return this.lineRange(a, Math.min(this.d.lines, a + count - 1));
  }

  private lineRange(a: number, b: number): Range {
    return { from: this.d.line(a).from, to: this.d.line(b).to, line: true };
  }

  private setReg(reg: string, r: Reg): void {
    this.regs.set('"', r);
    if (reg !== '"') this.regs.set(reg, r);
  }

  /** operate runs d, c or y over r. */
  private operate(op: "d" | "c" | "y", reg: string, r: Range): void {
    const d = this.d;
    const text = d.sliceString(r.from, r.to);
    if (r.to <= r.from && !r.line) {
      if (op === "c") this.insertAt(r.from);
      return;
    }
    this.setReg(reg, { text: r.line ? text + "\n" : text, line: r.line });
    if (op === "y") {
      this.h.place(r.line ? this.firstNonBlank(d.lineAt(r.from).number) : r.from);
      return;
    }
    if (this.h.readOnly()) return;
    if (!r.line) {
      this.h.edit(r.from, r.to, "", r.from);
      if (op === "c") this.insertAt(r.from);
      return;
    }
    if (op === "c") {
      // cc keeps the first line's indent and the line itself.
      const ind = /^\s*/.exec(d.lineAt(r.from).text)?.[0] ?? "";
      this.h.edit(r.from, r.to, ind, r.from + ind.length);
      this.insertAt(r.from + ind.length);
      return;
    }
    // dd: the lines and one line end go.
    let from = r.from, to = r.to;
    if (to < d.length) to++;
    else if (from > 0) from--;
    this.h.edit(from, to, "", from);
    const nd = this.d;
    const ln = nd.lineAt(Math.min(from === r.from ? from : from + 1, nd.length)).number;
    this.h.place(this.firstNonBlank(Math.min(ln, nd.lines)));
  }

  private insertAt(at: number): void {
    this.mode = "insert";
    this.want = null;
    this.h.place(at);
  }

  private insertCommand(c: string): void {
    const at = this.pos();
    const d = this.d;
    const l = d.lineAt(at);
    switch (c) {
      case "i":
        return this.insertAt(at);
      case "a":
        return this.insertAt(Math.min(l.to, at + (l.length > 0 ? 1 : 0)));
      case "I":
        return this.insertAt(l.from + (/^\s*/.exec(l.text)?.[0].length ?? 0));
      case "A":
        return this.insertAt(l.to);
      case "o":
      case "O": {
        if (this.h.readOnly()) return;
        const ind = /^\s*/.exec(l.text)?.[0] ?? "";
        if (c === "o") {
          this.h.edit(l.to, l.to, "\n" + ind, l.to + 1 + ind.length);
          return this.insertAt(l.to + 1 + ind.length);
        }
        this.h.edit(l.from, l.from, ind + "\n", l.from + ind.length);
        return this.insertAt(l.from + ind.length);
      }
    }
  }

  private put(reg: string, before: boolean, count: number): void {
    const r = this.regs.get(reg);
    if (!r || this.h.readOnly()) return;
    const at = this.pos();
    const d = this.d;
    const l = d.lineAt(at);
    if (r.line) {
      const body = r.text.repeat(count);
      if (before) {
        this.h.edit(l.from, l.from, body, l.from);
        this.h.place(this.firstNonBlank(l.number));
      } else if (l.to < d.length) {
        this.h.edit(l.to + 1, l.to + 1, body, l.to + 1);
        this.h.place(this.firstNonBlank(l.number + 1));
      } else {
        // After the last line: a line end first, none at the end.
        this.h.edit(l.to, l.to, "\n" + body.slice(0, -1), l.to + 1);
        this.h.place(this.firstNonBlank(l.number + 1));
      }
      return;
    }
    const pos = before || l.length === 0 ? at : at + 1;
    const body = r.text.repeat(count);
    this.h.edit(pos, pos, body, pos + body.length - 1);
  }

  private join(lines: number): void {
    if (this.h.readOnly()) return;
    const d = this.d;
    const first = d.lineAt(this.pos()).number;
    const last = Math.min(d.lines, first + lines - 1);
    if (last === first) return;
    let out = d.line(first).text;
    let caret = 0;
    for (let n = first + 1; n <= last; n++) {
      const t = d.line(n).text.replace(/^\s+/, "");
      caret = out.replace(/\s+$/, "").length;
      out = out.replace(/\s+$/, "") + (t === "" || t.startsWith(")") ? "" : " ") + t;
    }
    const from = d.line(first).from;
    this.h.edit(from, d.line(last).to, out, from + caret);
  }

  private toggleCase(count: number): void {
    if (this.h.readOnly()) return;
    const at = this.pos();
    const to = Math.min(this.lineEnd(at), at + count);
    if (to <= at) return;
    const s = this.d.sliceString(at, to);
    this.h.edit(at, to, swapCase(s), Math.min(to, this.lineEnd(at) - 1));
  }

  private caseRange(r: Range): void {
    if (this.h.readOnly() || r.to <= r.from) return;
    this.h.edit(r.from, r.to, swapCase(this.d.sliceString(r.from, r.to)), r.from);
  }

  private replaceChars(c: string, count: number): void {
    if (this.h.readOnly() || c === "\x00") return;
    const at = this.pos();
    if (at + count > this.lineEnd(at)) return;
    const ins = c === "\n" ? "\n" : c.repeat(count);
    this.h.edit(at, at + count, ins, c === "\n" ? at + 1 : at + count - 1);
  }
}

function swapCase(s: string): string {
  let o = "";
  for (const ch of s) {
    const u = ch.toUpperCase();
    o += ch === u ? ch.toLowerCase() : u;
  }
  return o;
}
