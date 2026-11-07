// Vim mode of the desktop editor (WEBP-1): a keymap of our own over the
// frame's text (no library), in its own file (monaco-vim.js) that the
// frame loads only when Vim mode is on. Normal, insert, visual,
// visual-line and visual-block (Ctrl+V: a box of columns; d y c r ~ on
// it, I and A type on every line of it, p puts a yanked box) modes; counts; the motions h j k l w b e W B E 0 ^ $ gg G
// f F t T ; , { }; the operators d c y (with a motion, doubled for whole
// lines, or the text objects iw aw); x X D C s S Y p P J r ~ i a I A o O;
// u and Ctrl+R; the . repeat; macros (q, @, @@); the registers a-z (A-Z
// appends), the unnamed one, 0 (the last yank), 1-9 (deletes of lines),
// - (small deletes), _ (the black hole) and . (the last insert; Ctrl+R
// in insert mode puts a register); search (/ ? n N * #); ex commands
// (:N, :s, :d, :y, :j, :> :<, :normal, :w, :q, the :map family); marks;
// text objects (words, quotes, brackets, paragraphs); the user's own
// mappings (the laptop editor's, and :map made here). Every change goes through the host
// as one edit, so the page's undo and the laptop's buffer see it like
// typing.
import type { Text } from "@codemirror/state";

export type VimMode = "normal" | "insert" | "visual" | "vline" | "vblock";

/** VimHost is what the keymap needs of the editor; offsets index the
 * text (UTF-16), as the frame's own Text does. */
