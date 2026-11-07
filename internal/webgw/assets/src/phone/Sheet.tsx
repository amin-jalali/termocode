// PhoneSheet: the web bottom sheet (ui/Sheet: drag down, the dim area or
// Esc mean Later and never answer) with a title and a column of actions.
// The safe action comes first and has focus; a risky action is inert for
// the arm time (600 ms, useArming) so a stray tap cannot answer.
import { h, type ComponentChildren } from "preact";
import { useRef } from "preact/hooks";
import { useArming } from "../ui/arming";
import { Sheet } from "../ui/Sheet";

export interface SheetAction {
  label: string;
  onClick: () => void;
  /** safe: the choice that changes nothing (Cancel, Deny); it goes first. */
  safe?: boolean;
  /** risky: arms after 600 ms and is drawn in the failed colour. */
  risky?: boolean;
  primary?: boolean;
  disabled?: boolean;
}

function RiskyAct({ a }: { a: SheetAction }) {
  const ref = useRef<HTMLButtonElement | null>(null);
  const arm = useArming(ref);
  const off = a.disabled || !arm.armed;
  return (
    <button
      ref={ref}
      type="button"
      class="ph-sheet-act is-risky"
      aria-disabled={off ? "true" : undefined}
      onPointerDown={arm.pressBegan}
      onKeyDown={arm.pressBegan}
      onClick={() => {
        if (!a.disabled && arm.pressOK()) a.onClick();
      }}
    >
      {a.label}
    </button>
  );
}

function Act({ a }: { a: SheetAction }) {
  if (a.risky) return <RiskyAct a={a} />;
  return (
    <button
      type="button"
      class={`ph-sheet-act${a.safe ? " is-safe" : ""}${a.primary ? " is-primary" : ""}`}
      aria-disabled={a.disabled ? "true" : undefined}
      autoFocus={a.safe}
      onClick={() => {
        if (!a.disabled) a.onClick();
      }}
    >
      {a.label}
    </button>
  );
}

/** safeFirst puts the safe actions first, keeping the order otherwise. */
export function safeFirst(actions: readonly SheetAction[]): SheetAction[] {
  return [...actions.filter((a) => a.safe), ...actions.filter((a) => !a.safe)];
}

export function PhoneSheet({
  title,
  onLater,
  actions,
  children,
}: {
  title: string;
  onLater: () => void;
  actions?: readonly SheetAction[];
  children?: ComponentChildren;
}) {
  return (
    <Sheet onLater={onLater} label={title}>
      <h2 class="ph-sheet-title">{title}</h2>
      {children}
      {actions && actions.length > 0 && (
        <div class="ph-sheet-acts">
          {safeFirst(actions).map((a) => (
            <Act key={a.label} a={a} />
          ))}
        </div>
      )}
    </Sheet>
  );
}
