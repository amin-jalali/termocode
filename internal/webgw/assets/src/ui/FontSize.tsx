// The rows' text size as buttons (WEB-36): the twin of the pinch.
import { h } from "preact";
import { prefs, ROW_MAX, ROW_MIN } from "../lib/prefs";
import { resetRowPx, rowPx, setRowPx } from "../lib/rowfont";
import { t } from "../strings";

export function FontSize() {
  const custom = prefs.value.rowPx !== 0;
  const px = rowPx();
  return (
    <div class="field font-size" role="group" aria-labelledby="font-size-t">
      <span class="field-label" id="font-size-t">
        {t("term.font")}
      </span>
      <div class="font-row">
        <button type="button" class="key font-smaller" aria-label={t("font.smaller")} disabled={px <= ROW_MIN} onClick={() => setRowPx(px - 1)}>
          {t("key.font_smaller")}
        </button>
        <span class="font-now" aria-live="polite">
          {t("font.px", { n: px })}
        </span>
        <button type="button" class="key font-bigger" aria-label={t("font.bigger")} disabled={px >= ROW_MAX} onClick={() => setRowPx(px + 1)}>
          {t("key.font_bigger")}
        </button>
        <button type="button" class="key font-reset" disabled={!custom} onClick={resetRowPx}>
          {t("font.reset")}
        </button>
      </div>
    </div>
  );
}
