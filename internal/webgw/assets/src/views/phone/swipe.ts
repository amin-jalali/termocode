// A sideways swipe (MOBUX-11): a finger that moves mostly sideways by
// SWIPE_PX or more and lifts calls onSwipe(-1) for a swipe to the left
// (the next item) or onSwipe(1) to the right (the one before). An up or
// down move is a scroll and is left alone; a mouse never swipes (the
// ‹ › buttons are there for it). The area sets touch-action: pan-y, so
// the browser scrolls up and down and leaves sideways moves to us.
import { useEffect, useRef } from "preact/hooks";

export const SWIPE_PX = 60;

export function useSwipe(el: { current: HTMLElement | null }, onSwipe: (dir: -1 | 1) => void): void {
  const cb = useRef(onSwipe);
  cb.current = onSwipe;
  useEffect(() => {
    const node = el.current;
    if (!node) return;
    let at: { x: number; y: number; id: number } | null = null;
    const down = (e: PointerEvent) => {
      if (e.pointerType === "mouse" || !e.isPrimary) return;
      at = { x: e.clientX, y: e.clientY, id: e.pointerId };
    };
    const up = (e: PointerEvent) => {
      if (!at || e.pointerId !== at.id) return;
      const dx = e.clientX - at.x, dy = e.clientY - at.y;
      at = null;
      if (Math.abs(dx) >= SWIPE_PX && Math.abs(dx) > 2 * Math.abs(dy)) cb.current(dx < 0 ? -1 : 1);
    };
    const cancel = () => {
      at = null;
    };
    node.addEventListener("pointerdown", down);
    node.addEventListener("pointerup", up);
    node.addEventListener("pointercancel", cancel);
    return () => {
      node.removeEventListener("pointerdown", down);
      node.removeEventListener("pointerup", up);
      node.removeEventListener("pointercancel", cancel);
    };
  }, [el.current]);
}
