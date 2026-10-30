// A view whose data comes from topics: skeleton until the first snapshot,
// then the view's own content or its empty state. A topic the hub refused
// (no workspace, a closed terminal) shows why, with Try again, instead of
// a skeleton that never ends.
import { h, type ComponentChildren } from "preact";
import { useEffect } from "preact/hooks";
import type { Refusal } from "../net/link";
import { link, store } from "../session";
import { t } from "../strings";
import { Button } from "../ui/Button";
import { Progress, SkeletonBubbles, SkeletonRows } from "../ui/Skeleton";

function isEmpty(data: unknown): boolean {
  if (data === null || data === undefined) return true;
  if (Array.isArray(data)) return data.length === 0;
  if (typeof data === "object") return Object.keys(data as object).length === 0;
  return false;
}

/** Refused says why the hub would not send a topic, with Try again. */
export function Refused({ topic, children }: { topic: string; children: ComponentChildren }) {
  return (
    <div class="empty refused" role="status">
      <p dir="auto">{children}</p>
      <Button kind="neutral" small needsLink onClick={() => link.retry(topic)}>
        {t("retry")}
      </Button>
    </div>
  );
}

export function TopicBody({
  topic,
  also = [],
  empty,
  refused,
  bubbles,
  children,
}: {
  topic: string;
  also?: string[];
  empty: ComponentChildren;
  /** refused draws the hub's refusal; by default its own words. */
  refused?: (r: Refusal) => ComponentChildren;
  bubbles?: boolean;
  children?: ComponentChildren;
}) {
  useEffect(() => link.subscribe(topic, ...also), [topic]);
  const st = store.topic(topic).value;
  if (!st) {
    const r = link.refused.value.get(topic);
    if (r) return <Refused topic={topic}>{refused ? refused(r) : r.msg}</Refused>;
    return (
      <div class="loading">
        <Progress k={topic} />
        {bubbles ? <SkeletonBubbles /> : <SkeletonRows rows={10} />}
      </div>
    );
  }
  return (
    <div class={`topic${st.stale ? " is-stale" : ""}`}>
      {isEmpty(st.data) || !children ? <div class="empty">{empty}</div> : children}
    </div>
  );
}
