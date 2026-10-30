// Waiting for the laptop (web UX 3.9): a destructive action asks the
// laptop, which shows its own card. Calm line, elapsed time, Cancel.
import { h } from "preact";
import { useNow } from "../lib/time";
import { t } from "../strings";

function clock(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

export function WaitingLaptop({ since, onCancel }: { since: number; onCancel: () => void }) {
  const now = useNow(true, 1000);
  return (
    <div class="waiting" role="status">
      <div class="waiting-head">
        <span class="spinner" aria-hidden="true" />
        <span class="waiting-title">{t("wait.title")}</span>
        <span class="waiting-time">{clock(now - since)}</span>
      </div>
      <p class="waiting-body">{t("wait.body")}</p>
      <button type="button" class="btn btn-neutral btn-wide" onClick={onCancel}>
        {t("cancel")}
      </button>
    </div>
  );
}
