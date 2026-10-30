// Chips: small mono facts that can be tapped (terminal picker, Follow,
// Not saved, Pinned). A dot shows state: busy amber, done green.
import { Fragment, h, type ComponentChildren } from "preact";

export function Chip({
  children,
  dot,
  on,
  tone,
  onClick,
  label,
  tab,
}: {
  children: ComponentChildren;
  dot?: "busy" | "ok" | "warn";
  on?: boolean;
  tone?: "ai" | "amber" | "plain";
  onClick?: () => void;
  label?: string;
  /** tab: the chip is one tab of a tablist; on is its selected state. */
  tab?: boolean;
}) {
  const cls = `chip${on ? " is-on" : ""}${tone ? " tone-" + tone : ""}`;
  const inner = (
    <>
      {dot && <span class={`chip-dot dot-${dot}`} aria-hidden="true" />}
      {children}
    </>
  );
  return onClick ? (
    <button
      type="button"
      class={cls}
      role={tab ? "tab" : undefined}
      aria-selected={tab ? (on ? "true" : "false") : undefined}
      aria-pressed={!tab && on ? "true" : undefined}
      aria-label={label}
      onClick={onClick}
    >
      {inner}
    </button>
  ) : (
    <span class={cls}>{inner}</span>
  );
}
