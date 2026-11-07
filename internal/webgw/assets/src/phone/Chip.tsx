// PhoneChip: a context chip in the row above the composer. A count shows
// how many items wait; "needs" draws the chip with the needs colour and
// shape; a pinned chip shows a pin (the user pins up to 3).
import { h, type ComponentChildren } from "preact";
import { StatusMark } from "./Mark";
import type { Status } from "./status";

export function PhoneChip({
  label,
  count,
  status,
  on,
  pinned,
  onClick,
}: {
  label: string;
  count?: number;
  status?: Status;
  on?: boolean;
  pinned?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      class={`ph-chip${on ? " is-on" : ""}${status === "needs" ? " is-needs" : ""}`}
      aria-pressed={on ? "true" : undefined}
      onClick={onClick}
    >
      {pinned && (
        <span class="ph-chip-pin" aria-hidden="true">
          📌
        </span>
      )}
      {status && <StatusMark s={status} />}
      <span>{label}</span>
      {count !== undefined && count > 0 && <span class="ph-chip-count">{count}</span>}
    </button>
  );
}

/** PhoneChips is the sideways-scrolling row of chips. */
export function PhoneChips({ children, label }: { children: ComponentChildren; label?: string }) {
  return (
    <div class="ph-chips" role="toolbar" aria-label={label}>
      {children}
    </div>
  );
}
