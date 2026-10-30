// A one-time hint under the strip (web UX 3.1 step 4).
import { h } from "preact";
import { prefs, setPref } from "../lib/prefs";
import { t } from "../strings";

export function FirstRunHint() {
  if (prefs.value.firstRunDone) return null;
  return (
    <div class="hint" role="note">
      <span class="star" aria-hidden="true">✦</span>
      <span class="hint-text">{t("firstrun")}</span>
      <button type="button" class="btn btn-quiet btn-small" onClick={() => setPref("firstRunDone", true)}>
        {t("firstrun.ok")}
      </button>
    </div>
  );
}
