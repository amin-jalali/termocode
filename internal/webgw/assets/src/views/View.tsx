// The frame every view shares: a sticky header and a scrolling body.
import { h, type ComponentChildren } from "preact";
import type { Tab } from "../state/route";

export function View({
  tab,
  title,
  sub,
  children,
}: {
  tab: Tab;
  title: string;
  sub?: ComponentChildren;
  children: ComponentChildren;
}) {
  return (
    <section class="view" data-view={tab} aria-labelledby={`vh-${tab}`}>
      <header class="view-head">
        <h1 class="view-title" id={`vh-${tab}`}>
          {title}
        </h1>
        {sub && <div class="view-sub">{sub}</div>}
      </header>
      <div class="view-body">{children}</div>
    </section>
  );
}
