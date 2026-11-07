// BoardTile: one piece of work on the live board: project · agent or
// model · step · cost · CI. The status is the shape at the start and the
// coloured edge; tapping opens its logs, diff or chat.
import { h } from "preact";
import { t } from "../strings";
import { StatusMark } from "./Mark";
import { STATUS, type Status } from "./status";

export function BoardTile({
  status,
  title,
  project,
  agent,
  step,
  cost,
  ci,
  live,
  onClick,
}: {
  status: Status;
  title: string;
  project?: string;
  /** agent is the agent or model name (data). */
  agent?: string;
  /** step is the progress already in words, like "3/5". */
  step?: string;
  cost?: string;
  ci?: Status;
  live?: boolean;
  onClick?: () => void;
}) {
  const inner = (
    <>
      <StatusMark s={status} live={live} />
      <span class="ph-tile-title">{title}</span>
      {cost ? <span class="ph-tile-cost">{cost}</span> : <span />}
      <span class="ph-tile-facts">
        {project && <span class="ph-tile-project">{project}</span>}
        {agent && <span>{agent}</span>}
        {step && <span>{step}</span>}
        {ci && (
          <span aria-label={t("ph.checks", { state: t(STATUS[ci].label) })}>
            <StatusMark s={ci} hidden /> {t("ph.ci")}
          </span>
        )}
      </span>
    </>
  );
  const cls = `ph-tile tone-${STATUS[status].tone}`;
  return onClick ? (
    <button type="button" class={cls} onClick={onClick}>
      {inner}
    </button>
  ) : (
    <div class={cls}>{inner}</div>
  );
}
