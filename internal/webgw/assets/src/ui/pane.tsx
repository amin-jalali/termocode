// The desktop side bar's building blocks, with the laptop editor's
// metrics (styles/panes.css): a view with its 35 px title and actions,
// folding sections (22 px uppercase headers with a chevron, a count and
// actions on hover), tree rows (22 px, 8 px a level, a twistie, an icon,
// a dim description, actions on hover) and the arrow keys of a tree:
// one tab stop, Up/Down move, Right/Left open and close, Enter acts.
import { h, type ComponentChildren } from "preact";
import { useLayoutEffect, useRef } from "preact/hooks";
import { dict } from "../state/dict";
import { t } from "../strings";
import { Cod } from "./icons";

/** PaneView is a side bar view: its title (uppercase) and actions. */
export function PaneView({ id, title, actions, head = true, children }: { id: string; title: string; actions?: ComponentChildren; head?: boolean; children: ComponentChildren }) {
  // head false: the view sits under another one's title (Run and Debug's
  // tasks and runs under its debugger), named for screen readers only.
  if (!head)
    return (
      <section class="pv view pv-headless" data-view={id} aria-label={title}>
        <div class="pv-body">{children}</div>
      </section>
    );
  return (
    <section class="pv view" data-view={id} aria-labelledby={`pv-${id}`}>
      <header class="pv-title">
        <h1 id={`pv-${id}`}>{title}</h1>
        {actions && <div class="pv-actions on-hover">{actions}</div>}
      </header>
      <div class="pv-body">{children}</div>
    </section>
  );
}

/** Act is an icon button (a codicon), its name as label and tooltip. */
export function Act({ icon, label, keys, on, disabled, onClick, act }: { icon: string; label: string; keys?: string; on?: boolean; disabled?: boolean; onClick: (e: MouseEvent) => void; act?: string }) {
  return (
    <button
      type="button"
      class={`act${on ? " is-on" : ""}`}
      data-act={act}
      aria-label={label}
      aria-pressed={on === undefined ? undefined : on ? "true" : "false"}
      aria-disabled={disabled ? "true" : undefined}
      title={keys ? `${label} (${keys})` : label}
      onClick={(e) => {
        e.stopPropagation();
        if (!disabled) onClick(e);
      }}
    >
      <Cod name={icon} />
    </button>
  );
}

/** PaneSection is a folding part of a view. */
export function PaneSection({
  id,
  title,
  open,
  onToggle,
  count,
  actions,
  fixed,
  children,
}: {
  id: string;
  title: string;
  open: boolean;
  onToggle: () => void;
  count?: number;
  actions?: ComponentChildren;
  /** fixed: the section takes the room its rows need, not the rest. */
  fixed?: boolean;
  children: ComponentChildren;
}) {
  return (
    <section class={`ps${open ? " is-open" : ""}${fixed ? " is-fixed" : ""}`} data-section={id} aria-labelledby={`ps-${id}`}>
      <div
        class="ps-head"
        role="button"
        tabIndex={0}
        id={`ps-${id}`}
        aria-expanded={open ? "true" : "false"}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.target !== e.currentTarget) return;
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onToggle();
          }
        }}
      >
        <Cod name={open ? "chevron-down" : "chevron-right"} />
        <span class="ps-title">{title}</span>
        {actions && <span class="ps-actions">{actions}</span>}
        {count !== undefined && <span class="count">{count}</span>}
      </div>
      {open && <div class="ps-body">{children}</div>}
    </section>
  );
}

export interface RowProps {
  level?: number;
  /** twist: the row opens and closes (null: a leaf, room kept). */
  twist?: "open" | "closed" | null;
  icon?: ComponentChildren;
  label: ComponentChildren;
  labelClass?: string;
  desc?: ComponentChildren;
  /** end shows at the row's end (a letter, a state); actions replace it on hover. */
  end?: ComponentChildren;
  actions?: ComponentChildren;
  selected?: boolean;
  title?: string;
  data?: Record<string, string>;
  onClick?: () => void;
  onDblClick?: () => void;
  onToggle?: () => void;
  onContextMenu?: (e: MouseEvent) => void;
}

