// The offline bar under the strip (web UX 3.6). Lost: amber, a countdown
// and Try now. Back: green for 2 s. Final: the session is over; the text
// says why and that the work goes on on the laptop; no retry.
import { h, type ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { Meta } from "../../../../webproto/proto";
import { useNow } from "../lib/time";
import { link } from "../session";
import { t, tn, type StringKey } from "../strings";
import { Button } from "./Button";

/** finalText is what a close code means for the user (web UX 7.4 E-texts). */
export function finalText(code: number | null): { main: StringKey; sub?: StringKey; reload?: boolean; limit?: "idle" | "max" } {
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
      return { main: "e.idle_any", sub: "e.still_running", limit: "idle" };
    case 4007:
      return { main: "e.max_any", sub: "e.still_running", limit: "max" };
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

/** limitText says which of the panel's limits ended the session, with
 * the laptop's own setting from the hub's meta (idle minutes, max
 * hours); without it, in general words. */
export function limitText(limit: "idle" | "max", m: Meta | null): string {
  if (limit === "idle") {
    const min = Math.round((m?.idle_s ?? 0) / 60);
    return min >= 1 ? tn("e.idle", min) : t("e.idle_any");
  }
  const hours = Math.round((m?.max_s ?? 0) / 3600);
  return hours >= 1 ? tn("e.max", hours) : t("e.max_any");
}

export type BarKind = "lost" | "back" | "final";

/** Bar is one line under the strip: amber lost, green back, red final.
 * spoken, when given, is what a screen reader hears instead of text
 * (a countdown would be read again every second). */
export function Bar({ kind, text, sub, action, spoken }: { kind: BarKind; text: string; sub?: string; action?: ComponentChildren; spoken?: string }) {
  return (
    <div class={`bar bar-${kind}`} role={kind === "final" ? "alert" : "status"}>
      {spoken && <span class="sr-only">{spoken}</span>}
      <span class="bar-text" aria-hidden={spoken ? "true" : undefined}>
        {text}
        {sub && <span class="bar-sub"> {sub}</span>}
      </span>
      {action}
    </div>
  );
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
      <Bar
        kind="final"
        text={f.limit ? limitText(f.limit, link.meta.value) : t(f.main)}
        sub={f.sub && t(f.sub)}
        action={
          f.reload && (
            <Button small onClick={() => location.reload()}>
              {t("e.reload")}
            </Button>
          )
        }
      />
    );
  }
  if (state === "offline" && down !== null && now - down >= SHOW_AFTER_MS) {
    const s = Math.max(0, Math.ceil((link.retryAt.value - now) / 1000));
    return (
      <Bar
        kind="lost"
        text={s > 0 ? t("offline.lost", { s }) : t("offline.trying")}
        spoken={t("offline.lost_sr")}
        action={
          <Button small onClick={() => link.tryNow()}>
            {t("offline.try")}
          </Button>
        }
      />
    );
  }
  if (state === "online" && showBack && back !== null && back >= SHOW_AFTER_MS) {
    return <Bar kind="back" text={t("offline.back", { s: Math.round(back / 1000) })} />;
  }
  return null;
}
