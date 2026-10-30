// Small parts the Run, Test and Debug views share: toolbar icon buttons,
// a section that folds (the side bar's VARIABLES, WATCH …), a request
// that toasts its failure, and whether this page may act.
import { h, type ComponentChildren } from "preact";
import { useEffect, useRef } from "preact/hooks";
import { link, live } from "../../session";
import { Cod } from "../../ui/icons";
import { gitFailed } from "../git/errors";
import { t } from "../../strings";

/** canAct: this page controls the laptop (a viewer only looks). */
export function canAct(): boolean {
  return link.meta.value?.role !== "viewer";
}

/** ask sends one op; then hears its answer, a failure is a toast with
 * the hub's words. */
export function ask(op: string, args: unknown = {}, then?: (data: unknown) => void): void {
  link.request(op, args).then((d) => then?.(d), gitFailed);
}

/** IconBtn is a 22 px toolbar button with a codicon; its name is its
 * label and tooltip (with the keys when given). Off while offline. */
export function IconBtn({ icon, label, keys, act, onClick, off, on }: { icon: string; label: string; keys?: string; act?: string; onClick: () => void; off?: boolean; on?: boolean }) {
  const dead = off === true || !live.value;
  return (
    <button
      type="button"
      class={`tb tb-icon${on ? " is-on" : ""}`}
      data-act={act}
      aria-label={label}
      title={keys ? `${label} (${keys})` : label}
      aria-disabled={dead ? "true" : undefined}
      onClick={(e) => {
        e.stopPropagation();
        if (!dead) onClick();
      }}
    >
      <Cod name={icon} />
      <span class="tb-text">{label}</span>
    </button>
  );
}

/** Pane is one folding part of a side bar view: its head (with actions
 * that show on hover) and its body while open. */
export function Pane({ id, title, open, onToggle, actions, count, children }: { id: string; title: string; open: boolean; onToggle: () => void; actions?: ComponentChildren; count?: number; children: ComponentChildren }) {
  return (
    <section class={`rp${open ? " is-open" : ""}`} data-pane={id} aria-labelledby={`rp-${id}`}>
      <h2 class="rp-head">
        <button type="button" class="rp-toggle" id={`rp-${id}`} aria-expanded={open ? "true" : "false"} onClick={onToggle}>
          <Cod name={open ? "chevron-down" : "chevron-right"} />
          <span class="rp-title">{title}</span>
          {count !== undefined && count > 0 && <span class="rp-count">{count}</span>}
        </button>
        {actions && open && <span class="rp-actions">{actions}</span>}
      </h2>
      {open && <div class="rp-body">{children}</div>}
    </section>
  );
}

/** Note is a quiet line in a view (empty states). */
export function Note({ children }: { children: ComponentChildren }) {
  return (
    <p class="rp-note" dir="auto">
      {children}
    </p>
  );
}

/** Confirm asks in the row before an action: the question, Cancel
 * (first and focused; Enter and Esc never do the action) and the verb. */
export function Confirm({ text, verb, onYes, onNo, danger }: { text: string; verb: string; onYes: () => void; onNo: () => void; danger?: boolean }) {
  const safe = useRef<HTMLButtonElement | null>(null);
  useEffect(() => safe.current?.focus(), []);
  return (
    <span class="dbg-ask" role="group" aria-label={text} onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.key === "Escape" && onNo()}>
      <span>{text}</span>
      <button type="button" class="btn btn-neutral btn-small" ref={safe} onClick={onNo}>
        {t("cancel")}
      </button>
      <button type="button" class={`btn ${danger ? "btn-danger" : "btn-primary"} btn-small`} onClick={onYes}>
        {verb}
      </button>
    </span>
  );
}
