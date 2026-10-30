// Sheets (web UX 7.3): a bottom sheet on phones, a centred dialog from
// 720 px. Dragging the handle down, tapping the dim area or Esc mean
// Later: they close the sheet and never answer anything. Focus stays
// inside while open and goes back where it was when it closes.
import { h, type ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";

const CLOSE_DRAG_PX = 80;
const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function Sheet({ onLater, children, label }: { onLater: () => void; children: ComponentChildren; label?: string }) {
  const panel = useRef<HTMLDivElement | null>(null);
  const [drag, setDrag] = useState(0);
  const start = useRef<number | null>(null);

  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    const p = panel.current;
    if (p && !p.contains(document.activeElement)) p.querySelector<HTMLElement>(FOCUSABLE)?.focus();
    return () => before?.focus?.();
  }, []);

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      onLater();
      return;
    }
    if (e.key !== "Tab" || !panel.current) return;
    const f = Array.from(panel.current.querySelectorAll<HTMLElement>(FOCUSABLE));
    if (f.length === 0) return;
    const first = f[0], last = f[f.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  };

  const down = (e: PointerEvent) => {
    start.current = e.clientY;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  };
  const move = (e: PointerEvent) => {
    if (start.current !== null) setDrag(Math.max(0, e.clientY - start.current));
  };
  const up = () => {
    if (start.current === null) return;
    start.current = null;
    if (drag > CLOSE_DRAG_PX) onLater();
    setDrag(0);
  };

  return (
    <div class="sheet-layer" onKeyDown={onKeyDown}>
      <div class="scrim" onClick={onLater} />
      <div
        ref={panel}
        class={`sheet${drag > 0 ? " is-dragging" : ""}`}
        role="presentation"
        aria-label={label}
        style={drag > 0 ? { transform: `translateY(${drag}px)` } : undefined}
      >
        <div class="grip" onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={up} aria-hidden="true">
          <span class="grip-bar" />
        </div>
        <div class="sheet-body">{children}</div>
      </div>
    </div>
  );
}
