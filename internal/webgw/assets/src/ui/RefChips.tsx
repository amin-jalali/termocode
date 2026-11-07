// The refs waiting above the composer (MOBUX-19): one chip per ref with
// its label; a tap takes it out. They go with the next Send.
import { h } from "preact";
import { composerRefs, removeRef } from "../lib/tochat";
import { t } from "../strings";

export function RefChips() {
  const refs = composerRefs.value;
  if (!refs.length) return null;
  return (
    <ul class="ref-chips" aria-label={t("tochat.list", { n: refs.length })}>
      {refs.map((c) => (
        <li key={c.key}>
          <button type="button" class="ref-chip" aria-label={t("tochat.remove", { name: c.label })} onClick={() => removeRef(c.key)}>
            <span class="ref-chip-kind" aria-hidden="true">↩</span>
            <span class="ref-chip-label" dir="auto">{c.label}</span>
            <span class="ref-chip-x" aria-hidden="true">×</span>
          </button>
        </li>
      ))}
    </ul>
  );
}
