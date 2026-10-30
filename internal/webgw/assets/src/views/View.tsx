// The frame every view shares: a sticky header and a scrolling body.
import { h, type ComponentChildren } from "preact";
import type { Ref } from "preact/hooks";
import type { Tab } from "../state/route";

export function View({
  tab,
  title,
  sub,
  children,
  foot,
  bodyRef,
  actions,
}: {
  tab: Tab;
  title: string;
  sub?: ComponentChildren;
  children: ComponentChildren;
  /** foot stays under the scrolling body (the chat composer). */
  foot?: ComponentChildren;
  bodyRef?: Ref<HTMLDivElement>;
  /** actions sit at the end of the header (small buttons). */
  actions?: ComponentChildren;
}) {
  return (
    <section class="view" data-view={tab} aria-labelledby={`vh-${tab}`}>
      <header class="view-head">
        <h1 class="view-title" id={`vh-${tab}`}>
          {title}
        </h1>
        {sub && <div class="view-sub">{sub}</div>}
        {actions && <div class="view-actions">{actions}</div>}
      </header>
      <div class="view-body" ref={bodyRef}>
        {children}
      </div>
      {foot}
    </section>
  );
}
