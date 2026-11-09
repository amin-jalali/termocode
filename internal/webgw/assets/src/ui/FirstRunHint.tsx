// A one-time hint (web UX 3.1 step 4): one slim line with a close
// button at its end, in the page's flow, so it never covers anything.
// It shows on the first visit only: seen once, it is done (a phone's
// screen goes to the work, not to a hint read before).
import { h } from "preact";
import { useEffect } from "preact/hooks";
import { signal } from "@preact/signals";
import { prefs, setPref } from "../lib/prefs";
import { t } from "../strings";
import { IconClose } from "./icons";

/** shown: the hint is on this visit's screen (its first). */
const shown = signal(!prefs.peek().firstRunDone);

export function FirstRunHint() {
  useEffect(() => {
    if (shown.peek()) setPref("firstRunDone", true);
  }, []);
  if (!shown.value) return null;
  return (
    <div class="hint" role="note">
      <span class="star" aria-hidden="true">✦</span>
      <span class="hint-text">{t("firstrun")}</span>
      <button type="button" class="hint-x" aria-label={t("firstrun.ok")} title={t("firstrun.ok")} onClick={() => (shown.value = false)}>
        <IconClose />
      </button>
    </div>
  );
}
