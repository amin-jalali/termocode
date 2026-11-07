// Phone states (MOBUX-21), drawn from data only (the kit shows each):
//   PhoneSkeleton  grey shapes while a block, a list, the cards or the
//                  board wait for their first data; "Still loading…"
//                  after 3 s
//   ConnLine       the bar under the top bar: offline (answers are not
//                  saved), laptop asleep with its last seen time, or the
//                  session is over; always a colour and a shape
//   ErrorLine      one calm line with ✗ and Retry, where the data was
import { h, type ComponentChildren } from "preact";
import { useAfter } from "../lib/time";
import { t } from "../strings";
import type { PhoneConn } from "./conn";

export type SkeletonShape = "rows" | "block" | "cards" | "board";

/** PhoneSkeleton keeps the place of the data it waits for, so nothing
 * jumps when the data comes. A screen reader hears "Loading" once. */
export function PhoneSkeleton({ shape = "rows", n = 4 }: { shape?: SkeletonShape; n?: number }) {
  const slow = useAfter(3000, shape);
  const items = Array.from({ length: Math.max(1, Math.min(n, 12)) }, (_, i) => i);
  let body: ComponentChildren;
  switch (shape) {
    case "block":
      body = (
        <div class="ph-sk-block">
          <span class="ph-sk ph-sk-line ph-sk-title" />
          {items.map((i) => (
            <span key={i} class="ph-sk ph-sk-line" />
          ))}
        </div>
      );
      break;
    case "cards":
      body = (
        <div class="ph-sk-cards">
          {items.slice(0, 3).map((i) => (
            <span key={i} class="ph-sk ph-sk-card" />
          ))}
        </div>
      );
      break;
    case "board":
      body = items.map((i) => <span key={i} class="ph-sk ph-sk-tile" />);
      break;
    default:
      body = items.map((i) => (
        <span key={i} class="ph-sk-row">
          <span class="ph-sk ph-sk-dot" />
          <span class="ph-sk ph-sk-line" />
        </span>
      ));
  }
  return (
    <div class={`ph-skel ph-skel-${shape}`} role="status" aria-busy="true">
      <span class="sr-only">{t("ph.loading")}</span>
      <div class="ph-skel-shapes" aria-hidden="true">
        {body}
      </div>
      {slow && (
        <span class="ph-skel-slow" aria-hidden="true">
          {t("loading.slow")}
        </span>
      )}
    </div>
  );
}

/** CONN_MARK is the shape of each bar: never colour alone. */
const CONN_MARK: Readonly<Record<"offline" | "asleep" | "final", string>> = {
  offline: "⊘",
  asleep: "◌",
  final: "✗",
};

/** ConnLine is the connection bar for one state. text is what it says;
 * spoken, when given, is what a screen reader hears instead (it is not
 * read again on every change). onTry, when given, adds Try now. */
export function ConnLine({
  conn,
  text,
  spoken,
  onTry,
}: {
  conn: Exclude<PhoneConn, "online" | "connecting">;
  text: string;
  spoken?: string;
  onTry?: () => void;
}) {
  return (
    <div class={`ph-conn ph-conn-${conn}`} role={conn === "final" ? "alert" : "status"}>
      {spoken && <span class="sr-only">{spoken}</span>}
      <span class="ph-conn-mark" aria-hidden="true">
        {CONN_MARK[conn]}
      </span>
      <span class="ph-conn-text" aria-hidden={spoken ? "true" : undefined}>
        {text}
      </span>
      {onTry && (
        <button type="button" class="ph-conn-act" onClick={onTry}>
          {t("offline.try")}
        </button>
      )}
    </div>
  );
}

/** ErrorLine says what could not be shown, in the place of the data,
 * with Retry. Offline, Retry is greyed (it could not work). */
export function ErrorLine({ text, onRetry, offline }: { text: string; onRetry?: () => void; offline?: boolean }) {
  return (
    <div class="ph-err" role="alert">
      <span class="ph-err-mark" aria-hidden="true">
        ✗
      </span>
      <span class="ph-err-text">{text}</span>
      {onRetry && (
        <button
          type="button"
          class="ph-err-retry ph-answer"
          aria-disabled={offline ? "true" : undefined}
          onClick={() => {
            if (!offline) onRetry();
          }}
        >
          {t("ph.retry")}
        </button>
      )}
    </div>
  );
}
