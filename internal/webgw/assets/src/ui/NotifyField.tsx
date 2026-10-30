// The notifications switch of the More tab (WEB-37). The browser asks
// for the permission only after "Notify me" is pressed.
import { h } from "preact";
import { notifyState, perm, turnOff, turnOn } from "../lib/notify";
import { t } from "../strings";

export function NotifyField() {
  void perm.value; // redraw once the browser answered
  const st = notifyState();
  if (st === "unsupported") return null;
  return (
    <div class="field notify">
      <span class="field-label" id="notify-t">
        {t("notify.title")}
      </span>
      <p class="group-text">{st === "blocked" ? t("notify.blocked") : t("notify.body")}</p>
      {st === "off" && (
        <button type="button" class="btn btn-neutral notify-on" aria-describedby="notify-t" onClick={turnOn}>
          {t("notify.on")}
        </button>
      )}
      {st === "on" && (
        <button type="button" class="btn btn-quiet notify-off" aria-describedby="notify-t" onClick={turnOff}>
          {t("notify.off")}
        </button>
      )}
    </div>
  );
}
