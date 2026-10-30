// The frame every view shares: a sticky header and a scrolling body.
import { h, type ComponentChildren, type Ref } from "preact";
import { Portal } from "../ui/Portal";
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
  name,
  actionsInto,
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
  /** name replaces tab in data-view and the title's id: the desktop shows
   * the tree beside a file, and data-view names the route's own view. */
  name?: string;
  /** actionsInto: the id of the element the actions go to (the desktop
   * editor group's tab bar), when it is there. */
  actionsInto?: string;
}) {
  const id = name ?? tab;
  return (
    <section class="view" data-view={id} aria-labelledby={`vh-${id}`}>
      <header class="view-head">
        {back && (
          <button type="button" class="head-back" aria-label={back.label} onClick={back.onClick}>
            <span aria-hidden="true">‹</span>
          </button>
        )}
        <h1 class="view-title" id={`vh-${id}`}>
          {title}
        </h1>
        {sub && <div class="view-sub">{sub}</div>}
        {actions && !actionsInto && <div class="view-actions">{actions}</div>}
        {actions && actionsInto && (
          <Portal into={actionsInto}>
            <div class="view-actions">{actions}</div>
          </Portal>
        )}
      </header>
      <div class={`view-body${bodyClass ? " " + bodyClass : ""}`} ref={bodyRef}>
        {children}
      </div>
      {foot}
    </section>
  );
}
