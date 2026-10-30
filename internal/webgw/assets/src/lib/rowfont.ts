// The text size of terminal and file rows (WEB-36): two fingers on the
// rows change it, the ⓘ sheet has buttons for the same (WCAG 2.5.7). It
// is a per-device preference; the page's own zoom is left alone.
import { effect } from "@preact/signals";
import { useEffect } from "preact/hooks";
import { prefs, ROW_MAX, ROW_MIN, setPref } from "./prefs";

/** defaultRowPx is the rows' size from the style sheet (it changes with the layout). */
export function defaultRowPx(): number {
  const html = document.documentElement;
  const v = getComputedStyle(html).getPropertyValue("--fs-row").trim();
  const rem = parseFloat(getComputedStyle(html).fontSize) || 16;
  const n = v.endsWith("rem") ? parseFloat(v) * rem : parseFloat(v);
  return Number.isFinite(n) && n > 0 ? Math.round(n) : 13;
}

/** rowPx is the rows' size now. */
export function rowPx(): number {
  return prefs.value.rowPx || defaultRowPx();
}

/** resetRowPx goes back to the style sheet's size. */
export function resetRowPx(): void {
  setPref("rowPx", 0);
}

/** setRowPx keeps px within bounds; the default size is stored as 0. */
export function setRowPx(px: number): void {
  const n = Math.min(ROW_MAX, Math.max(ROW_MIN, Math.round(px)));
  setPref("rowPx", n === defaultRowPx() ? 0 : n);
}

effect(() => {
  const px = prefs.value.rowPx;
  const html = document.documentElement.style;
  if (px) {
    html.setProperty("--row-zoom", px + "px");
    html.setProperty("--row-zoom-lh", Math.round(px * 1.5) + "px");
  } else {
    html.removeProperty("--row-zoom");
    html.removeProperty("--row-zoom-lh");
  }
});

function gap(t: TouchList): number {
  return Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY);
}

/** usePinch lets two fingers on el change the rows' size. */
export function usePinch(el: { current: HTMLElement | null }): void {
  useEffect(() => {
    const node = el.current;
    if (!node) return;
    let start = 0;
    let base = 0;
    const down = (e: TouchEvent) => {
      if (e.touches.length !== 2) return;
      start = gap(e.touches);
      base = rowPx();
    };
    const move = (e: TouchEvent) => {
      if (e.touches.length !== 2 || start <= 0) return;
      e.preventDefault(); // ours, not the page's zoom
      setRowPx(base * (gap(e.touches) / start));
    };
    const up = (e: TouchEvent) => {
      if (e.touches.length < 2) start = 0;
    };
    node.addEventListener("touchstart", down, { passive: true });
    node.addEventListener("touchmove", move, { passive: false });
    node.addEventListener("touchend", up, { passive: true });
    node.addEventListener("touchcancel", up, { passive: true });
    return () => {
      node.removeEventListener("touchstart", down);
      node.removeEventListener("touchmove", move);
      node.removeEventListener("touchend", up);
      node.removeEventListener("touchcancel", up);
    };
  }, [el.current]);
}
