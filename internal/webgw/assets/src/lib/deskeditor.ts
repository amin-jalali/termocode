// The desktop editor (workbench): the vendored editor in a frame of its
// own (monaco.html), behind the same EditorHandle as the phone editor, so
// lib/live.ts keeps the laptop's buffer and this text in step the same
// way. The frame gets the text and colours by message and sends back what
// the user typed; the page keeps a copy of the frame's text, so text() is
// at hand at once.
import { computed, signal } from "@preact/signals";
import type { EditorHandle, EditorHost, HlRow } from "../editor/api";
import { FRAME_PATH, type FrameOpts, type FromFrame, type ToFrame } from "../monaco/protocol";
import { dict } from "../state/dict";
import type { Mark } from "./gutter";
import { cursorAt, focusSlot, quick } from "./workbench";
import { editorMenu } from "./editormenu";

/** COLOR_TOKENS are the workbench colours the frame paints with. */
const COLOR_TOKENS = [
  "--wb-editor-bg", "--wb-editor-fg", "--wb-line-num", "--wb-line-num-active", "--wb-line-hl", "--wb-indent", "--wb-sel",
  "--wb-side-bg", "--wb-focus", "--wb-scroll", "--wb-scroll-hover",
  "--syn-kw", "--syn-ctl", "--syn-fn", "--syn-var", "--syn-ty", "--syn-str", "--syn-num", "--syn-cmt", "--syn-rx", "--syn-cst", "--syn-op",
  "--amber", "--blue", "--red", "--git-add", "--git-del", "--git-mod", "--wb-gm-add", "--wb-gm-mod", "--wb-gm-del",
];

/** hex8 writes a computed colour as #rrggbb or #rrggbbaa. */
export function hex8(c: string): string {
  const s = c.trim();
  if (/^#[0-9a-f]{6}([0-9a-f]{2})?$/i.test(s)) return s.toLowerCase();
  const m = /^rgba?\(\s*(\d+)[\s,]+(\d+)[\s,]+(\d+)(?:[\s,/]+([\d.]+%?))?\s*\)$/i.exec(s);
  if (!m) return s;
  const h = (n: number) => Math.round(Math.min(255, Math.max(0, n))).toString(16).padStart(2, "0");
  let out = "#" + h(+m[1]) + h(+m[2]) + h(+m[3]);
  if (m[4] !== undefined) {
    const a = m[4].endsWith("%") ? parseFloat(m[4]) / 100 : parseFloat(m[4]);
    if (a < 1) out += h(a * 255);
  }
  return out;
}

/** frameColors reads the workbench colours where the editor sits. */
export function frameColors(at: Element): { colors: Record<string, string>; dark: boolean } {
  const cs = getComputedStyle(at);
  const colors = dict<string>();
  for (const k of COLOR_TOKENS) {
    const val = cs.getPropertyValue(k).trim();
    if (val) colors[k] = hex8(val);
  }
  const bg = colors["--wb-editor-bg"] ?? "#1e1e1e";
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(bg.slice(i, i + 2), 16));
  return { colors, dark: 0.299 * r + 0.587 * g + 0.114 * b < 128 };
}

export interface DeskHost extends EditorHost {
  /** slot is the editor group's slot; only the focused one moves the status bar. */
  slot?: string;
  readOnly: boolean;
  wrap: boolean;
  tabSize: number;
  insertSpaces: boolean;
}

/** DeskHandle is an EditorHandle with the settings the view changes. */
export interface DeskHandle extends EditorHandle {
  opts(o: FrameOpts): void;
  gutter(marks: Mark[]): void;
  reveal(line: number): void;
}

/** desks are the desktop editors on screen, by editor group slot. */
export const desks = signal<ReadonlyMap<string, DeskHandle>>(new Map());

/** activeDesk is the focused group's editor (Go to line reveals in it). */
export const activeDesk = computed<DeskHandle | null>(() => desks.value.get(focusSlot.value) ?? null);

