// PhoneCard: a summary card of the Home (needs you · working · done
// today): status shape and label, a big number, one line of detail.
// Tapping it opens the list behind it.
import { h } from "preact";
import { t } from "../strings";
import { StatusMark } from "./Mark";
import { STATUS, type Status } from "./status";

export function PhoneCard({
  status,
  title,
  value,
  detail,
  onClick,
}: {
  status: Status;
  /** title replaces the status label when given (data, already in words). */
  title?: string;
  value?: string | number;
  detail?: string;
  onClick?: () => void;
}) {
  const head = title ?? t(STATUS[status].label);
  const inner = (
    <>
      <span class="ph-card-head">
        <StatusMark s={status} hidden />
        <span>{head}</span>
      </span>
      {value !== undefined && <span class="ph-card-value">{value}</span>}
      {detail && <span class="ph-card-detail">{detail}</span>}
    </>
  );
  const cls = `ph-card tone-${STATUS[status].tone}`;
  return onClick ? (
    <button type="button" class={cls} onClick={onClick}>
      {inner}
    </button>
  ) : (
    <div class={cls} role="group" aria-label={head}>
      {inner}
    </div>
  );
}
