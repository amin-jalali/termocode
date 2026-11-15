// tactile.js: the Tactile Code on-screen keyboard (the package in
// web/packages/tactile-keyboard) bound to the page's text surfaces, built
// alone with its stylesheet (tactile.css). lib/tactile.ts loads it on a
// touch device with the keyboard on and gives it what it needs through
// bind(); it imports no value of the page (lib/tactileprefs.ts is bundled
// in).
import "../../../../../web/packages/tactile-keyboard/src/tactile-keyboard.css";
import "./page.css";
import { autoCompact, autoPair, currentWord, editorSuggestions, mountDock, newlineIndent, padStepper, threeFingerToggle, webVibrate, type Dock, type KeyEvent, type SuggestionBar } from "../../../../../web/packages/tactile-keyboard/src/index";
import { kbPrefs, setKbPrefs } from "../lib/tactileprefs";

export type { KeyEvent };

/** Sink is a text surface the keyboard types into. */
export interface Sink {
  /** key gets every key (text and named keys, DOM KeyboardEvent terms). */
  key(e: KeyEvent): void;
  /** ai: the ✨ key (none: the key does nothing, like in the terminal). */
  ai?(): void;
  /** pad: how the spacebar trackpad moves: "arrows" sends ← → only (a
   * shell line), "caret" moves the caret in all four ways (an editor). */
  pad: "arrows" | "caret";
  /** suggest refreshes the suggestion strip after a key (an editor). */
  suggest?(bar: SuggestionBar): void;
}

/** EditorLike is the phone editor's handle (editor/api.ts), as far as
 * the keyboard needs it. */
export interface EditorLike {
  text(): string;
  caret(): number;
  type(text: string, del?: number, back?: number): void;
  key(k: { key: string; ctrl: boolean; alt: boolean; shift: boolean }): void;
}

/** An editor to type into: the keyboard brings its typing smarts. */
export interface EditorSpec {
  editor: EditorLike;
  /** path names the file (its language for the keyword snippets). */
  path: string;
}

const LANG: Record<string, string> = { go: "go", py: "python", js: "javascript", ts: "typescript", tsx: "typescript", jsx: "javascript", dart: "dart", rs: "rust", java: "java", c: "c", cpp: "cpp" };

/** editorSink types into an editor like the original app's: auto pairs,
 * Enter keeps the indent, the strip shows the keyword snippet and the
 * buffer's words, Esc clears it. Big files skip the word scan. */
export function editorSink({ editor: ed, path }: EditorSpec): Sink {
  const lang = LANG[path.slice(path.lastIndexOf(".") + 1).toLowerCase()] ?? null;
  return {
    pad: "caret",
    key(e) {
      const plain = !e.ctrl && !e.alt && !e.meta;
      if (plain && e.key.length === 1) {
        const p = autoPair(e.key, ed.text()[ed.caret()] ?? "");
        if (p?.skip) ed.key({ key: "ArrowRight", ctrl: false, alt: false, shift: false });
        else if (p) ed.type(p.text, 0, p.caretBackBy);
        else ed.type(e.key);
        return;
      }
      if (plain && e.key === "Enter") return ed.type("\n" + newlineIndent(ed.text().slice(0, ed.caret())));
      ed.key(e);
    },
    suggest(bar) {
      const text = ed.text();
      if (text.length > 200000) return bar.clear();
      const at = ed.caret();
      const items = editorSuggestions(text, at, lang);
      if (!items.length) return bar.clear();
      bar.show(items, (it) => {
        const w = currentWord(ed.text(), ed.caret()).length;
        ed.type(it.insertText, w, it.caretOffset === undefined ? 0 : it.insertText.length - it.caretOffset);
      });
    },
  };
}

/** Host is what the page gives: its haptics switch. */
export interface Host {
  haptics(): boolean;
}

const FS = { hide: "Hide keyboard", show: "Show keyboard", system: "System keyboard" };
const FA = { hide: "پنهان کردن صفحه‌کلید", show: "نمایش صفحه‌کلید", system: "صفحه‌کلید سیستم" };

let dock: Dock | undefined;
let box: HTMLElement | undefined;
let cur: { el: HTMLElement; sink: Sink } | undefined;
let system: HTMLElement | undefined; // the surface the user switched to the system keyboard
const vib = webVibrate();

const arrow = (key: string): KeyEvent => ({ key, code: key, id: key, ctrl: false, alt: false, shift: false, meta: false, repeat: false });