/** setDesk puts (or with null, takes) the editor of a slot. */
export function setDesk(slot: string, d: DeskHandle | null): void {
  const next = new Map(desks.peek());
  if (d) next.set(slot, d);
  else next.delete(slot);
  desks.value = next;
}

/** mountDesk puts the frame in parent and returns its handle. */
export function mountDesk(parent: HTMLElement, host: DeskHost): DeskHandle {
  const frame = document.createElement("iframe");
  frame.className = "desk-editor";
  frame.title = host.label;
  frame.src = FRAME_PATH;
  parent.replaceChildren(frame);
  let text = host.doc;
  let seen = 0;
  let canUndo = false, canRedo = false;
  let ready = false;
  const queue: ToFrame[] = [];

  const send = (m: ToFrame) => {
    if (!ready) {
      queue.push(m);
      return;
    }
    frame.contentWindow?.postMessage(m, location.origin);
  };

  const onMessage = (e: MessageEvent) => {
    if (e.source !== frame.contentWindow || e.origin !== location.origin) return;
    const m = e.data as FromFrame;
    if (typeof m !== "object" || m === null || typeof m.t !== "string") return;
    switch (m.t) {
      case "ready": {
        ready = true;
        const c = frameColors(parent);
        frame.contentWindow?.postMessage({ t: "init", doc: text, label: host.label, readOnly: host.readOnly, wrap: host.wrap, tabSize: host.tabSize, insertSpaces: host.insertSpaces, ...c } satisfies ToFrame, location.origin);
        for (const q of queue.splice(0)) {
          // A focus asked before the frame was ready does not take the
          // keyboard from something the user went to meanwhile.
          const away = document.activeElement !== null && document.activeElement !== frame && document.activeElement !== document.body;
          if (q.t === "focus" && (away || quick.peek())) continue;
          frame.contentWindow?.postMessage(q, location.origin);
        }
        return;
      }
      case "text":
        seen = Math.max(seen, m.v);
        canUndo = m.canUndo;
        canRedo = m.canRedo;
        if (m.text !== text) {
          text = m.text;
          if (m.user) host.onChange();
        }
        return;
      case "viewport":
        host.onViewport(m.first, m.last);
        return;
      case "cursor":
        if ((host.slot ?? "g0") === focusSlot.peek()) cursorAt.value = { line: m.line, col: m.col };
        return;
      case "save":
        host.onSave();
        return;
      case "menu": {
        const r = frame.getBoundingClientRect();
        editorMenu(r.left + m.x, r.top + m.y, m, send);
        return;
      }
      case "key":
        // The workbench's keys work while the editor has the focus.
        dispatchEvent(new KeyboardEvent("keydown", { key: m.key, code: m.code, ctrlKey: m.ctrl, metaKey: m.meta, shiftKey: m.shift, altKey: m.alt, bubbles: true, cancelable: true }));
        return;
    }
  };
  addEventListener("message", onMessage);

  return {
    text: () => text,
    remote(t) {
      if (t === text) return;
      text = t;
      send({ t: "remote", text: t, seen });
    },
    replace(t) {
      if (t === text) return;
      text = t;
      send({ t: "replace", text: t });
    },
    setHl(rows: readonly HlRow[]) {
      send({ t: "hl", rows: rows.slice() });
    },
    undo() {
      send({ t: "undo" });
      return canUndo;
    },
    redo() {
      send({ t: "redo" });
      return canRedo;
    },
    canUndo: () => canUndo,
    canRedo: () => canRedo,
    focus() {
      if (quick.peek()) return; // the quick input keeps the keyboard
      frame.focus();
      send({ t: "focus" });
    },
    opts(o) {
      send({ t: "opts", ...o });
    },
    gutter(marks) {
      send({ t: "gutter", marks });
    },
    reveal(line) {
      send({ t: "reveal", line });
    },
    destroy() {
      removeEventListener("message", onMessage);
      if ((host.slot ?? "g0") === focusSlot.peek()) cursorAt.value = null;
      frame.remove();
    },
  };
}