export interface VimHost {
  doc(): Text;
  caret(): number;
  /** place puts the caret at off with no selection. */
  place(off: number): void;
  /** select shows a selection from anchor to head (head is the caret). */
  select(anchor: number, head: number): void;
  /** selectBlock shows one selection per line of a visual block (from,
   * to on each line) and keeps the line of head on screen. */
  selectBlock(sels: readonly { from: number; to: number }[], head: number): void;
  /** edit replaces [from, to) with insert as one user change and puts
   * the caret at caret (an offset in the new text). */
  edit(from: number, to: number, insert: string, caret: number): void;
  undo(): void;
  redo(): void;
  readOnly(): boolean;
  /** typeKey types one key (a token: "a", "<CR>", "<BS>" …) as the
   * user's own typing in insert mode (a macro, the . repeat). */
  typeKey(tok: string): void;
  /** show tells the page the mode, the keys typed so far, the register
   * a macro is being recorded into ("": none) and a message ("": none;
   * else its code, a NUL and its argument: "notfound\x00foo"). */
  show(mode: VimMode, pending: string, rec: string, msg: string): void;
  /** save writes the file (:w); close closes its tab (:q). */
  save(): void;
  close(): void;
  /** indent is one level of indent (four spaces, a tab). */
  indent(): string;
  /** view is the first and last line on screen (1-based). */
  view(): { top: number; bottom: number };
  /** reveal scrolls the line of off to the middle, top or bottom. */
  reveal(off: number, where: "center" | "top" | "bottom"): void;
  /** copied: text went into the + or * register (the page's clipboard
   * history keeps it; the system clipboard is never read). */
  copied(text: string): void;
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
  /** block: a visual block's yank, one line of text per line of it. */
  block?: boolean;
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

const MOTIONS = new Set(["h", "j", "k", "l", "w", "b", "e", "W", "B", "E", "ge", "gE", "0", "^", "$", "_", "-", "+", "|", "G", "gg", "{", "}", "%", "H", "M", "L", "n", "N", "*", "#", ";", ",", "S", " ", "Backspace", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End"]);
const CHAR_MOTIONS = new Set(["f", "F", "t", "T"]);
/** JUMPS are the motions that set the ' mark (where the caret was). */
const JUMPS = new Set(["G", "gg", "%", "H", "M", "L", "n", "N", "*", "#", "S", "{", "}"]);
const BRACKETS: Record<string, [string, string]> = { "(": ["(", ")"], ")": ["(", ")"], b: ["(", ")"], "[": ["[", "]"], "]": ["[", "]"], "{": ["{", "}"], "}": ["{", "}"], B: ["{", "}"], "<": ["<", ">"], ">": ["<", ">"] };

type Op = "d" | "c" | "y" | ">" | "<" | "~" | "u" | "U";

/** vimRegex makes a JavaScript pattern of a Vim one ("magic": ( ) | + ?
 * { } are plain chars unless escaped; \< \> are word edges; \c ignores
 * case). null: not a valid pattern. */
export function vimRegex(pat: string, global: boolean): RegExp | null {
  let flags = global ? "gm" : "m";
  let out = "";
  for (let i = 0; i < pat.length; i++) {
    const c = pat[i];
    if (c === "\\" && i + 1 < pat.length) {
      const n = pat[++i];
      if (n === "c") flags += flags.includes("i") ? "" : "i";
      else if (n === "C") continue;
      else if (n === "<" || n === ">") out += "\\b";
      else if ("()|+?{}".includes(n)) out += n;
      else if (n === "=") out += "?";
      else out += "\\" + n;
    } else if ("()|+?{}".includes(c)) out += "\\" + c;
    else out += c;
  }
  try {
    return new RegExp(out, flags);
  } catch {
    return null;
  }
}

/** STEP_LIMIT stops a macro that calls itself forever. */
const STEP_LIMIT = 20000;

const KEY_TOKENS: Record<string, string> = {
  Escape: "<Esc>",
  Enter: "<CR>",
  Backspace: "<BS>",
  Delete: "<Del>",
  Tab: "<Tab>",
  ArrowLeft: "<Left>",
  ArrowRight: "<Right>",
  ArrowUp: "<Up>",
  ArrowDown: "<Down>",
  Home: "<Home>",
  End: "<End>",
};

/** tokOf names a keydown as one token: the char it types, or a special
 * key in the editor's notation ("<CR>", "<C-r>"); null: not Vim's. */
export function tokOf(e: VimKey): string | null {
  if (e.metaKey || e.altKey) return null;
  const k = e.key;
  if (e.ctrlKey) return k.length === 1 ? "<C-" + k.toLowerCase() + ">" : null;
  if (k.length === 1) return k;
  return KEY_TOKENS[k] ?? null;
}

/** PIECES are the special tokens as the command parser reads them. */
const PIECES: Record<string, string> = {
  "<CR>": "\n",
  "<Del>": "\x7f",
  "<Tab>": "",
  "<BS>": "\x00Backspace\x00",
  "<Left>": "\x00ArrowLeft\x00",
  "<Right>": "\x00ArrowRight\x00",
  "<Up>": "\x00ArrowUp\x00",
  "<Down>": "\x00ArrowDown\x00",
  "<Home>": "\x00Home\x00",
  "<End>": "\x00End\x00",
};

/** VimMapDef is one mapping as the page gives it (the laptop editor's
 * notation: "<Space>w", ":w<CR>"); mode n, x, o or i. */
export interface VimMapDef {
  mode: string;
  lhs: string;
  rhs: string;
  noremap?: boolean;
}

interface VimMapping {
  lhs: string[];
  rhs: string[];
  noremap: boolean;
}

const NAMED_KEYS: Record<string, string> = {
  space: " ",
  lt: "<",
  bar: "|",
  bslash: "\\",
  cr: "<CR>",
  enter: "<CR>",
  return: "<CR>",
  nl: "<CR>",
  esc: "<Esc>",
  bs: "<BS>",
  backspace: "<BS>",
  del: "<Del>",
  delete: "<Del>",
  tab: "<Tab>",
  up: "<Up>",
  down: "<Down>",
  left: "<Left>",
  right: "<Right>",
  home: "<Home>",
  end: "<End>",
  nop: "",
  cmd: "<Cmd>",
};

/** parseKeys reads keys in the editor's notation as tokens; null: it
 * names a key the browser never gives the editor (Alt, F-keys, the
 * mouse) or a script's name (<Plug>, <SID>), so the mapping is left out. */
export function parseKeys(s: string, leader = "\\"): string[] | null {
  const out: string[] = [];
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    const j = c === "<" ? s.indexOf(">", i + 2) : -1;
    if (j > 0) {
      const name = s.slice(i + 1, j);
      const low = name.toLowerCase();
      if (low === "leader" || low === "localleader") {
        out.push(...[...leader].map((ch) => (ch === " " ? " " : ch)));
        i = j;
        continue;
      }
      if (low in NAMED_KEYS) {
        if (NAMED_KEYS[low]) out.push(NAMED_KEYS[low]);
        i = j;
        continue;
      }
      const ctrl = /^c-(.)$/i.exec(name);
      if (ctrl) {
        out.push(ctrl[1] === "[" ? "<Esc>" : "<C-" + ctrl[1].toLowerCase() + ">");
        i = j;
        continue;
      }
      if (/^(?:[msadt]-|f\d|plug|sid|snr|sni|.*mouse|.*drag|.*release|scroll)/i.test(name)) return null;
    }
    out.push(c);
  }
  return out;
}

/** keysText writes tokens as a register's text (a recorded macro). */
function keysText(toks: readonly string[]): string {
  return toks.map((t) => (t === "<CR>" ? "\n" : t === "<Esc>" ? "\x1b" : t === "<BS>" ? "\b" : t === "<Tab>" ? "\t" : t)).join("");
}

/** textKeys reads a register's text as keys (@a on yanked text). */
function textKeys(s: string): string[] {
  const out: string[] = [];
  for (const ch of s) out.push(ch === "\n" ? "<CR>" : ch === "\x1b" ? "<Esc>" : ch === "\b" ? "<BS>" : ch === "\t" ? "<Tab>" : ch);
  return out;
}

/** insertedText is the text a run of insert-mode keys typed. */
function insertedText(toks: readonly string[]): string {
  let out = "";
  for (const t of toks) {
    if (t === "<CR>") out += "\n";
    else if (t === "<BS>") out = out.slice(0, -1);
    else if (t === "<Tab>") out += "\t";
    else if (t.length === 1) out += t;
  }
  return out;
}

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
  /** dotRec gathers the keys of the change being made (the . repeat);
   * lastDot is the last finished one. */
  private dotRec: string[] | null = null;
  private lastDot: string[] = [];
  private dotReplay = false;
  /** changed: the command running now changed the text. */
  private changed = false;
  /** insKeys are the keys typed since insert mode began; insRepeat how
   * many more times they are typed at Escape (2ia). */
  private insKeys: string[] = [];
  private insRepeat = 0;
  /** insReg: Ctrl+R in insert mode waits for a register's name. */
  private insReg = false;
  /** rec is the register a macro is being recorded into ("": none). */
  private rec = "";
  private recAppend = false;
  private recKeys: string[] = [];
  private macros = new Map<string, string[]>();
  private lastMacro = "";
  private steps = 0;
  private depth = 0;
  /** cl is the command line being typed (/ ? :), after the keys of
   * the command it belongs to (prefix: d in d/foo). */
  private cl: { kind: "/" | "?" | ":"; text: string; prefix: string } | null = null;
  private search: { pat: string; back: boolean } | null = null;
  private lastFind: { kind: string; c: string } | null = null;
  private lastEx = "";
  private marks = new Map<string, { line: number; col: number }>();
  /** lastVisual is the last visual selection (gv). */
  private lastVisual: { anchor: number; head: number; mode: VimMode } | null = null;
  /** vdollar: the visual block runs to each line's end ($). */
  private vdollar = false;
  /** blockIns: an I, A or c of a visual block types on line lo at col;
   * at Escape the same text goes on lines lo+1..hi (end: at each line's
   * end; pad: short lines are padded up to col). len0 is line lo's
   * length before the typing. */
  private blockIns: { lo: number; hi: number; col: number; end: boolean; pad: boolean; len0: number; lines: number } | null = null;
  private msg = "";
  private lastSub: { rep: string } | null = null;
  /** maps are the user's mappings by mode (n x o i): the page's (the
   * laptop editor's) and the ones :map made here. */
  private maps = new Map<string, VimMapping[]>();
  private mapBuf: string[] = [];
  private mapTimer: ReturnType<typeof setTimeout> | null = null;
  private timeout = 1000;
  private leader = "\\";

  constructor(private readonly h: VimHost) {
    this.normal();
  }

  /** key handles one keydown: true when Vim took it (the editor must
   * not see it). */
  key(e: VimKey): boolean {
    const tok = tokOf(e);
    if (tok === null) return false;
    if (this.rec) this.recKeys.push(tok);
    this.steps = 0;
    if (this.msg) {
      this.msg = "";
      this.show();
    }
    return this.typed(tok, true);
  }

  /** setMaps takes the page's mappings (the laptop editor's); timeout is
   * how long a key that starts a longer mapping waits (0: forever). */
  setMaps(defs: readonly VimMapDef[], timeout: number): void {
    for (const k of this.maps.keys()) this.maps.set(k, []);
    for (const d of defs) this.addMap(d.mode, d.lhs, d.rhs, d.noremap === true);
    if (timeout >= 0) this.timeout = timeout;
  }

  private addMap(mode: string, lhs: string, rhs: string, noremap: boolean): void {
    const l = parseKeys(lhs, this.leader), r = parseKeys(rhs, this.leader);
    if (!l || !r || l.length === 0 || !"nxoi".includes(mode) || mode.length !== 1) return;
    const list = (this.maps.get(mode) ?? []).filter((m) => !same(m.lhs, l));
    list.push({ lhs: l, rhs: r, noremap });
    this.maps.set(mode, list);
  }

  private unmap(mode: string, lhs: string): boolean {
    const l = parseKeys(lhs, this.leader);
    const list = this.maps.get(mode);
    if (!l || !list) return false;
    const left = list.filter((m) => !same(m.lhs, l));
    this.maps.set(mode, left);
    return left.length < list.length;
  }

  /** mapMode is the mode mappings are looked up in now (null: none; a
   * char the command waits for is never mapped). */
  private mapMode(): string | null {
    if (this.cl || this.insReg) return null;
    if (this.mode === "insert") return "i";
    if (this.awaitingChar()) return null;
    if (this.inVisual()) return "x";
    return this.pendingOp() ? "o" : "n";
  }

  /** typed runs a typed token through the user's mappings: a key that
   * starts a longer mapping waits for the next (or the timeout). */
  private typed(tok: string, live: boolean): boolean {
    const mode = this.mapMode();
    const list = mode ? this.maps.get(mode) : undefined;
    if (!list || list.length === 0) {
      if (this.mapBuf.length === 0) return this.exec(tok, live);
    }
    this.stopTimer();
    const buf = [...this.mapBuf, tok];
    const maps = list ?? [];
    const exact = maps.find((m) => same(m.lhs, buf));
    if (maps.some((m) => m.lhs.length > buf.length && same(m.lhs.slice(0, buf.length), buf))) {
      this.mapBuf = buf;
      if (this.timeout > 0) this.mapTimer = setTimeout(() => this.flushMaps(), this.timeout);
      this.show();
      return true;
    }
    this.mapBuf = [];
    if (exact) {
      this.runMap(exact);
      return true;
    }
    if (buf.length === 1) return this.exec(tok, live);
    // No mapping: the first key as itself, the others looked up again.
    this.exec(buf[0], false);
    for (const t of buf.slice(1)) this.typed(t, false);
    this.show();
    return true;
  }

  private stopTimer(): void {
    if (this.mapTimer !== null) clearTimeout(this.mapTimer);
    this.mapTimer = null;
  }

  /** flushMaps: the wait ran out; the keys so far run (as their
   * mapping if they are one). */
  private flushMaps(): void {
    this.mapTimer = null;
    const buf = this.mapBuf;
    this.mapBuf = [];
    if (buf.length === 0) return;
    this.steps = 0;
    const mode = this.mapMode();
    const exact = mode ? this.maps.get(mode)?.find((m) => same(m.lhs, buf)) : undefined;
    if (exact) this.runMap(exact);
    else {
      this.exec(buf[0], false);
      for (const t of buf.slice(1)) this.typed(t, false);
    }
    this.show();
  }

  private runMap(m: VimMapping): void {
    if (this.depth > 50) return;
    this.depth++;
    try {
      // A right side that starts with its own keys does not map them
      // again (nmap < <gv), as in Vim.
      const own = !m.noremap && same(m.rhs.slice(0, m.lhs.length), m.lhs) ? m.lhs.length : 0;
      m.rhs.forEach((t, i) => {
        if (this.steps > STEP_LIMIT) return;
        if (m.noremap || i < own) this.exec(t, false);
        else this.typed(t, false);
      });
    } finally {
      this.depth--;
    }
  }

  /** exec runs one token; live: it is the keydown being handled now (an
   * insert-mode key the editor then types itself), else Vim types it. */
  private exec(tok: string, live: boolean): boolean {
    if (++this.steps > STEP_LIMIT) return true;
    const took = this.handle(tok);
    if (!took && !live && this.mode === "insert") this.h.typeKey(tok);
    return took;
  }

  /** play runs tokens as if typed (a macro, the . repeat); remap: the
   * user's mappings apply (a macro, :normal; not the . repeat). */
  private play(toks: readonly string[], remap = false): void {
    if (this.depth > 50) return;
    this.depth++;
    try {
      for (const t of toks) {
        if (this.steps > STEP_LIMIT) break;
        if (remap) this.typed(t, false);
        else this.exec(t, false);
      }
    } finally {
      this.depth--;
    }
  }

  private handle(tok: string): boolean {
    if (this.cl) return this.clKey(tok);
    if (tok === "<Cmd>") {
      // A mapping's <Cmd>…<CR>: an ex command that keeps the mode.
      this.cl = { kind: ":", text: "", prefix: "" };
      return true;
    }
    if (this.mode === "insert") return this.insertKey(tok);
    if (tok === "<Esc>" || tok === "<C-[>") return this.escape();
    if (tok.startsWith("<C-")) return this.ctrlKey(tok);
    if ((tok === "/" || tok === "?" || tok === ":") && !this.awaitingChar()) {
      if (this.pending === "" && this.mode === "normal" && !this.dotReplay) {
        this.dotRec = [];
        this.changed = false;
      }
      if (!this.dotReplay) this.dotRec?.push(tok);
      let text = "";
      if (tok === ":" && this.inVisual()) {
        this.leaveVisual();
        text = "'<,'>";
      } else if (tok === ":" && /^[1-9]\d*$/.test(this.pending)) {
        const n = parseInt(this.pending, 10);
        this.pending = "";
        text = n > 1 ? ".,.+" + (n - 1) : ".";
      }
      this.cl = { kind: tok, text, prefix: tok === ":" ? "" : this.pending };
      this.show();
      return true;
    }
    const piece = tok.length === 1 ? tok : PIECES[tok];
    if (piece === undefined) return false; // PageUp …: the editor's
    if (this.pending === "" && this.mode === "normal" && !this.dotReplay) {
      this.dotRec = [];
      this.changed = false;
    }
    if (!this.dotReplay) this.dotRec?.push(tok);
    this.pending += piece;
    this.run();
    return true;
  }

  private insertKey(tok: string): boolean {
    if (this.insReg) {
      this.insReg = false;
      const r = tok.length === 1 ? this.getReg(tok) : undefined;
      if (r) for (const t of textKeys(r.text)) this.typeIn(t);
      return true;
    }
    if (tok === "<Esc>" || tok === "<C-[>") {
      this.leaveInsert();
      return true;
    }
    if (tok === "<C-r>") {
      this.insReg = true;
      return true;
    }
    if (tok.startsWith("<C-")) return false; // the workbench's keys
    this.insKeys.push(tok);
    if (!this.dotReplay) this.dotRec?.push(tok);
    return false;
  }

  /** typeIn types a key in insert mode as if the user typed it. */
  private typeIn(tok: string): void {
    this.insKeys.push(tok);
    if (!this.dotReplay) this.dotRec?.push(tok);
    this.h.typeKey(tok);
  }

  private leaveInsert(): void {
    this.blockInsert();
    const keys = this.insKeys;
    for (let i = 0; i < this.insRepeat; i++) for (const t of keys) this.h.typeKey(t);
    this.insRepeat = 0;
    this.regs.set(".", { text: insertedText(keys), line: false });
    if (this.dotRec && !this.dotReplay) {
      this.dotRec.push("<Esc>");
      this.lastDot = this.dotRec;
    }
    this.dotRec = null;
    this.changed = false;
    const at = this.h.caret();
    const ls = this.lineStart(at);
    this.mode = "normal";
    this.h.place(Math.max(ls, at - 1));
    this.normal();
  }

  private escape(): boolean {
    this.pending = "";
    if (this.inVisual()) this.leaveVisual();
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
    this.show();
  }

  private after(): void {
    this.normal();
  }

  private show(): void {
    const p = this.cl ? this.cl.prefix.replace(/\x00[^\x00]*\x00/g, "") + this.cl.kind + this.cl.text : this.pending + this.mapBuf.filter((t) => t.length === 1).join("");
    this.h.show(this.mode, p, this.rec, this.msg);
  }

  private say(code: string, arg: string): void {
    this.msg = code + "\x00" + arg;
  }

  /** awaitingChar: the next key is a plain char of the command (f, r,
   * m, ", @ …), never a command or a mapping of its own. */
  awaitingChar(): boolean {
    let p = this.pending;
    if (p === '"') return true;
    if (p.startsWith('"')) p = p.slice(2);
    p = p.replace(/^[1-9]\d*/, "");
    const op = /^(?:[dcy<>]|g[~uU])/.exec(p);
    if (op) p = p.slice(op[0].length).replace(/^[1-9]\d*/, "");
    if (op || this.inVisual()) {
      if (p === "i" || p === "a") return true;
    }
    return ["f", "F", "t", "T", "r", "m", "'", "`", "q", "@", "gr"].includes(p) && !(p === "q" && this.rec !== "" && !op);
  }

  /** pendingOp: an operator waits for its motion (operator-pending mode). */
  pendingOp(): boolean {
    let p = this.pending;
    if (p.startsWith('"')) p = p.slice(2);
    return /^[1-9]?\d*(?:[dcy<>]|g[~uU])[1-9]?\d*$/.test(p);
  }

  private ctrlKey(tok: string): boolean {
    const visual = this.inVisual();
    const n = /^[1-9]\d*$/.test(this.pending) ? parseInt(this.pending, 10) : 0;
    switch (tok) {
      case "<C-v>":
        this.pending = "";
        if (this.mode === "vblock") this.leaveVisual();
        else {
          const head = this.pos();
          if (!visual) this.anchor = head;
          this.mode = "vblock";
          this.vdollar = false;
          this.paintVisual(head);
        }
        this.after();
        return true;
      case "<C-r>":
        if (visual) return false;
        this.pending = "";
        for (let i = 0; i < Math.max(1, n); i++) this.h.redo();
        this.after();
        return true;
      case "<C-d>":
      case "<C-u>": {
        const v = this.h.view();
        const half = Math.max(1, Math.floor((v.bottom - v.top + 1) / 2));
        const ln = this.d.lineAt(this.pos()).number + (tok === "<C-d>" ? half : -half);
        const to = this.firstNonBlank(Math.min(this.d.lines, Math.max(1, ln)));
        this.pending = "";
        this.moveTo({ to }, "");
        this.h.reveal(to, "center");
        this.after();
        return true;
      }
      case "<C-a>":
      case "<C-x>":
        if (visual) return false;
        this.pending = "";
        if (!this.dotReplay) this.dotRec = [...(n > 0 ? String(n) : ""), tok];
        this.changed = false;
        this.addNumber((tok === "<C-a>" ? 1 : -1) * Math.max(1, n));
        this.endCommand();
        this.after();
        return true;
    }
    return false; // the workbench's keys, the editor's own
  }

  /** addNumber adds n to the number at or after the caret on its line. */
  private addNumber(n: number): void {
    if (this.h.readOnly()) return;
    const at = this.pos();
    const l = this.d.lineAt(at);
    const re = /-?\d+/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(l.text))) {
      const end = l.from + m.index + m[0].length;
      if (end > at) {
        const from = l.from + m.index;
        const s = String(parseInt(m[0], 10) + n);
        this.edit(from, end, s, from + s.length - 1);
        return;
      }
    }
  }

  private clKey(tok: string): boolean {
    const cl = this.cl;
    if (!cl) return false;
    if (!this.dotReplay && cl.kind !== ":") this.dotRec?.push(tok);
    if (tok === "<Esc>" || tok === "<C-[>" || tok === "<C-c>" || (tok === "<BS>" && cl.text === "")) {
      this.cl = null;
      this.reset();
      return true;
    }
    if (tok === "<CR>") {
      this.cl = null;
      if (cl.kind === ":") {
        if (!this.dotReplay && this.mode === "normal") this.dotRec = null;
        this.pending = "";
        this.ex(cl.text);
        this.after();
        return true;
      }
      const pat = cl.text || this.search?.pat || "";
      if (!pat) return this.reset(), true;
      this.search = { pat, back: cl.kind === "?" };
      this.regs.set("/", { text: pat, line: false });
      this.pending = cl.prefix + "\x00S\x00";
      this.run();
      return true;
    }
    if (tok === "<BS>") cl.text = cl.text.slice(0, -1);
    else if (tok === "<C-u>") cl.text = "";
    else if (tok.length === 1) cl.text += tok;
    this.show();
    return true;
  }

  private leaveVisual(): void {
    const lo = Math.min(this.anchor, this.vhead), hi = Math.max(this.anchor, this.vhead);
    this.lastVisual = { anchor: this.anchor, head: this.vhead, mode: this.mode };
    this.vdollar = false;
    this.setMark("<", lo);
    this.setMark(">", hi);
    this.mode = "normal";
    this.h.place(this.clamp(this.vhead));
  }

  private setMark(c: string, at: number): void {
    const { n, col } = this.lineCol(Math.min(at, this.d.length));
    this.marks.set(c, { line: n, col });
  }

  /** markAt is where mark c is now (null: not set, or its line is gone). */
  private markAt(c: string): number | null {
    const m = this.marks.get(c === "`" ? "'" : c);
    if (!m || m.line > this.d.lines) return null;
    const l = this.d.line(m.line);
    return l.from + Math.min(m.col, Math.max(0, l.length - 1));
  }

  /** pos is where the commands work from: the caret, or in visual mode
   * the char the selection's head is on. */
  private inVisual(): boolean {
    return this.mode === "visual" || this.mode === "vline" || this.mode === "vblock";
  }

  private pos(): number {
    return this.inVisual() ? this.vhead : this.h.caret();
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
    const m = /^(?:"([a-zA-Z0-9"_+*.:/-]))?([1-9]\d*)?(.*)$/s.exec(p);
    if (!m) return this.reset();
    const reg = m[1] ?? '"';
    if (p.startsWith('"') && m[1] === undefined) {
      if (p.length < 2) return this.show();
      return this.reset();
    }
    const n1 = m[2] ? parseInt(m[2], 10) : 0;
    const rest = m[3];
    if (rest === "") return this.show();
    const done = this.command(reg, n1, rest);
    if (done === "more") return this.show();
    this.pending = "";
    this.endCommand();
    this.after();
  }

  private reset(): void {
    this.pending = "";
    this.endCommand();
    this.after();
  }

  /** endCommand keeps a finished change for the . repeat (a change that
   * went on into insert mode is kept at Escape). */
  private endCommand(): void {
    if (this.dotReplay || this.mode === "insert") return;
    if (this.changed && this.dotRec) this.lastDot = this.dotRec;
    this.dotRec = null;
    this.changed = false;
  }

  /** edit is the host's edit, noted as a change for the . repeat. */
  private edit(from: number, to: number, insert: string, caret: number): void {
    this.changed = true;
    this.h.edit(from, to, insert, caret);
  }

  /** command runs rest (after register and count); "more": wait for keys. */
  private command(reg: string, n1: number, rest: string): "more" | "done" {
    const visual = this.inVisual();
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
        this.insertCommand(c0);
        if (this.mode === "insert") this.insRepeat = count - 1;
        return "done";
      case ".":
        return this.repeat(n1), "done";
      case "q":
        if (this.rec) return this.stopRecording(), "done";
        if (rest.length < 2) return "more";
        if (/^[a-zA-Z0-9"]$/.test(rest[1])) {
          this.rec = rest[1].toLowerCase();
          this.recAppend = rest[1] !== this.rec;
          this.recKeys = [];
        }
        return "done";
      case "@":
        if (rest.length < 2) return "more";
        return this.playMacro(rest[1], count), "done";
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
      case ">":
      case "<":
        return this.operator(c0, reg, n1, rest.slice(1));
      case "m":
        if (rest.length < 2) return "more";
        if (/^[a-zA-Z'`]$/.test(rest[1])) this.setMark(rest[1] === "`" ? "'" : rest[1], this.pos());
        return "done";
      case "z":
        if (rest.length < 2) return "more";
        if (rest[1] === "z" || rest[1] === "." ) this.h.reveal(this.pos(), "center");
        else if (rest[1] === "t" || rest[1] === "\n") this.h.reveal(this.pos(), "top");
        else if (rest[1] === "b" || rest[1] === "-") this.h.reveal(this.pos(), "bottom");
        return "done";
      case "g":
        if (rest.length < 2) return "more";
        if (rest[1] === "~" || rest[1] === "u" || rest[1] === "U") return this.operator(rest[1], reg, n1, rest.slice(2));
        if (rest[1] === "v" && this.lastVisual) {
          const lv = this.lastVisual;
          this.mode = lv.mode;
          this.anchor = Math.min(lv.anchor, this.d.length);
          this.paintVisual(Math.min(lv.head, this.d.length));
        }
        if (rest[1] === "J") return this.join(Math.max(2, count), false), "done";
        return "done";
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
    if (s.startsWith("ge") || s.startsWith("gE")) {
      name = s.slice(0, 2);
      len = 2;
    }
    if (CHAR_MOTIONS.has(name)) {
      if (s.length < 2) return "more";
      this.lastFind = { kind: name, c: s[1] };
      const m = this.findChar(name, s[1], Math.max(1, n));
      return m ? { m, len: 2 } : { m: { to: this.pos() }, len: 2 };
    }
    if (name === "'" || name === "`") {
      if (s.length < 2) return "more";
      const to = this.markAt(s[1]);
      if (to === null) {
        this.say("nomark", s[1]);
        return { m: { to: this.pos() }, len: 2 };
      }
      this.setMark("'", this.pos());
      return name === "'" ? { m: { to: this.firstNonBlank(this.d.lineAt(to).number), line: true }, len: 2 } : { m: { to }, len: 2 };
    }
    if (name === "Enter") return { m: this.vertical(Math.max(1, n), true), len: 1 };
    if (!MOTIONS.has(name)) return null;
    const at = this.pos();
    const m = this.motion(name, n);
    if (JUMPS.has(name) && m.to !== at) this.setMark("'", at);
    return { m, len };
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
      case "ge":
      case "gE":
        return { to: this.wordEndBack(at, count, name === "gE"), incl: true };
      case "_":
      case "+":
      case "-": {
        const ln = d.lineAt(at).number + (name === "_" ? count - 1 : name === "+" ? count : -count);
        return { to: this.firstNonBlank(Math.min(d.lines, Math.max(1, ln))), line: true };
      }
      case "|": {
        const l = d.lineAt(at);
        return { to: l.from + Math.min(count - 1, Math.max(0, l.length - 1)) };
      }
      case "%":
        return { to: this.matchBracket(at) ?? at, incl: true };
      case "H":
      case "M":
      case "L": {
        const v = this.h.view();
        const ln = name === "H" ? v.top + count - 1 : name === "L" ? v.bottom - count + 1 : Math.floor((v.top + v.bottom) / 2);
        return { to: this.firstNonBlank(Math.min(d.lines, Math.max(1, ln))), line: true };
      }
      case "n":
      case "N":
      case "S":
        return { to: this.searchFrom(at, name === "N", count) ?? at };
      case "*":
      case "#": {
        const w = this.wordAt(at);
        if (!w) return { to: at };
        this.search = { pat: "\\<" + w.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "\\>", back: name === "#" };
        this.regs.set("/", { text: this.search.pat, line: false });
        return { to: this.searchFrom(at, false, count) ?? at };
      }
      case ";":
      case ",": {
        const f = this.lastFind;
        if (!f) return { to: at };
        const kind = name === ";" ? f.kind : ({ f: "F", F: "f", t: "T", T: "t" } as Record<string, string>)[f.kind];
        let m = this.findChar(kind, f.c, count);
        if (m && m.to === at && (kind === "t" || kind === "T")) m = this.findChar(kind, f.c, count + 1);
        return m ?? { to: at };
      }
    }
    return { to: at };
  }

  private wordEndBack(at: number, count: number, big: boolean): number {
    let p = at;
    for (let i = 0; i < count; i++) {
      const k = kindOf(this.ch(p), big);
      while (p > 0 && kindOf(this.ch(p), big) === k && k !== 0) p--;
      while (p > 0 && kindOf(this.ch(p), big) === 0) p--;
    }
    return p;
  }

  private wordAt(at: number): string | null {
    const l = this.d.lineAt(at);
    let i = at - l.from;
    while (i < l.length && kindOf(l.text[i], false) !== 1) i++;
    if (i >= l.length) return null;
    let a = i, b = i;
    while (a > 0 && kindOf(l.text[a - 1], false) === 1) a--;
    while (b < l.length && kindOf(l.text[b], false) === 1) b++;
    return l.text.slice(a, b);
  }

  /** searchFrom finds the search's next match from at (back: the other
   * way), round the ends; null (and a message) when there is none. */
  private searchFrom(at: number, other: boolean, count: number): number | null {
    const s = this.search;
    if (!s) return null;
    const re = vimRegex(s.pat, true);
    if (!re) {
      this.say("notfound", s.pat);
      return null;
    }
    const text = this.d.toString();
    const back = s.back !== other;
    let p = at;
    for (let i = 0; i < count; i++) {
      const next = back ? prevMatch(text, re, p) : nextMatch(text, re, p);
      if (next < 0) {
        this.say("notfound", s.pat);
        return null;
      }
      p = next;
    }
    return p;
  }

  /** matchBracket is the bracket matching the first one at or after at
   * on its line (null: none). */
  private matchBracket(at: number): number | null {
    const l = this.d.lineAt(at);
    let p = at;
    while (p < l.to && !"()[]{}".includes(this.ch(p))) p++;
    if (p >= l.to) return null;
    const c = this.ch(p);
    const [o, cl] = BRACKETS[c];
    return c === o ? this.scanBracket(p + 1, o, cl, 1) : this.scanBracket(p - 1, o, cl, -1);
  }

  /** scanBracket walks from at (dir: +1 for the close, -1 for the open)
   * to the bracket that closes the level it starts in. */
  private scanBracket(at: number, open: string, close: string, dir: 1 | -1): number | null {
    let depth = 0;
    const text = this.d.toString();
    for (let p = at; p >= 0 && p < text.length; p += dir) {
      const c = text[p];
      if (c === (dir > 0 ? open : close)) depth++;
      else if (c === (dir > 0 ? close : open)) {
        if (depth === 0) return p;
        depth--;
      }
    }
    return null;
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
    if (this.mode === "vblock" && !/^[jk]|^\x00Arrow(Up|Down)/.test(name)) this.vdollar = name.startsWith("$");
    if (this.inVisual()) return this.paintVisual(m.to);
    this.h.place(this.clamp(m.to));
  }

  private paintVisual(head: number): void {
    this.vhead = head;
    const a = this.anchor;
    if (this.mode === "vblock") {
      this.h.selectBlock(this.blockRanges(), head);
      return;
    }
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
    if (this.mode === "vline" || this.mode === "vblock") return this.lineRange(this.d.lineAt(lo).number, this.d.lineAt(hi).number);
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
    if (c === "O" && this.mode === "vblock") {
      const d = this.d;
      const la = d.lineAt(this.anchor), lh = d.lineAt(this.vhead);
      const ca = this.anchor - la.from, ch = this.vhead - lh.from;
      this.anchor = la.from + Math.min(ch, la.length);
      this.paintVisual(lh.from + Math.min(ca, lh.length));
      return "done";
    }
    if (c === "i" || c === "a") {
      if (rest.length < 2) return "more";
      const r = this.textObject(c === "a", rest[1]);
      if (r) {
        if (r.line) this.mode = "vline";
        this.anchor = Math.min(this.anchor, r.from);
        this.paintVisual(Math.max(r.from, r.to - 1));
      }
      return "done";
    }
    if (c === "g") {
      if (rest.length < 2) return "more";
      if (rest[1] === "u" || rest[1] === "U" || rest[1] === "~") return this.visualCommand(reg, rest[1] === "~" ? "~" : rest[1]);
      return "done";
    }
    if (this.mode === "vblock" && "dxDyYcsCIAr~uU\x7f".includes(c)) return this.blockCommand(reg, rest);
    const r = this.visualRange();
    if ("dxc~s\x7f".includes(c)) this.visualDot(c === "x" || c === "\x7f" ? "d" : c === "s" ? "c" : c, r);
    if ("<>uUdxcsy~J\x7f".includes(c)) {
      this.lastVisual = { anchor: this.anchor, head: this.vhead, mode: this.mode };
      this.setMark("<", r.from);
      this.setMark(">", Math.max(r.from, r.to - 1));
    }
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
      case "u":
      case "U":
        done();
        this.operate(c, reg, r);
        return "done";
      case ">":
      case "<":
        done();
        this.operate(c, reg, r);
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

  /** visualDot keeps a visual change for the . repeat as the normal
   * command that does the same from the caret: as many lines (3dd), or
   * as many chars on one line (4x). Other shapes are not repeated. */
  private visualDot(op: string, r: Range): void {
    const d = this.d;
    const a = d.lineAt(r.from).number, b = d.lineAt(Math.max(r.from, r.to - 1)).number;
    let keys: string[] | null = null;
    if (r.line && op !== "~") {
      const n = b - a + 1;
      keys = [...(n > 1 ? String(n) : ""), op, op];
    } else if (!r.line && a === b && r.to > r.from) {
      const n = r.to - r.from;
      keys = [...(n > 1 ? String(n) : ""), op === "d" ? "x" : op === "c" ? "s" : op];
    }
    this.dotRec = keys;
    this.changed = false;
  }

  /** headChar is the char the visual caret is on (the host's caret is
   * after it when the selection runs forward). */
  private headChar(): number {
    return this.vhead;
  }

  // ---- visual block ----

  /** blockBox is the visual block: its lines lo..hi and its columns
   * left..right (inclusive; columns count UTF-16 units, a tab is one). */
  private blockBox(): { lo: number; hi: number; left: number; right: number } {
    const d = this.d;
    const la = d.lineAt(Math.min(this.anchor, d.length)), lh = d.lineAt(Math.min(this.vhead, d.length));
    const ca = this.anchor - la.from, ch = this.vhead - lh.from;
    return { lo: Math.min(la.number, lh.number), hi: Math.max(la.number, lh.number), left: Math.min(ca, ch), right: Math.max(ca, ch) };
  }

  /** blockRanges are the block's part of each of its lines (empty on a
   * line too short to reach it). */
  private blockRanges(): { from: number; to: number }[] {
    const b = this.blockBox();
    const out: { from: number; to: number }[] = [];
    for (let n = b.lo; n <= b.hi; n++) {
      const l = this.d.line(n);
      const from = l.from + Math.min(b.left, l.length);
      out.push({ from, to: this.vdollar ? l.to : Math.max(from, l.from + Math.min(b.right + 1, l.length)) });
    }
    return out;
  }

  /** editLines replaces lines lo..hi with the lines out as one edit. */
  private editLines(lo: number, hi: number, out: readonly string[], caret: number): void {
    const d = this.d;
    this.edit(d.line(lo).from, d.line(hi).to, out.join("\n"), caret);
  }

  /** blockCommand runs d y c r ~ u U I A (and D Y C: to the lines' ends)
   * on the visual block, each line on its own, as one edit. */
  private blockCommand(reg: string, rest: string): "more" | "done" {
    const c = rest[0];
    if (c === "r" && rest.length < 2) return "more";
    if ("DYC".includes(c)) this.vdollar = true;
    const b = this.blockBox();
    const rs = this.blockRanges();
    const d = this.d;
    const end = this.vdollar;
    this.leaveVisual();
    this.dotRec = null;
    const top = d.line(b.lo);
    const at = top.from + Math.min(b.left, top.length);
    const pieces = rs.map((r) => d.sliceString(r.from, r.to));
    const lines: string[] = [];
    for (let n = b.lo; n <= b.hi; n++) lines.push(d.line(n).text);
    const cut = (fn: (s: string) => string): string[] => lines.map((t, i) => {
      const l = d.line(b.lo + i);
      const f = rs[i].from - l.from, e = rs[i].to - l.from;
      return t.slice(0, f) + fn(t.slice(f, e)) + t.slice(e);
    });
    if ("dxDyYcsC\x7f".includes(c)) {
      const text = pieces.join("\n");
      this.setReg(reg, { text, line: false, block: true }, c === "y" || c === "Y");
      if (reg === "+" || reg === "*") this.h.copied(text);
    }
    if (c === "y" || c === "Y") {
      this.h.place(at);
      return "done";
    }
    if (this.h.readOnly()) return "done";
    switch (c) {
      case "I":
      case "A":
      case "c":
      case "s":
      case "C": {
        const app = c === "A";
        let col = app ? b.right + 1 : b.left;
        if (c !== "I" && c !== "A") this.editLines(b.lo, b.hi, cut(() => ""), at);
        const l = this.d.line(b.lo);
        if (app && !end && l.length < col) {
          this.edit(l.to, l.to, " ".repeat(col - l.length), l.from + col);
        }
        const first = this.d.line(b.lo);
        if (app && end) col = first.length;
        this.blockIns = { lo: b.lo, hi: b.hi, col, end: app && end, pad: app, len0: first.length, lines: this.d.lines };
        this.insertAt(first.from + Math.min(col, first.length));
        return "done";
      }
      case "r":
        this.editLines(b.lo, b.hi, cut((s) => s.replace(/[^\n]/g, rest[1])), at);
        return "done";
      case "~":
      case "u":
      case "U":
        this.editLines(b.lo, b.hi, cut((s) => (c === "u" ? s.toLowerCase() : c === "U" ? s.toUpperCase() : swapCase(s))), at);
        return "done";
    }
    // d x D: the block goes.
    this.editLines(b.lo, b.hi, cut(() => ""), at);
    this.h.place(this.clamp(at));
    return "done";
  }

  /** blockInsert puts the text an I, A or c typed on the block's first
   * line on its other lines too (not when it ran over lines). */
  private blockInsert(): void {
    const bi = this.blockIns;
    this.blockIns = null;
    if (!bi || bi.hi <= bi.lo || this.h.readOnly()) return;
    const d = this.d;
    if (bi.hi > d.lines) return;
    const first = d.line(bi.lo);
    const n = first.length - bi.len0;
    if (n <= 0 || d.lines !== bi.lines) return;
    const text = first.text.slice(bi.col, bi.col + n);
    const out: string[] = [];
    for (let ln = bi.lo + 1; ln <= bi.hi; ln++) {
      const t = d.line(ln).text;
      if (bi.end) out.push(t + text);
      else if (t.length >= bi.col) out.push(t.slice(0, bi.col) + text + t.slice(bi.col));
      else out.push(bi.pad ? t + " ".repeat(bi.col - t.length) + text : t);
    }
    const caret = this.h.caret();
    this.edit(d.line(bi.lo + 1).from, d.line(bi.hi).to, out.join("\n"), caret);
  }

  /** putBlock puts a yanked block at col of line n and the lines after
   * it (count times side by side), padding short lines with spaces and
   * adding lines at the end of the text. */
  private putBlock(text: string, col: number, n: number, count: number): void {
    const d = this.d;
    const rows = text.split("\n");
    const w = Math.max(...rows.map((r) => r.length));
    const last = Math.min(d.lines, n + rows.length - 1);
    const out: string[] = [];
    for (let i = 0; i < rows.length; i++) {
      const t = n + i <= last ? d.line(n + i).text : "";
      let piece = rows[i].padEnd(w).repeat(count);
      if (col >= t.length) piece = piece.replace(/ +$/, "");
      out.push(t.length >= col ? t.slice(0, col) + piece + t.slice(col) : t + " ".repeat(col - t.length) + piece);
    }
    const from = d.line(n).from;
    const to = d.line(last).to;
    this.edit(from, to, out.join("\n"), from + Math.min(col, out[0].length));
  }

  // ---- operators ----

  private operator(op: Op, reg: string, n1: number, rest: string): "more" | "done" {
    if (rest === "") return "more";
    const m = /^([1-9]\d*)?(.*)$/s.exec(rest);
    const n2 = m && m[1] ? parseInt(m[1], 10) : 0;
    const tail = m ? m[2] : rest;
    if (tail === "") return "more";
    const n = Math.max(1, n1) * Math.max(1, n2);
    // Doubled: whole lines (dd, >>, guu, gugu).
    if (tail[0] === op || ("~uU".includes(op) && tail.startsWith("g" + op))) return this.operate(op, reg, this.lines(n)), "done";
    if (tail === "g") return "more";
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
    if (c === '"' || c === "'" || c === "`") return this.quoteObject(around, c);
    if (BRACKETS[c]) return this.bracketObject(around, BRACKETS[c]);
    if (c === "p") return this.paragraphObject(around);
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

  /** quoteObject: the quoted text on the caret's line (the pair round
   * the caret, else the first after it). */
  private quoteObject(around: boolean, q: string): Range | null {
    const at = this.pos();
    const l = this.d.lineAt(at);
    const col = at - l.from;
    const qs: number[] = [];
    for (let i = 0; i < l.length; i++) if (l.text[i] === q && (i === 0 || l.text[i - 1] !== "\\")) qs.push(i);
    for (let i = 0; i + 1 < qs.length; i += 2) {
      const a = qs[i], b = qs[i + 1];
      if (col > b) continue;
      let from = l.from + a, to = l.from + b + 1;
      if (!around) return { from: from + 1, to: to - 1, line: false };
      let e = to;
      while (e < l.to && /\s/.test(this.ch(e))) e++;
      if (e === to) while (from > l.from && /\s/.test(this.ch(from - 1))) from--;
      return { from, to: e, line: false };
    }
    return null;
  }

  private bracketObject(around: boolean, [open, close]: [string, string]): Range | null {
    const at = this.pos();
    const c = this.ch(at);
    const o = c === open ? at : c === close ? this.scanBracket(at - 1, open, close, -1) : this.scanBracket(at - 1, open, close, -1);
    if (o === null) return null;
    const e = this.scanBracket(o + 1, open, close, 1);
    if (e === null) return null;
    return around ? { from: o, to: e + 1, line: false } : { from: o + 1, to: e, line: false };
  }

  private paragraphObject(around: boolean): Range | null {
    const d = this.d;
    let a = d.lineAt(this.pos()).number, b = a;
    const blank = (n: number) => d.line(n).text.trim() === "";
    const kind = blank(a);
    while (a > 1 && blank(a - 1) === kind) a--;
    while (b < d.lines && blank(b + 1) === kind) b++;
    if (around) while (b < d.lines && blank(b + 1) !== kind) b++;
    return this.lineRange(a, b);
  }  private charRange(count: number, dir: 1 | -1): Range {
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

  /** setReg keeps what y or d took: in reg (A-Z appends to a-z; _
   * keeps nothing), the unnamed register, 0 for a yank, 1-9 (lines) or
   * - (a small delete) for a delete. */
  private setReg(reg: string, r: Reg, yank: boolean): void {
    if (reg === "_") return;
    if (/^[A-Z]$/.test(reg)) {
      const lower = reg.toLowerCase();
      const prev = this.regs.get(lower);
      if (prev) r = prev.line || r.line ? { text: nl(prev.text) + nl(r.text), line: true } : { text: prev.text + r.text, line: false };
      this.macros.delete(lower);
      reg = lower;
    }
    this.regs.set('"', r);
    if (reg !== '"') {
      this.regs.set(reg, r);
      if (/^[a-z]$/.test(reg)) this.macros.delete(reg);
      return;
    }
    if (yank) this.regs.set("0", r);
    else if (r.line || r.text.includes("\n")) {
      for (let i = 9; i > 1; i--) {
        const prev = this.regs.get(String(i - 1));
        if (prev) this.regs.set(String(i), prev);
      }
      this.regs.set("1", r);
    } else this.regs.set("-", r);
  }

  private getReg(reg: string): Reg | undefined {
    return this.regs.get(reg.toLowerCase());
  }

  /** operate runs an operator over r. */
  private operate(op: Op, reg: string, r: Range): void {
    const d = this.d;
    if (op === ">" || op === "<") return this.shift(r, op === ">");
    if (op === "~" || op === "u" || op === "U") return this.caseRange(r, op);
    const text = d.sliceString(r.from, r.to);
    if (r.to <= r.from && !r.line) {
      if (op === "c") this.insertAt(r.from);
      return;
    }
    this.setReg(reg, { text: r.line ? text + "\n" : text, line: r.line }, op === "y");
    if (reg === "+" || reg === "*") this.h.copied(r.line ? text + "\n" : text);
    if (op === "y") {
      this.h.place(r.line ? this.firstNonBlank(d.lineAt(r.from).number) : r.from);
      return;
    }
    if (this.h.readOnly()) return;
    if (!r.line) {
      this.edit(r.from, r.to, "", r.from);
      if (op === "c") this.insertAt(r.from);
      return;
    }
    if (op === "c") {
      // cc keeps the first line's indent and the line itself.
      const ind = /^\s*/.exec(d.lineAt(r.from).text)?.[0] ?? "";
      this.edit(r.from, r.to, ind, r.from + ind.length);
      this.insertAt(r.from + ind.length);
      return;
    }
    // dd: the lines and one line end go.
    let from = r.from, to = r.to;
    if (to < d.length) to++;
    else if (from > 0) from--;
    this.edit(from, to, "", from);
    const nd = this.d;
    const ln = nd.lineAt(Math.min(from === r.from ? from : from + 1, nd.length)).number;
    this.h.place(this.firstNonBlank(Math.min(ln, nd.lines)));
  }

  // ---- ex commands ----

  /** ex runs a command line (:3, :w, :%s/a/b/g …). */
  private ex(line: string): void {
    if (line.trim() === "") return;
    this.lastEx = line;
    this.regs.set(":", { text: line, line: false });
    const d = this.d;
    const cur = d.lineAt(this.pos()).number;
    let s = line.trim().replace(/^:+/, "");
    let a: number | null = null, b: number | null = null;
    if (s.startsWith("%")) {
      a = 1;
      b = d.lines;
      s = s.slice(1);
    } else {
      const r1 = this.address(s, cur);
      if (r1 === "bad") return;
      if (r1) {
        a = r1.n;
        s = r1.rest;
        if (s.startsWith(",") || s.startsWith(";")) {
          const r2 = this.address(s.slice(1), s[0] === ";" ? a : cur);
          if (r2 === "bad") return;
          b = r2 ? r2.n : a;
          s = r2 ? r2.rest : s.slice(1);
        }
      }
    }
    const m = /^\s*([a-zA-Z]*|[<>&!=#@*]+)(!?)\s*(.*)$/s.exec(s);
    if (!m) return this.say("noecmd", line);
    const [, name, , arg] = m;
    if (name === "") {
      if (a === null || arg !== "") return this.say("noecmd", line);
      const ln = Math.min(d.lines, Math.max(1, b ?? a));
      this.setMark("'", this.pos());
      this.h.place(this.firstNonBlank(ln));
      return;
    }
    let lo = Math.min(d.lines, Math.max(1, a ?? cur)), hi = Math.min(d.lines, Math.max(1, b ?? a ?? cur));
    if (lo > hi) [lo, hi] = [hi, lo];
    // A name may be cut short down to min chars (:w, :wri, :write).
    const full = (long: string, min: number) => name.length >= min && long.startsWith(name);
    if (full("write", 1) || full("update", 2)) return this.h.save();
    if (full("wq", 2) || full("xit", 1) || full("exit", 3) || name === "wqa" || name === "xa") {
      this.h.save();
      return this.h.close();
    }
    if (full("quit", 1) || full("close", 3) || name === "qa" || name === "qall") return this.h.close();
    if (full("substitute", 1) || name === "&" || name === "&&") return this.substitute(lo, hi, name.startsWith("&") ? "" : arg);
    if (full("delete", 1) || full("yank", 1)) {
      const reg = /^[a-zA-Z0-9"_+*-]/.test(arg) ? arg[0] : '"';
      this.h.place(d.line(lo).from);
      return this.operate(name[0] === "d" ? "d" : "y", reg, this.lineRange(lo, hi));
    }
    if (full("join", 1)) {
      this.h.place(d.line(lo).from);
      return this.join(Math.max(2, hi - lo + 1));
    }
    if (/^[<>]+$/.test(name)) {
      for (let i = 0; i < name.length; i++) this.shift(this.lineRange(lo, hi), name[0] === ">");
      return;
    }
    if (full("normal", 4)) return this.normalLines(lo, hi, arg);
    if (this.mapCommand(name, arg)) return;
    const leader = /^(?:let\s+)?(?:g:)?mapleader\s*=\s*(["'])(.*)\1\s*$/.exec(line.trim());
    if (leader && name === "let") {
      this.leader = leader[2] === "\\<Space>" || leader[2] === " " ? " " : leader[2];
      return;
    }
    if (full("nohlsearch", 3) || full("set", 2) || full("redraw", 4)) return;
    if (full("registers", 3) || full("display", 2) || full("marks", 4)) return;
    this.say("noecmd", line);
  }

  /** mapCommand runs :map, :nnoremap, :iunmap … (made here, for this
   * editor while the page is open); false: name is none of them. */
  private mapCommand(name: string, arg: string): boolean {
    const known: Record<string, [string, boolean, boolean]> = {
      map: ["nxo", false, false], noremap: ["nxo", true, false], no: ["nxo", true, false], unmap: ["nxo", false, true], unm: ["nxo", false, true],
      nmap: ["n", false, false], nm: ["n", false, false], nnoremap: ["n", true, false], nn: ["n", true, false], nno: ["n", true, false], nunmap: ["n", false, true], nun: ["n", false, true],
      vmap: ["x", false, false], vm: ["x", false, false], xmap: ["x", false, false], xm: ["x", false, false], vnoremap: ["x", true, false], vn: ["x", true, false], xnoremap: ["x", true, false], xn: ["x", true, false], vunmap: ["x", false, true], vu: ["x", false, true], xunmap: ["x", false, true], xu: ["x", false, true],
      omap: ["o", false, false], om: ["o", false, false], onoremap: ["o", true, false], ono: ["o", true, false], ounmap: ["o", false, true], ou: ["o", false, true],
      imap: ["i", false, false], im: ["i", false, false], inoremap: ["i", true, false], ino: ["i", true, false], iunmap: ["i", false, true], iu: ["i", false, true],
    };
    const k = known[name];
    if (!k) return false;
    const [modes, noremap, un] = k;
    let rest = arg.trim();
    for (;;) {
      const opt = /^<(silent|buffer|nowait|unique|special|script|expr)>\s*/i.exec(rest);
      if (!opt) break;
      if (opt[1].toLowerCase() === "expr" || opt[1].toLowerCase() === "script") return true; // the web cannot run it
      rest = rest.slice(opt[0].length);
    }
    const sp = rest.search(/\s/);
    const lhs = sp < 0 ? rest : rest.slice(0, sp);
    const rhs = sp < 0 ? "" : rest.slice(sp).trim();
    if (lhs === "") return true;
    for (const mode of modes) {
      if (un) this.unmap(mode, lhs);
      else if (rhs !== "") this.addMap(mode, lhs, rhs, noremap);
    }
    return true;
  }

  /** address reads one line address at the start of s: a number, . $
   * 'x (a mark), /pat/, with +n -n after it. null: none; "bad": a mark
   * that is not set or a pattern with no match (said). */
  private address(s: string, cur: number): { n: number; rest: string } | null | "bad" {
    const d = this.d;
    const m = /^(\d+|\.|\$|'[a-zA-Z<>'`]|\/(?:[^/\\]|\\.)*\/?)?((?:\s*[+-]\d*)*)/.exec(s);
    if (!m || m[0] === "") return null;
    let n = cur;
    const base = m[1] ?? "";
    if (/^\d+$/.test(base)) n = parseInt(base, 10);
    else if (base === "$") n = d.lines;
    else if (base.startsWith("'")) {
      const at = this.markAt(base[1]);
      if (at === null) return this.say("nomark", base[1]), "bad";
      n = d.lineAt(at).number;
    } else if (base.startsWith("/")) {
      const pat = base.slice(1).replace(/\/$/, "") || this.search?.pat || "";
      const re = vimRegex(pat, true);
      const at = re ? nextMatch(d.toString(), re, d.line(cur).to) : -1;
      if (at < 0) return this.say("notfound", pat), "bad";
      n = d.lineAt(at).number;
    }
    for (const o of m[2].matchAll(/([+-])(\d*)/g)) n += (o[1] === "+" ? 1 : -1) * (o[2] === "" ? 1 : parseInt(o[2], 10));
    return { n, rest: s.slice(m[0].length) };
  }

  /** substitute runs :s/pat/rep/flags over lines lo..hi (one edit). */
  private substitute(lo: number, hi: number, arg: string): void {
    if (this.h.readOnly()) return;
    let pat = "", rep = "", flags = "";
    if (arg !== "" && !/^[a-zA-Z0-9\s"|\\]/.test(arg[0])) {
      const del = arg[0];
      const parts: string[] = [""];
      for (let i = 1; i < arg.length; i++) {
        const c = arg[i];
        if (c === "\\" && i + 1 < arg.length) {
          parts[parts.length - 1] += arg[i + 1] === del ? del : c + arg[i + 1];
          i++;
        } else if (c === del && parts.length < 3) parts.push("");
        else parts[parts.length - 1] += c;
      }
      [pat, rep, flags] = [parts[0] ?? "", parts[1] ?? "", parts[2] ?? ""];
      this.lastSub = { rep };
    } else {
      rep = this.lastSub?.rep ?? "";
      flags = arg;
    }
    pat = pat || this.search?.pat || "";
    if (!pat) return;
    this.search = { pat, back: false };
    const re = vimRegex(pat + (flags.includes("i") ? "\\c" : ""), flags.includes("g"));
    if (!re) return this.say("notfound", pat);
    const d = this.d;
    const from = d.line(lo).from, to = d.line(hi).to;
    const lines = d.sliceString(from, to).split("\n");
    let last = -1;
    const out = lines.map((t, i) => {
      re.lastIndex = 0;
      const nt = t.replace(re, (...g: unknown[]) => subRep(rep, g.slice(0, -2) as string[]));
      if (nt !== t || re.test(t)) last = i;
      return nt;
    });
    if (last < 0) return flags.includes("e") ? undefined : this.say("notfound", pat);
    const body = out.join("\n");
    this.edit(from, to, body, from);
    this.h.place(this.firstNonBlank(lo + last + body.split("\n").length - lines.length));
  }

  /** normalLines runs keys as normal-mode keys on each line lo..hi. */
  private normalLines(lo: number, hi: number, keys: string): void {
    const toks = textKeys(keys);
    const n = hi - lo + 1;
    for (let i = 0; i < n; i++) {
      const d = this.d;
      const ln = lo + i;
      if (ln > d.lines) break;
      this.mode = "normal";
      this.pending = "";
      this.h.place(d.line(ln).from);
      this.play(toks, true);
      this.cl = null;
      const mode = this.mode as VimMode;
      if (mode === "insert") this.leaveInsert();
      else if (mode !== "normal") this.leaveVisual();
      this.pending = "";
    }
  }

  /** repeat does the last change again (n: with this count instead). */
  private repeat(n: number): void {
    let keys = this.lastDot;
    if (keys.length === 0) return;
    if (n > 0) {
      let i = 0;
      while (i < keys.length && /^[0-9]$/.test(keys[i]) && !(i === 0 && keys[i] === "0")) i++;
      keys = [...String(n), ...keys.slice(i)];
    }
    this.pending = "";
    this.dotRec = null;
    this.dotReplay = true;
    try {
      this.play(keys);
      if (this.mode === "insert") this.leaveInsert();
    } finally {
      this.dotReplay = false;
    }
    this.dotRec = null;
    this.changed = false;
  }

  private stopRecording(): void {
    const keys = this.recKeys.slice(0, -1); // not the q that stops it
    const r = this.rec;
    const all = this.recAppend ? [...(this.macros.get(r) ?? []), ...keys] : keys;
    this.rec = "";
    this.recKeys = [];
    this.macros.set(r, all);
    this.regs.set(r, { text: keysText(all), line: false });
  }

  /** playMacro plays register r count times (@@: the last one again). */
  private playMacro(r: string, count: number): void {
    if (r === "@") r = this.lastMacro;
    if (r === ":") {
      this.lastMacro = ":";
      this.pending = "";
      for (let i = 0; i < count && this.lastEx; i++) this.ex(this.lastEx);
      return;
    }
    r = r.toLowerCase();
    if (!r) return;
    const reg = this.regs.get(r);
    const keys = this.macros.get(r) ?? (reg ? textKeys(reg.text) : []);
    if (keys.length === 0) return;
    this.lastMacro = r;
    this.pending = "";
    this.dotRec = null;
    for (let i = 0; i < count && this.steps <= STEP_LIMIT; i++) this.play(keys, true);
  }

  private insertAt(at: number): void {
    this.insKeys = [];
    this.insRepeat = 0;
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
          this.edit(l.to, l.to, "\n" + ind, l.to + 1 + ind.length);
          return this.insertAt(l.to + 1 + ind.length);
        }
        this.edit(l.from, l.from, ind + "\n", l.from + ind.length);
        return this.insertAt(l.from + ind.length);
      }
    }
  }

  private put(reg: string, before: boolean, count: number): void {
    const r = this.getReg(reg);
    if (!r || this.h.readOnly()) return;
    const at = this.pos();
    const d = this.d;
    const l = d.lineAt(at);
    if (r.block) return this.putBlock(r.text, before || l.length === 0 ? at - l.from : at - l.from + 1, l.number, count);
    if (r.line) {
      const body = r.text.repeat(count);
      if (before) {
        this.edit(l.from, l.from, body, l.from);
        this.h.place(this.firstNonBlank(l.number));
      } else if (l.to < d.length) {
        this.edit(l.to + 1, l.to + 1, body, l.to + 1);
        this.h.place(this.firstNonBlank(l.number + 1));
      } else {
        // After the last line: a line end first, none at the end.
        this.edit(l.to, l.to, "\n" + body.slice(0, -1), l.to + 1);
        this.h.place(this.firstNonBlank(l.number + 1));
      }
      return;
    }
    const pos = before || l.length === 0 ? at : at + 1;
    const body = r.text.repeat(count);
    this.edit(pos, pos, body, pos + body.length - 1);
  }

  private join(lines: number, spaces = true): void {
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
      out = spaces ? out.replace(/\s+$/, "") + (t === "" || t.startsWith(")") ? "" : " ") + t : out + d.line(n).text;
    }
    const from = d.line(first).from;
    this.edit(from, d.line(last).to, out, from + caret);
  }

  private toggleCase(count: number): void {
    if (this.h.readOnly()) return;
    const at = this.pos();
    const to = Math.min(this.lineEnd(at), at + count);
    if (to <= at) return;
    const s = this.d.sliceString(at, to);
    this.edit(at, to, swapCase(s), Math.min(to, this.lineEnd(at) - 1));
  }

  private caseRange(r: Range, op: "~" | "u" | "U" = "~"): void {
    if (this.h.readOnly() || r.to <= r.from) return;
    const s = this.d.sliceString(r.from, r.to);
    this.edit(r.from, r.to, op === "u" ? s.toLowerCase() : op === "U" ? s.toUpperCase() : swapCase(s), r.line ? this.firstNonBlank(this.d.lineAt(r.from).number) : r.from);
  }

  /** shift indents (or outdents) the lines of r by one level. */
  private shift(r: Range, right: boolean): void {
    if (this.h.readOnly()) return;
    const d = this.d;
    const a = d.lineAt(r.from).number, b = d.lineAt(Math.max(r.from, r.to - (r.line ? 0 : 1))).number;
    const unit = this.h.indent();
    const w = unit === "\t" ? 1 : unit.length;
    const out: string[] = [];
    for (let n = a; n <= b; n++) {
      const t = d.line(n).text;
      if (right) out.push(t === "" ? t : unit + t);
      else {
        const m = /^(\t| {1,8})/.exec(t);
        out.push(m ? t.slice(m[1] === "\t" ? 1 : Math.min(w, m[1].length)) : t);
      }
    }
    const from = d.line(a).from;
    const body = out.join("\n");
    this.edit(from, d.line(b).to, body, from);
    this.h.place(this.firstNonBlank(a));
  }

  private replaceChars(c: string, count: number): void {
    if (this.h.readOnly() || c === "\x00") return;
    const at = this.pos();
    if (at + count > this.lineEnd(at)) return;
    const ins = c === "\n" ? "\n" : c.repeat(count);
    this.edit(at, at + count, ins, c === "\n" ? at + 1 : at + count - 1);
  }
}

function nextMatch(text: string, re: RegExp, at: number): number {
  re.lastIndex = at + 1;
  let m = re.exec(text);
  if (!m) {
    re.lastIndex = 0;
    m = re.exec(text);
  }
  return m ? m.index : -1;
}

function prevMatch(text: string, re: RegExp, at: number): number {
  let last = -1, before = -1;
  re.lastIndex = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index < at) before = m.index;
    last = m.index;
    if (m[0] === "") re.lastIndex++;
  }
  return before >= 0 ? before : last;
}

/** subRep writes a :s replacement for one match (g: the match and its
 * groups): & and \0 the match, \1-\9 a group, \r or \n a new line. */
function subRep(rep: string, g: string[]): string {
  let out = "";
  for (let i = 0; i < rep.length; i++) {
    const c = rep[i];
    if (c === "&") out += g[0] ?? "";
    else if (c === "\\" && i + 1 < rep.length) {
      const n = rep[++i];
      if (n >= "0" && n <= "9") out += g[+n] ?? "";
      else if (n === "r" || n === "n") out += "\n";
      else if (n === "t") out += "\t";
      else out += n;
    } else out += c;
  }
  return out;
}

function same(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((t, i) => t === b[i]);
}

function nl(s: string): string {
  return s.endsWith("\n") ? s : s + "\n";
}

function swapCase(s: string): string {
  let o = "";
  for (const ch of s) {
    const u = ch.toUpperCase();
    o += ch === u ? ch.toLowerCase() : u;
  }
  return o;
}