function ensureDock(host: Host): Dock {
  if (dock) return dock;
  box = document.createElement("div");
  box.className = "tk-host";
  document.body.append(box);
  const p = kbPrefs();
  const d = mountDock(box, {
    layout: "auto",
    labels: document.documentElement.lang === "fa" ? FA : FS,
    soundOn: p.sound,
    hapticLevel: p.haptic,
    vibrate: (ms) => {
      if (host.haptics()) vib?.(ms);
    },
    theme: { accent: getComputedStyle(document.documentElement).getPropertyValue("--blue").trim() || "#007acc" },
    onKey(e) {
      if (e.key === "Escape") d.suggestions.clear();
      cur?.sink.key(e);
      if (e.key !== "Escape") cur?.sink.suggest?.(d.suggestions);
    },
    onAI: () => cur?.sink.ai?.(),
    onPadMove: (dx, dy) => step(dx, dy),
    onToggleSound: (on) => setKbPrefs({ sound: on }),
    onSystemKeyboard: () => toSystem(),
    onCollapse: () => mark(),
  });
  const step = padStepper((s) => {
    if (!cur || (cur.sink.pad === "arrows" && (s === "up" || s === "down"))) return;
    cur.sink.key(arrow(s === "left" ? "ArrowLeft" : s === "right" ? "ArrowRight" : s === "up" ? "ArrowUp" : "ArrowDown"));
  });
  const fit = (): void => {
    d.refit();
    d.setOptions({ layout: autoCompact(innerWidth, innerHeight) ? "compact" : "full" });
    mark();
  };
  new ResizeObserver(() => mark()).observe(box); // the strip comes and goes, a drag peeks
  visualViewport?.addEventListener("resize", fit);
  addEventListener("resize", fit);
  dock = d;
  return d;
}

/** mark tells the page's layout the dock's height (--tk-dock-h) and that
 * it is up (html.tk-on: the terminal's extra-keys row and the bottom tabs
 * hide, the keyboard has those keys). */
function mark(): void {
  const on = !!dock && !!dock.attached && !dock.collapsed;
  const root = document.documentElement;
  root.classList.toggle("tk-on", on);
  root.classList.toggle("tk-off", !!dock?.attached && !!dock.collapsed);
  root.style.setProperty("--tk-dock-h", `${dock?.attached ? (box?.offsetHeight ?? 0) : 0}px`);
}

function toSystem(): void {
  const el = cur?.el;
  if (!el || !dock) return;
  system = el;
  dock.detach(el);
  mark();
  el.setAttribute("inputmode", "text");
  el.blur();
  el.focus(); // inside the tap: the system keyboard opens
}

const focused = (el: HTMLElement): boolean => (el.getRootNode() as Document | ShadowRoot).activeElement === el;

/** bind gives el (the surface's focus target) the Tactile keyboard while
 * it has focus. Returns unbind. */
export function bind(el: HTMLElement, spec: Sink | EditorSpec, host: Host): () => void {
  const sink = "editor" in spec ? editorSink(spec) : spec;
  const off3 = threeFingerToggle(el, { toggleCollapsed: () => (dock?.toggleCollapsed(), mark()) });
  const onFocus = (): void => {
    if (system === el) return;
    const p = kbPrefs();
    if (!p.on) {
      el.removeAttribute("inputmode");
      return;
    }
    el.setAttribute("inputmode", "none");
    const d = ensureDock(host);
    d.setOptions({ soundOn: p.sound, hapticLevel: p.haptic });
    cur = { el, sink };
    d.suggestions.clear();
    d.attach(el);
    mark();
    sink.suggest?.(d.suggestions);
  };
  const onBlur = (): void => {
    setTimeout(() => {
      if (focused(el)) return;
      if (system === el) {
        system = undefined;
        el.setAttribute("inputmode", "none");
      }
      dock?.detach(el);
      if (cur?.el === el) cur = undefined;
      mark();
    });
  };
  if (kbPrefs().on) el.setAttribute("inputmode", "none");
  el.addEventListener("focus", onFocus);
  el.addEventListener("blur", onBlur);
  if (focused(el)) onFocus();
  return () => {
    off3();
    el.removeEventListener("focus", onFocus);
    el.removeEventListener("blur", onBlur);
    el.removeAttribute("inputmode");
    dock?.detach(el);
    if (cur?.el === el) cur = undefined;
    if (system === el) system = undefined;
    mark();
  };
}
