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
  back,
  bodyClass,
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
  /** back draws a back button before the title, labelled with where it goes. */
  back?: { label: string; onClick: () => void };
  bodyClass?: string;
}) {
  return (
    <section class="view" data-view={tab} aria-labelledby={`vh-${tab}`}>
      <header class="view-head">
        {back && (
          <button type="button" class="head-back" aria-label={back.label} onClick={back.onClick}>
            <span aria-hidden="true">‹</span>
          </button>
        )}
        <h1 class="view-title" id={`vh-${tab}`}>
          {title}
        </h1>
        {sub && <div class="view-sub">{sub}</div>}
        {actions && <div class="view-actions">{actions}</div>}
      </header>
      <div class={`view-body${bodyClass ? " " + bodyClass : ""}`} ref={bodyRef}>
        {children}
      </div>
      {foot}
    </section>
  );
}
