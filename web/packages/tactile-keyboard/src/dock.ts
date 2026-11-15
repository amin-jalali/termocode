// The bottom dock that hosts the keyboard while a text surface (terminal,
// editor, prompt) has focus: a drag handle (peek at what is under the
// keyboard, spring back), a hide button, the system-keyboard switch, the
// suggestion strip, the keyboard and a safe-area filler. Collapsed, it is
// one "Show keyboard" bar.
import { mount, type MountOptions, type TactileKeyboard } from "./dom";
import { setIcon } from "./icons";
import { SuggestionBar } from "./suggestions";

export interface DockOptions extends MountOptions {
  /** Keyboard height in px (default 380, clamped 220…540). */
  height?: number;
  /** The system keyboard button: the host drops inputmode="none". */
  onSystemKeyboard?(): void;
  /** The dock collapsed (true) or expanded (false). */
  onCollapse?(collapsed: boolean): void;
  /** The visible height of the page (default: the visual viewport). */
  viewport?(): number;
  labels?: Partial<DockLabels>;
}

export interface DockLabels {
  hide: string;
  show: string;
  system: string;
}

const LABELS: DockLabels = { hide: "Hide keyboard", show: "Show keyboard", system: "System keyboard" };

export interface Dock {
  readonly el: HTMLElement;
  readonly keyboard: TactileKeyboard;
  readonly suggestions: SuggestionBar;
  readonly attached: HTMLElement | null;
  readonly collapsed: boolean;
  readonly dragY: number;
  /** The keyboard's height after the viewport cap. */
  readonly kbHeight: number;
  /** Show the dock for a focused surface (expands a collapsed dock). */
  attach(target: HTMLElement): void;
  /** Hide it, if target is the surface it serves. */
  detach(target: HTMLElement): void;
  toggleCollapsed(): void;
  setCollapsed(c: boolean): void;
  setHeight(h: number): void;
  /** Drag the dock down by dy (px). */
  dragBy(dy: number): void;
  /** Release the drag with a velocity (px/s, down positive). */
  dragEnd(velocity: number): void;
  setOptions(o: Partial<DockOptions>): void;
  /** Re-reads the viewport (call on visualViewport resize). */
  refit(): void;
  destroy(): void;
}

export const HANDLE_H = 26;
export const SUGGEST_H = 40;
export const MIN_HEIGHT = 220;
export const MAX_HEIGHT = 540;
/** Code always left above the keyboard. */
export const MIN_EDITOR = 140;
const SPRING = { mass: 1, stiffness: 480, damping: 22 };

