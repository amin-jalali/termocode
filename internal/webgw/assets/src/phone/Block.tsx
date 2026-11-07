// Block: one answer in the phone stream (a table, a list, a card, a
// diff …): a kind label, a title, an optional status, ⚙ Customize, the
// body and a row of actions at the foot.
import { h, type ComponentChildren } from "preact";
import { t } from "../strings";
import { StatusMark } from "./Mark";
import type { Status } from "./status";

export function Block({
  kind,
  title,
  status,
  live,
  onCustomize,
  actions,
  children,
}: {
  /** kind is the block kind in small caps (table, list, card …): data. */
  kind?: string;
  title: string;
  status?: Status;
  live?: boolean;
  onCustomize?: () => void;
  actions?: ComponentChildren;
  children: ComponentChildren;
}) {
  return (
    <section class="ph-block" aria-label={title}>
      <header class="ph-block-head">
        {status && <StatusMark s={status} live={live} />}
        {kind && (
          <span class="ph-block-kind" aria-hidden="true">
            {kind}
          </span>
        )}
        <span class="ph-block-title">{title}</span>
        {onCustomize && (
          <button type="button" class="ph-block-tool" aria-label={t("ph.customize")} onClick={onCustomize}>
            ⚙
          </button>
        )}
      </header>
      <div class="ph-block-body">{children}</div>
      {actions && <footer class="ph-block-foot">{actions}</footer>}
    </section>
  );
}
