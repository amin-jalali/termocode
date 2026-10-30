// A view whose data comes from topics: skeleton until the first snapshot,
// then the view's own content (later tasks) or its empty state.
import { h, type ComponentChildren } from "preact";
import { useEffect } from "preact/hooks";
import { link, store } from "../session";
import { Progress, SkeletonBubbles, SkeletonRows } from "../ui/Skeleton";

function isEmpty(data: unknown): boolean {
  if (data === null || data === undefined) return true;
  if (Array.isArray(data)) return data.length === 0;
  if (typeof data === "object") return Object.keys(data as object).length === 0;
  return false;
}

export function TopicBody({
  topic,
  also = [],
  empty,
  bubbles,
  children,
}: {
  topic: string;
  also?: string[];
  empty: ComponentChildren;
  bubbles?: boolean;
  children?: ComponentChildren;
}) {
  useEffect(() => link.subscribe(topic, ...also), [topic]);
  const st = store.topic(topic).value;
  if (!st) {
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
