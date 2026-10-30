// Buttons (web UX 7.3): neutral outline, primary (the strip blue),
// AI (lavender, used for Allow once), danger text. A button that needs the
// laptop says "Offline" while the link is down and does nothing: inputs
// change state, not shape (web UX 3.6).
import { h, type ComponentChildren } from "preact";
import { live } from "../session";
import { t } from "../strings";

export type ButtonKind = "neutral" | "primary" | "ai" | "danger" | "quiet";

interface Props {
  kind?: ButtonKind;
  onClick?: () => void;
  children: ComponentChildren;
  /** needsLink: the action goes to the laptop, so it locks while offline. */
  needsLink?: boolean;
  disabled?: boolean;
  wide?: boolean;
  small?: boolean;
  label?: string;
  autoFocus?: boolean;
  /** icon is drawn before the text (toolbars). */
  icon?: ComponentChildren;
  /** iconOnly hides the text from sight (it stays for screen readers and
   * as the tooltip). */
  iconOnly?: boolean;
  /** pressed makes it a toggle (aria-pressed). */
  pressed?: boolean;
  /** title is the tooltip, with the keys when there are some. */
  title?: string;
}

export function Button({ kind = "neutral", onClick, children, needsLink, disabled, wide, small, label, autoFocus, icon, iconOnly, pressed, title }: Props) {
  const offline = needsLink === true && !live.value;
  const off = disabled || offline;
  return (
    <button
      type="button"
      class={`btn btn-${kind}${wide ? " btn-wide" : ""}${small ? " btn-small" : ""}${offline ? " is-offline" : ""}${icon ? " has-icon" : ""}${iconOnly ? " is-icon" : ""}${pressed ? " is-pressed" : ""}`}
      aria-disabled={off ? "true" : undefined}
      aria-label={label}
      aria-pressed={pressed === undefined ? undefined : pressed ? "true" : "false"}
      title={title}
      autoFocus={autoFocus}
      onClick={() => {
        if (!off && onClick) onClick();
      }}
    >
      {icon && !offline && icon}
      {offline ? t("offline.input") : icon ? <span class="btn-text">{children}</span> : children}
    </button>
  );
}
