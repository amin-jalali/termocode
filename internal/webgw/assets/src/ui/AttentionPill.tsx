// The attention pill: one line above the tab bar on every tab, the "now"
// without a Home tab (web UX 4.1). It shows the most urgent thing only.
import { h } from "preact";
import { clearNotice, notice } from "../lib/attention";
import { goTab } from "../lib/router";
import { waiting } from "../session";
import { openCards } from "./CardSheet";
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

/** AttentionPill shows the most urgent thing only: cards to answer, then
 * cards that wait for the laptop, then the newest run or task event. */
export function AttentionPill() {
  const w = waiting.value;
  const n = notice.value;
  if (w.answer.length > 0 || (w.count > 0 && w.laptop.length === 0)) {
    return <AttentionView kind="cards" n={w.answer.length || w.count} onClick={openCards} />;
  }
  if (w.laptop.length !== 0) {
    return <AttentionView kind="waiting_laptop" n={w.laptop.length} onClick={openCards} />;
  }
  if (n) {
    return (
      <AttentionView
        kind={n}
        onClick={() => {
          clearNotice();
          goTab("more");
        }}
      />
    );
  }
  return null;
}
