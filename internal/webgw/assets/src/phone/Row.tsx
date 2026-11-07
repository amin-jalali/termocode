// Row: one item in a block or a list. The whole row is the 44 px target
// when it can be tapped; the status shape leads, facts in mono below the
// title, one short fact at the end.
import { h, type ComponentChildren } from "preact";
import { StatusMark } from "./Mark";
import type { Status } from "./status";

export function Row({
  title,
  meta,
  end,
  status,
  live,
  onClick,
  onLongPress,
}: {
  title: ComponentChildren;
  meta?: ComponentChildren;
  end?: ComponentChildren;
  status?: Status;
  live?: boolean;
  onClick?: () => void;
  /** onLongPress opens the row's menu (the context menu event, so a long
   * press on a phone and a right click both work). */
  onLongPress?: () => void;
}) {
  const menu = onLongPress
    ? (e: Event) => {
        e.preventDefault();
        onLongPress();
      }
    : undefined;
  return onClick ? (
    <button type="button" class="ph-row" onClick={onClick} onContextMenu={menu}>
      {status && <StatusMark s={status} live={live} />}
      <span class="ph-row-main">
        <span class="ph-row-title">{title}</span>
        {meta && <span class="ph-row-meta">{meta}</span>}
      </span>
      {end && <span class="ph-row-end">{end}</span>}
    </button>
  ) : (
    <div class="ph-row" onContextMenu={menu}>
      {status && <StatusMark s={status} live={live} />}
      <span class="ph-row-main">
        <span class="ph-row-title">{title}</span>
        {meta && <span class="ph-row-meta">{meta}</span>}
      </span>
      {end && <span class="ph-row-end">{end}</span>}
    </div>
  );
}

/** Rows is a list of rows with lines between them. */
export function Rows({ children, label }: { children: ComponentChildren; label?: string }) {
  return (
    <div class="ph-rows" role="group" aria-label={label}>
      {children}
    </div>
  );
}