/** Row is one tree row. */
export function Row(p: RowProps) {
  const extra = dict<string>();
  for (const [k, v] of Object.entries(p.data ?? {})) extra["data-" + k] = v;
  return (
    <div
      class={`tr${p.selected ? " is-sel" : ""}`}
      role="treeitem"
      tabIndex={-1}
      aria-level={(p.level ?? 0) + 1}
      aria-expanded={p.twist === "open" ? "true" : p.twist === "closed" ? "false" : undefined}
      aria-selected={p.selected ? "true" : "false"}
      title={p.title}
      style={{ "--lv": String(p.level ?? 0) }}
      onClick={() => (p.twist && p.onToggle ? p.onToggle() : p.onClick?.())}
      onDblClick={p.onDblClick}
      onContextMenu={p.onContextMenu}
      data-twist={p.twist ?? undefined}
      {...extra}
    >
      {p.twist !== undefined && <span class="tr-twist">{p.twist && <Cod name={p.twist === "open" ? "chevron-down" : "chevron-right"} />}</span>}
      {p.icon && <span class="tr-icon">{p.icon}</span>}
      <span class={`tr-label${p.labelClass ? " " + p.labelClass : ""}`}>{p.label}</span>
      {p.desc !== undefined && p.desc !== "" && <span class="tr-desc">{p.desc}</span>}
      {(p.end || p.actions) && (
        <span class="tr-end">
          {p.actions && <span class="tr-actions">{p.actions}</span>}
          {p.end && <span class={p.actions ? "tr-hide-hover" : undefined}>{p.end}</span>}
        </span>
      )}
    </div>
  );
}

/** Tree holds rows with one tab stop and the arrow keys of a tree. */
export function Tree({ label, children, class: cls }: { label: string; children: ComponentChildren; class?: string }) {
  const box = useRef<HTMLDivElement | null>(null);
  const rows = () => [...(box.current?.querySelectorAll<HTMLElement>(".tr") ?? [])];
  // One row takes the Tab key: the selected one, or the first.
  useLayoutEffect(() => {
    const all = rows();
    if (all.some((r) => r.tabIndex === 0 && r.isConnected)) return;
    const pick = all.find((r) => r.classList.contains("is-sel")) ?? all[0];
    for (const r of all) r.tabIndex = r === pick ? 0 : -1;
  });
  const move = (from: HTMLElement, to: HTMLElement | undefined) => {
    if (!to) return;
    from.tabIndex = -1;
    to.tabIndex = 0;
    to.focus();
  };
  return (
    <div
      ref={box}
      class={`pv-tree${cls ? " " + cls : ""}`}
      role="tree"
      aria-label={label}
      onFocusIn={(e) => {
        const r = (e.target as HTMLElement).closest<HTMLElement>(".tr");
        if (!r) return;
        for (const x of rows()) x.tabIndex = x === r ? 0 : -1;
      }}
      onKeyDown={(e) => {
        const cur = (e.target as HTMLElement).closest<HTMLElement>(".tr");
        if (!cur || (e.target as HTMLElement) !== cur) return;
        const all = rows();
        const i = all.indexOf(cur);
        const tw = cur.dataset.twist;
        switch (e.key) {
          case "ArrowDown":
            e.preventDefault();
            return move(cur, all[i + 1]);
          case "ArrowUp":
            e.preventDefault();
            return move(cur, all[i - 1]);
          case "Home":
            e.preventDefault();
            return move(cur, all[0]);
          case "End":
            e.preventDefault();
            return move(cur, all[all.length - 1]);
          case "ArrowRight":
            e.preventDefault();
            if (tw === "closed") cur.click();
            else if (tw === "open") move(cur, all[i + 1]);
            return;
          case "ArrowLeft": {
            e.preventDefault();
            if (tw === "open") return cur.click();
            const lv = Number(cur.getAttribute("aria-level") ?? "1");
            for (let j = i - 1; j >= 0; j--) if (Number(all[j].getAttribute("aria-level") ?? "1") < lv) return move(cur, all[j]);
            return;
          }
          case "Enter":
          case " ":
            e.preventDefault();
            cur.click();
            return;
        }
      }}
    >
      {children}
    </div>
  );
}

/** Ask is an inline question with the safe answer first and focused;
 * Enter and Esc never do the risky thing. */
export function Ask({ text, verb, danger, onYes, onNo }: { text: string; verb: string; danger?: boolean; onYes: () => void; onNo: () => void }) {
  const safe = useRef<HTMLButtonElement | null>(null);
  useLayoutEffect(() => safe.current?.focus(), []);
  return (
    <div class="pv-ask" role="group" aria-label={text} onKeyDown={(e) => e.key === "Escape" && (e.stopPropagation(), onNo())}>
      <p>{text}</p>
      <div class="pv-ask-row">
        <button type="button" class="pv-btn is-2" ref={safe} onClick={onNo}>
          {t("cancel")}
        </button>
        <button type="button" class={`pv-btn${danger ? " is-danger" : ""}`} onClick={onYes} onKeyDown={(e) => e.key === "Enter" && e.preventDefault()}>
          {verb}
        </button>
      </div>
    </div>
  );
}
