// Skeletons (web UX 7.3): grey rows while a view waits for its snapshot.
// After 1 s a thin progress line appears, after 3 s it says so (P4).
import { h } from "preact";
import { useAfter } from "../lib/time";
import { t } from "../strings";

export function SkeletonRows({ rows = 8, mono = true }: { rows?: number; mono?: boolean }) {
  return (
    <div class={`skeleton${mono ? " skeleton-mono" : ""}`} aria-hidden="true">
      {Array.from({ length: rows }, (_, i) => (
        <span key={i} class="sk-row" />
      ))}
    </div>
  );
}

export function SkeletonBubbles() {
  return (
    <div class="skeleton skeleton-chat" aria-hidden="true">
      <span class="sk-bubble" />
      <span class="sk-bubble sk-me" />
      <span class="sk-bubble sk-long" />
    </div>
  );
}

/** Progress shows nothing for 1 s, then a moving line, then "Still loading…" after 3 s. */
export function Progress({ k }: { k?: unknown }) {
  const shown = useAfter(1000, k);
  const slow = useAfter(3000, k);
  if (!shown) return null;
  return (
    <div class="progress" role="status">
      <span class="progress-line" />
      <span class="progress-text">{slow ? t("loading.slow") : t("loading")}</span>
    </div>
  );
}
