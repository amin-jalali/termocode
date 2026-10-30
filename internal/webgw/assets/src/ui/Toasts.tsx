// Toast stack (web UX 7.3): a severity hairline, title, detail; at most 3,
// older ones fold into "+N". role=status so screen readers hear them.
import { h } from "preact";
import { dismiss, toasts } from "../lib/toasts";
import { t } from "../strings";

const MAX_SHOWN = 3;

export function Toasts() {
  const all = toasts.value;
  if (all.length === 0) return null;
  const shown = all.slice(-MAX_SHOWN);
  const hidden = all.length - shown.length;
  return (
    <div class="toasts" role="status" aria-live="polite">
      {hidden > 0 && <div class="toast-more">{t("toast.more", { n: hidden })}</div>}
      {shown.map((x) => (
        <div key={x.id} class={`toast sev-${x.severity}`}>
          <div class="toast-body">
            <div class="toast-title" dir="auto">{x.title}</div>
            {x.detail && <div class="toast-detail" dir="auto">{x.detail}</div>}
          </div>
          <button type="button" class="toast-x" aria-label={t("toast.close")} onClick={() => dismiss(x.id)}>
            ×
          </button>
        </div>
      ))}
    </div>
  );
}
