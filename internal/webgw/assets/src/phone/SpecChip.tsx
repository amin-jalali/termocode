// SpecChip (MOBUX-17): under a view the AI built from a chat command, a
// chip says what was built ("table · Status, Total · Total ↓") and a tap
// opens the customizer (⚙, MOBUX-16) on that spec. The words are data
// from the spec, so they stay as the laptop wrote the ids and labels.
import { h } from "preact";
import type { Block, ViewSpec } from "../../../../webproto/proto";
import { t } from "../strings";
import "../strings.phone";
import { labelsOf, specSummary } from "./specsummary";

export function SpecChip({ block, onCustomize }: { block: Block; onCustomize: (s: ViewSpec, b: Block) => void }) {
  const spec = block.spec;
  if (!spec) return null;
  const what = specSummary(spec, labelsOf(block)) || spec.view;
  return (
    <button
      type="button"
      class="ph-chip ph-spec-chip"
      aria-label={t("ph.spec.open", { what })}
      onClick={() => onCustomize(spec, block)}
    >
      <span class="ph-spec-chip-mark" aria-hidden="true">
        ⚙
      </span>
      <span class="ph-spec-chip-lead">{t("ph.spec.built")}</span>
      <span class="ph-spec-chip-what">{what}</span>
    </button>
  );
}
