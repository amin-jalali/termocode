// Long press and right click (WEB-36): one gesture that opens a row's
// menu. A finger held still for LONG_MS, or the context menu event (a
// right click, or a long press on browsers that send one), calls onLong
// with the element pressed. Moving the finger is a scroll, not a press.
import { useEffect } from "preact/hooks";

export const LONG_MS = 450;
const MOVE_PX = 10;
/** CLICK_AFTER_MS: a click this soon after the finger lifts is that finger's. */
const CLICK_AFTER_MS = 300;

/** swallowTap drops what the lifted finger sends next (the mouse events
 * and the click a browser makes of a touch): the menu now lies under it,
 * and that must not pick, close or move focus. */
function swallowTap(node: HTMLElement, x: number, y: number): void {
  const end = (e: TouchEvent) => {
    e.preventDefault(); // no mouse events, no click
    setTimeout(off, CLICK_AFTER_MS);
  };
  const drop = (e: MouseEvent) => {
    if (Math.hypot(e.clientX - x, e.clientY - y) > MOVE_PX) return; // another tap
    e.stopPropagation();
    e.preventDefault();
    off();
  };
  const off = () => {
    node.removeEventListener("touchend", end);
    document.removeEventListener("click", drop, true);
  };
  node.addEventListener("touchend", end, { once: true, passive: false });
  document.addEventListener("click", drop, true);
}

/** useLongPress listens on el, which must exist when the caller mounts. */
export function useLongPress(el: { current: HTMLElement | null }, onLong: (target: Element) => void): void {
  useEffect(() => {
    const node = el.current;
    if (!node) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let at: { x: number; y: number } | null = null;
    let fired = false;
    const stop = () => {
      clearTimeout(timer);
      at = null;
    };
    const down = (e: PointerEvent) => {
      if (e.pointerType === "mouse" || !e.isPrimary) return;
      stop();
      fired = false;
      at = { x: e.clientX, y: e.clientY };
      const target = e.target as Element;
      const x = e.clientX, y = e.clientY;
      timer = setTimeout(() => {
        at = null;
        fired = true;
        swallowTap(node, x, y);
        onLong(target);
      }, LONG_MS);
    };
    const move = (e: PointerEvent) => {
      if (at && Math.hypot(e.clientX - at.x, e.clientY - at.y) > MOVE_PX) stop();
    };
    const menu = (e: MouseEvent) => {
      if (!(e.target instanceof Element) || !e.target.closest(".trow, .crow")) return; // elsewhere: the browser's own menu
      e.preventDefault();
      if (fired) return; // the timer opened it already
      stop();
      onLong(e.target as Element);
    };
    node.addEventListener("pointerdown", down);
    node.addEventListener("pointermove", move);
    node.addEventListener("pointerup", stop);
    node.addEventListener("pointercancel", stop);
    node.addEventListener("contextmenu", menu);
    return () => {
      stop();
      node.removeEventListener("pointerdown", down);
      node.removeEventListener("pointermove", move);
      node.removeEventListener("pointerup", stop);
      node.removeEventListener("pointercancel", stop);
      node.removeEventListener("contextmenu", menu);
    };
  }, [el.current]);
}
