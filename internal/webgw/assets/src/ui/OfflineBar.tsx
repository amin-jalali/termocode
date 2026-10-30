// The offline bar under the strip (web UX 3.6). Lost: amber, a countdown
// and Try now. Back: green for 2 s. Final: the session is over; the text
// says why and that the work goes on on the laptop; no retry.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import { useNow } from "../lib/time";
import { link } from "../session";
import { t, type StringKey } from "../strings";
import { Button } from "./Button";

/** finalText is what a close code means for the user (web UX 7.4 E-texts). */
export function finalText(code: number | null): { main: StringKey; sub?: StringKey; reload?: boolean } {
  switch (code) {
    case 4001:
    case 4002:
      return { main: "e.link_expired" };
    case 4003:
      return { main: "e.device_removed" };
    case 4004:
      return { main: "e.taken_over", sub: "e.still_running" };
    case 4005:
      return { main: "e.too_many", sub: "e.still_running" };
    case 4006:
      return { main: "e.idle", sub: "e.still_running" };
    case 4007:
      return { main: "e.max", sub: "e.still_running" };
    case 4010:
    case 4012:
      return { main: "e.ended" };
    case 4011:
      return { main: "e.version", reload: true };
    case 1003:
    case 1008:
    case 1009:
      return { main: "err.internal", sub: "e.still_running" };
    default:
      return { main: "e.stopped", sub: "e.still_running" };
  }
}

const SHOW_AFTER_MS = 1000;
const BACK_FOR_MS = 2000;

export function OfflineBar() {
  const state = link.state.value;
  const down = link.downSince.value;
  const back = link.reconnectedAfter.value;
  const now = useNow(state === "offline");
  const [showBack, setShowBack] = useState(false);
  useEffect(() => {
    if (back === null) return;
    setShowBack(true);
    const id = setTimeout(() => setShowBack(false), BACK_FOR_MS);
    return () => clearTimeout(id);
  }, [back]);

  if (state === "final") {
    const f = finalText(link.finalCode.value);
    return (
      <div class="bar bar-final" role="alert">
        <span class="bar-text">
          {t(f.main)}
          {f.sub && <span class="bar-sub"> {t(f.sub)}</span>}
        </span>
        {f.reload && (
          <Button small onClick={() => location.reload()}>
            {t("e.reload")}
          </Button>
        )}
      </div>
    );
  }
  if (state === "offline" && down !== null && now - down >= SHOW_AFTER_MS) {
    const s = Math.max(0, Math.ceil((link.retryAt.value - now) / 1000));
    return (
      <div class="bar bar-lost" role="status">
        <span class="bar-text">{s > 0 ? t("offline.lost", { s }) : t("offline.trying")}</span>
        <Button small onClick={() => link.tryNow()}>
          {t("offline.try")}
        </Button>
      </div>
    );
  }
  if (state === "online" && showBack && back !== null && back >= SHOW_AFTER_MS) {
    return (
      <div class="bar bar-back" role="status">
        <span class="bar-text">{t("offline.back", { s: Math.round(back / 1000) })}</span>
      </div>
    );
  }
  return null;
}