export function mountDock(host: HTMLElement, opts: DockOptions): Dock {
  let o: DockOptions = { ...opts };
  const doc = o.doc ?? document;
  const win = o.win ?? (typeof window === "undefined" ? undefined : window);
  const labels = { ...LABELS, ...o.labels };
  const div = (cls: string, parent?: HTMLElement, tag = "div"): HTMLElement => {
    const e = doc.createElement(tag);
    e.className = cls;
    parent?.appendChild(e);
    return e;
  };

  const root = div("tk-dock", host);
  root.hidden = true;
  const body = div("tk-dock-body", root);
  const handle = div("tk-handle", body);
  const sys = div("tk-system", handle, "button");
  sys.setAttribute("type", "button");
  sys.setAttribute("aria-label", labels.system);
  sys.setAttribute("title", labels.system);
  setIcon(sys, "system_keyboard");
  div("tk-grab", handle);
  const hide = div("tk-hide", handle, "button");
  hide.setAttribute("type", "button");
  hide.setAttribute("aria-label", labels.hide);
  hide.setAttribute("title", labels.hide);
  setIcon(hide, "keyboard_hide");
  const suggestions = new SuggestionBar(doc);
  body.appendChild(suggestions.el);
  const kbHost = div("tk-dock-kb", body);
  div("tk-inset", body);
  const bar = div("tk-collapsed", root, "button");
  bar.setAttribute("type", "button");
  bar.hidden = true;
  const barIcon = div("tk-collapsed-icon", bar, "span");
  setIcon(barIcon, "keyboard");
  div("tk-collapsed-label", bar, "span").textContent = labels.show;

  let height = Math.max(MIN_HEIGHT, Math.min(MAX_HEIGHT, o.height ?? 380));
  let attached: HTMLElement | null = null;
  let collapsed = false;
  let dragY = 0;
  let vel = 0;
  let anim = 0;

  const viewport = (): number => o.viewport?.() ?? (win?.visualViewport?.height || win?.innerHeight || 800);
  const suggestH = (): number => (suggestions.items.length ? SUGGEST_H : 0);
  const kbHeight = (): number => {
    const maxKb = Math.max(140, Math.min(MAX_HEIGHT, viewport() - MIN_EDITOR - HANDLE_H - suggestH()));
    return Math.min(height, maxKb);
  };
  const natural = (): number => HANDLE_H + suggestH() + kbHeight();

  const keyboard = mount(kbHost, { ...o, height: kbHeight(), doc, win });

  function layout(): void {
    const kh = kbHeight();
    if (keyboard.height !== kh) keyboard.setOptions({ height: kh });
    root.style.height = dragY > 0 ? `${Math.max(0, natural() - dragY)}px` : `calc(${natural()}px + env(safe-area-inset-bottom, 0px))`;
    body.style.height = `calc(${natural()}px + env(safe-area-inset-bottom, 0px))`;
    body.hidden = collapsed;
    bar.hidden = !collapsed;
    if (collapsed) root.style.height = "";
  }
  suggestions.onChange = layout;

  function setCollapsed(c: boolean): void {
    if (c === collapsed) return;
    collapsed = c;
    stopSpring();
    dragY = 0;
    layout();
    o.onCollapse?.(c);
  }

  const maxDrag = (): number => kbHeight() + 100;
  function dragBy(dy: number): void {
    stopSpring();
    dragY = Math.max(0, Math.min(maxDrag(), dragY + dy));
    layout();
  }
  function stopSpring(): void {
    if (anim && win) win.cancelAnimationFrame(anim);
    anim = 0;
  }
  function dragEnd(velocity: number): void {
    // spring home (0) with a bouncy settle, from the release velocity
    vel = velocity;
    const step = (): void => {
      const dt = 1 / 60;
      for (let i = 0; i < 4; i++) {
        const a = (-SPRING.stiffness * dragY - SPRING.damping * vel) / SPRING.mass;
        vel += (a * dt) / 4;
        dragY += (vel * dt) / 4;
      }
      if (Math.abs(dragY) < 0.5 && Math.abs(vel) < 5) {
        dragY = 0;
        vel = 0;
        anim = 0;
        layout();
        return;
      }
      if (dragY < 0) dragY = Math.max(dragY, -20); // a small overshoot, never above home by much
      layout();
      anim = win ? win.requestAnimationFrame(step) : 0;
    };
    if (!win) {
      dragY = 0;
      layout();
      return;
    }
    anim = win.requestAnimationFrame(step);
  }

  // the handle: drag to peek; its buttons act on click
  let dragPointer: number | undefined;
  let lastY = 0;
  let lastT = 0;
  let speed = 0;
  const now = (): number => (typeof performance !== "undefined" ? performance.now() : Date.now());
  handle.addEventListener("pointerdown", (e) => {
    const pe = e as PointerEvent;
    if (pe.target === sys || pe.target === hide) return;
    dragPointer = pe.pointerId;
    lastY = pe.clientY;
    lastT = now();
    speed = 0;
    try {
      handle.setPointerCapture(pe.pointerId);
    } catch {
      // synthetic pointer
    }
  });
  handle.addEventListener("pointermove", (e) => {
    const pe = e as PointerEvent;
    if (pe.pointerId !== dragPointer) return;
    const dy = pe.clientY - lastY;
    const t = now();
    if (t > lastT) speed = (dy / (t - lastT)) * 1000;
    lastY = pe.clientY;
    lastT = t;
    dragBy(dy);
  });
  const release = (e: Event): void => {
    if ((e as PointerEvent).pointerId !== dragPointer) return;
    dragPointer = undefined;
    dragEnd(speed);
  };
  handle.addEventListener("pointerup", release);
  handle.addEventListener("pointercancel", release);
  hide.addEventListener("click", () => setCollapsed(true));
  bar.addEventListener("click", () => setCollapsed(false));
  sys.addEventListener("click", () => o.onSystemKeyboard?.());
  // pressing anything in the dock must not take the focus from the surface
  const keepFocus = (e: Event): void => e.preventDefault();
  root.addEventListener("mousedown", keepFocus);
  bar.addEventListener("pointerdown", keepFocus);
  sys.addEventListener("pointerdown", keepFocus);
  hide.addEventListener("pointerdown", keepFocus);

  // a hardware key on the surface: an external keyboard is in use
  const onHardKey = (e: Event): void => {
    const k = e as KeyboardEvent;
    if (!k.isTrusted || !k.key || k.key === "Unidentified" || k.key === "Process" || k.isComposing) return;
    setCollapsed(true);
  };

  layout();

  return {
    el: root,
    keyboard,
    suggestions,
    get attached() {
      return attached;
    },
    get collapsed() {
      return collapsed;
    },
    get dragY() {
      return dragY;
    },
    get kbHeight() {
      return kbHeight();
    },
    attach(target) {
      if (attached !== target) {
        attached?.removeEventListener("keydown", onHardKey);
        attached = target;
        target.addEventListener("keydown", onHardKey);
      }
      root.hidden = false;
      setCollapsed(false);
      layout();
    },
    detach(target) {
      if (attached !== target) return;
      target.removeEventListener("keydown", onHardKey);
      attached = null;
      root.hidden = true;
    },
    toggleCollapsed: () => setCollapsed(!collapsed),
    setCollapsed,
    setHeight(h) {
      height = Math.max(MIN_HEIGHT, Math.min(MAX_HEIGHT, h));
      layout();
    },
    dragBy,
    dragEnd,
    setOptions(p) {
      o = { ...o, ...p };
      const { height: h, ...rest } = p;
      keyboard.setOptions(rest);
      if (h !== undefined) height = Math.max(MIN_HEIGHT, Math.min(MAX_HEIGHT, h));
      layout();
    },
    refit: layout,
    destroy() {
      stopSpring();
      attached?.removeEventListener("keydown", onHardKey);
      keyboard.destroy();
      root.remove();
    },
  };
}

/** threeFingerToggle: a three-finger tap on el toggles the dock (once per
 * gesture). Returns a function that removes the listeners. */
export function threeFingerToggle(el: HTMLElement, dock: Pick<Dock, "toggleCollapsed">): () => void {
  let fired = false;
  const start = (e: Event): void => {
    const n = (e as TouchEvent).touches?.length ?? 0;
    if (n >= 3 && !fired) {
      fired = true;
      dock.toggleCollapsed();
    }
  };
  const end = (e: Event): void => {
    if (((e as TouchEvent).touches?.length ?? 0) === 0) fired = false;
  };
  el.addEventListener("touchstart", start, { passive: true });
  el.addEventListener("touchend", end);
  el.addEventListener("touchcancel", end);
  return () => {
    el.removeEventListener("touchstart", start);
    el.removeEventListener("touchend", end);
    el.removeEventListener("touchcancel", end);
  };
}
