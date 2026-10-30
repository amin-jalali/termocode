// The attention pill: one line above the tab bar on every tab, the "now"
// without a Home tab (web UX 4.1). It shows the most urgent thing only.
import { h } from "preact";
import { goTab } from "../lib/router";
import { status } from "../session";
import { t, tn } from "../strings";

export function AttentionPill() {
  const s = status.value;
  if (s.cardsWaiting === 0) return null;
  return (
    <div class="pill-wrap">
      <button type="button" class="attention" onClick={() => goTab("chat")}>
        <span class="star" aria-hidden="true">✦</span>
        <span class="attention-text">{tn("pill.cards", s.cardsWaiting)}</span>
        <span class="attention-go">
          {t("pill.review")} <span aria-hidden="true">›</span>
        </span>
      </button>
    </div>
  );
}
