// The attention pill: one line above the tab bar on every tab, the "now"
// without a Home tab (web UX 4.1). It shows the most urgent thing only.
import { h } from "preact";
import { goTab } from "../lib/router";
import { status } from "../session";
import { t, tn } from "../strings";

export type AttentionKind = "cards" | "run_done" | "run_failed" | "task_done" | "waiting_laptop";

/** AttentionView draws one pill; the kit shows every kind with it. */
export function AttentionView({ kind, n = 1, onClick }: { kind: AttentionKind; n?: number; onClick?: () => void }) {
  const text =
    kind === "cards"
      ? tn("pill.cards", n)
      : kind === "waiting_laptop"
        ? t("pill.waiting_laptop", { n })
        : t(kind === "run_done" ? "pill.run_done" : kind === "run_failed" ? "pill.run_failed" : "pill.task_done");
  const mark = kind === "cards" ? "✦" : kind === "waiting_laptop" ? "⏳" : kind === "run_failed" ? "✕" : "✓";
  return (
    <div class="pill-wrap">
      <button type="button" class={`attention att-${kind}`} onClick={onClick}>
        <span class={kind === "cards" ? "star" : "att-mark"} aria-hidden="true">
          {mark}
        </span>
        <span class="attention-text">{text}</span>
        {kind === "cards" && (
          <span class="attention-go">
            {t("pill.review")} <span aria-hidden="true">›</span>
          </span>
        )}
      </button>
    </div>
  );
}

export function AttentionPill() {
  const s = status.value;
  if (s.cardsWaiting === 0) return null;
  return <AttentionView kind="cards" n={s.cardsWaiting} onClick={() => goTab("chat")} />;
}
