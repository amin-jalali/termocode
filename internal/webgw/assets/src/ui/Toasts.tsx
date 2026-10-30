// Toast stack (web UX 7.3): a severity hairline, title, detail; at most 3,
// older ones fold into "+N". role=status so screen readers hear them.
//
// Where: in the middle column, just under its view head, at the end side
// (full width on a phone). So a toast never covers the composer, the tab
// bar or the thumb zone on a phone, nor the chat column and its composer
// or a view's head buttons on a tablet or desktop.
import { h } from "preact";
import { useLayoutEffect, useRef } from "preact/hooks";
import { layout } from "../lib/layout";
import { route } from "../lib/router";
import { dismiss, toasts, type Toast } from "../lib/toasts";
import { t } from "../strings";

const MAX_SHOWN = 3;
const GAP_PX = 8; // --s2

/** place puts the stack under the middle column's head, at its end edge
 * (through the CSSOM: the page has no style attributes). */
function place(el: HTMLElement): void {
  const main = document.querySelector<HTMLElement>(".main");
  if (!main) return;
  const m = main.getBoundingClientRect();
  const head = main.querySelector<HTMLElement>(".view-head");
  const top = (head ? head.getBoundingClientRect().bottom : m.top) + GAP_PX;
  const rtl = getComputedStyle(main).direction === "rtl";
  el.style.setProperty("--toast-top", Math.round(top) + "px");
  el.style.setProperty("--toast-end", Math.max(0, Math.round(rtl ? m.left : document.documentElement.clientWidth - m.right)) + "px");
}

export function Toasts() {
  const all = toasts.value;
  void route.value; // a new view may have a head of another height
  void layout.value;
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    place(el);
    const again = () => place(el);
    addEventListener("resize", again);
    return () => removeEventListener("resize", again);
  });
  if (all.length === 0) return null;
  const shown = all.slice(-MAX_SHOWN);
  const hidden = all.length - shown.length;
  return (
    <div class="toasts" role="status" aria-live="polite" ref={ref}>
      {hidden > 0 && <div class="toast-more">{t("toast.more", { n: hidden })}</div>}
      {shown.map((x) => (
        <ToastView key={x.id} x={x} onClose={() => dismiss(x.id)} />
      ))}
    </div>
  );
}

/** ToastView draws one toast. */
export function ToastView({ x, onClose }: { x: Toast; onClose?: () => void }) {
  return (
    <div class={`toast sev-${x.severity}`}>
      <div class="toast-body">
        <div class="toast-title" dir="auto">
          {x.title}
        </div>
        {x.detail && (
          <div class="toast-detail" dir="auto">
            {x.detail}
          </div>
        )}
      </div>
      {x.action && (
        <button
          type="button"
          class="toast-act"
          onClick={() => {
            x.action?.run();
            onClose?.();
          }}
        >
          {x.action.label}
        </button>
      )}
      <button type="button" class="toast-x" aria-label={t("close")} onClick={onClose}>
        ×
      </button>
    </div>
  );
}
