// The Home's chip row (MOBUX-3): context chips above the composer, one
// line that scrolls sideways, in the phone shell's dock (MOBUX-2). The
// order comes from home/chips.ts. "Pin chips" at the end (or a long
// press on any chip) turns the row into pin mode: a tap then pins or
// unpins, up to 3, and Done ends it. A tap outside pin mode calls
// onPick; a chip never spends tokens. Status is the phone's colour and
// shape (StatusMark), never colour alone.
import { h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef } from "preact/hooks";
import { useLongPress } from "../lib/longpress";
import { toast } from "../lib/toasts";
import { StatusMark } from "../phone/Mark";
import type { Status } from "../phone/status";
import { t } from "../strings";
import "../strings.phone";
import { CHIPS, NEEDS, loadPins, orderChips, savePins, togglePin, type ChipDef, type ChipId, type ChipItem, type ChipMark, type ChipState } from "./chips";

/** pins are this phone's pinned chips; pinMode says the row is pinning. */
export const pins = signal<ChipId[]>(loadPins());
export const pinMode = signal(false);

const MARKS: Record<Exclude<ChipMark, "">, Status> = { waits: "needs", review: "review", busy: "working", bad: "failed", clear: "done" };

/** chipName is the chip's visible name. */
export function chipName(c: ChipItem): string {
  if (c.def.id === NEEDS) return c.count > 0 ? t("chips.needs", { n: c.count }) : t("chips.clear");
  return c.def.text ?? (c.def.label ? t(c.def.label) : c.def.id);
}

function pin(id: ChipId): void {
  const r = togglePin(pins.value, id);
  if (r.full) {
    toast("info", t("chips.full"));
    return;
  }
  pins.value = r.pins;
  savePins(r.pins);
}

function OneChip({ c, onPick }: { c: ChipItem; onPick: (id: ChipId) => void }) {
  const name = chipName(c);
  const editing = pinMode.value;
  const canPin = c.def.id !== NEEDS;
  const counted = c.def.id !== NEEDS && c.count > 0;
  const cls = `ph-chip hchip${c.mark ? " mark-" + c.mark : ""}${c.mark === "waits" ? " is-needs" : ""}${c.pinned ? " is-pinned" : ""}`;
  return (
    <button
      type="button"
      class={cls}
      data-chip={c.def.id}
      disabled={editing && !canPin}
      aria-pressed={editing && canPin ? (c.pinned ? "true" : "false") : undefined}
      aria-label={c.pinned ? t("chips.pinned_name", { name: counted ? `${name} ${c.count}` : name }) : undefined}
      onClick={() => (editing ? canPin && pin(c.def.id) : onPick(c.def.id))}
    >
      {c.pinned && (
        <span class="ph-chip-pin" aria-hidden="true">
          📌
        </span>
      )}
      {c.mark && <StatusMark s={MARKS[c.mark]} hidden />}
      <span>{name}</span>
      {counted && <span class="ph-chip-count">{c.count}</span>}
    </button>
  );
}

/** ChipRow draws the chips for state; defs adds saved views (MOBUX-18). */
export function ChipRow({ state, defs = CHIPS, onPick }: { state: ChipState; defs?: readonly ChipDef[]; onPick: (id: ChipId) => void }) {
  const row = useRef<HTMLDivElement>(null);
  const items = orderChips(defs, state, pins.value);
  const waits = state.needsYou > 0;
  const editing = pinMode.value;

  // Something new waits: bring the row back to its start, where
  // "Needs you" now is.
  useEffect(() => {
    if (waits && row.current) row.current.scrollLeft = 0;
  }, [waits]);

  useLongPress(row, (target) => {
    if (target.closest("[data-chip]")) pinMode.value = true;
  });

  return (
    <div class="hchips-wrap">
      {editing && (
        <p class="hchips-hint" role="status">
          {t("chips.body_hint")}
        </p>
      )}
      <div class={`ph-chips hchips${editing ? " is-pinning" : ""}`} ref={row} role="group" aria-label={t("chips.label")}>
        {editing && (
          <button type="button" class="ph-chip hchip is-done" onClick={() => (pinMode.value = false)}>
            {t("done")}
          </button>
        )}
        {items.map((c) => (
          <OneChip key={c.def.id} c={c} onPick={onPick} />
        ))}
        {!editing && (
          <button type="button" class="ph-chip hchip is-edit" onClick={() => (pinMode.value = true)}>
            <span class="ph-chip-pin" aria-hidden="true">
              📌
            </span>
            <span>{t("chips.edit")}</span>
          </button>
        )}
      </div>
    </div>
  );
}
