// A one-time hint (web UX 3.1 step 4): one slim line with a close
// button at its end, in the page's flow, so it never covers anything.
import { h } from "preact";
import { prefs, setPref } from "../lib/prefs";
import { t } from "../strings";
import { IconClose } from "./icons";

export function FirstRunHint() {
  if (prefs.value.firstRunDone) return null;
  return (
    <div class="hint" role="note">
      <span class="star" aria-hidden="true">✦</span>
      <span class="hint-text">{t("firstrun")}</span>
      <button type="button" class="hint-x" aria-label={t("firstrun.ok")} title={t("firstrun.ok")} onClick={() => setPref("firstRunDone", true)}>
        <IconClose />
      </button>
    </div>
  );
}
