// Approval cards (web UX 3.2, 5.3). The safe answer comes first and has
// focus; Enter presses the focused button only; Esc means Later, never an
// answer; the risky button is inert for 600 ms (useArming) with a visible
// fill. There is no "Always allow" from the web, ever.
import { h, type ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { t, type StringKey } from "../strings";
import { useArming } from "./arming";

export type CardKind = "tool" | "review" | "plan" | "mcp" | "trust";

/** open: waiting for an answer. answered: the laptop answered first.
 * offline: no answer without a live link. gone: the card no longer
 * exists. viewonly: this browser may not answer. long: a diff too long to
 * read here, so it cannot be accepted here. */
export type CardState = "open" | "answered" | "offline" | "gone" | "viewonly" | "long";

const titles: Record<CardKind, StringKey> = {
  tool: "card.tool.title",
  review: "card.review.title",
  plan: "card.plan.title",
  mcp: "card.mcp.title",
  trust: "card.trust.title",
};

const choices: Record<Exclude<CardKind, "trust">, { safe: StringKey; risky: StringKey }> = {
  tool: { safe: "card.deny", risky: "card.allow_once" },
  mcp: { safe: "card.deny", risky: "card.allow_once" },
  review: { safe: "card.reject", risky: "card.accept" },
  plan: { safe: "card.deny", risky: "card.approve" },
};

export interface CardProps {
  id: string;
  kind: CardKind;
  state?: CardState;
  /** subject is the command or path, in mono, LTR. */
  subject?: string;
  where?: string;
  children?: ComponentChildren;
  /** dialog: a real card (focus goes to the safe button, Esc = Later). inline: shown in a list. */
  mode?: "dialog" | "inline";
  onAnswer?: (choice: "safe" | "risky") => void;
  onLater?: () => void;
  onClose?: () => void;
  onLaptop?: () => void;
  /** by names who answered an answered card: "laptop" (the default) or another browser. */
  by?: string;
}

const CHECK_MS = 300;

export function Card({ id, kind, state = "open", subject, where, children, mode = "dialog", onAnswer, onLater, onClose, onLaptop, by }: CardProps) {
  const root = useRef<HTMLElement | null>(null);
  const safeBtn = useRef<HTMLButtonElement | null>(null);
  const arm = useArming(root);
  const [done, setDone] = useState(false);

  useEffect(() => {
    if (mode === "dialog") safeBtn.current?.focus();
  }, [mode, state]);

  const titleID = `card-${id}-t`;
  const armID = `card-${id}-arm`;

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      onLater?.();
    }
  };

  const risky = () => {
    if (done || !arm.pressOK()) return;
    setDone(true);
    setTimeout(() => onAnswer?.("risky"), CHECK_MS);
  };

  let foot: ComponentChildren;
  if (state === "answered" || state === "gone") {
    foot = (
      <div class="card-line">
        <span>{state === "gone" ? t("card.gone") : by && by !== "laptop" ? t("answered.other", { name: by }) : t("answered.laptop")}</span>
        <button type="button" class="btn btn-neutral" ref={safeBtn} onClick={onClose}>
          {t("close")}
        </button>
      </div>
    );
  } else if (state === "offline") {
    foot = <div class="card-line card-wait">{t("offline.card")}</div>;
  } else if (state === "viewonly") {
    foot = <div class="card-line">{t("card.viewonly")}</div>;
  } else if (kind === "trust") {
    foot = (
      <div class="choices">
        <button type="button" class="btn btn-neutral" ref={safeBtn} onClick={onLater}>
          {t("card.later")}
        </button>
        <button type="button" class="btn btn-neutral" onClick={onLaptop}>
          {t("card.answer_laptop")}
        </button>
      </div>
    );
  } else {
    const c = choices[kind];
    foot = (
      <div class="choices">
        <button type="button" class="btn btn-neutral" ref={safeBtn} onClick={() => onAnswer?.("safe")}>
          {t(c.safe)}
        </button>
        {state === "long" ? (
          <button type="button" class="btn btn-neutral" onClick={onLaptop}>
            {t("laptop.open")}
          </button>
        ) : (
          <button
            type="button"
            class={`btn btn-ai arm${arm.armed ? " is-armed" : ""}${done ? " is-done" : ""}`}
            aria-disabled={arm.armed ? undefined : "true"}
            aria-describedby={arm.armed ? undefined : armID}
            onPointerDown={arm.pressBegan}
            onKeyDown={(e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && arm.pressBegan()}
            onClick={risky}
          >
            <span class="arm-fill" aria-hidden="true" />
            <span class="arm-label">{done ? "✓" : t(c.risky)}</span>
            {!arm.armed && (
              <span class="arm-ticks" aria-hidden="true">
                {["1", "2", "✓"].slice(0, Math.max(arm.tick, 1)).join(" · ")}
              </span>
            )}
          </button>
        )}
        <span id={armID} class="sr-only">
          {t("card.arming")}
        </span>
      </div>
    );
  }

  return (
    <section
      ref={root}
      class={`card card-${kind} card-${mode} state-${state}`}
      role={mode === "dialog" ? "dialog" : "group"}
      aria-modal={mode === "dialog" ? "true" : undefined}
      aria-labelledby={titleID}
      onKeyDown={onKeyDown}
    >
      <header class="card-head">
        <span class="star" aria-hidden="true">✦</span>
        <h2 class="card-title" id={titleID}>
          {t(titles[kind])}
        </h2>
      </header>
      <div class="hairline" aria-hidden="true" />
      {subject && (
        <pre class="card-subject" dir="ltr">
          {subject}
        </pre>
      )}
      {state === "long" ? <p class="card-body">{t("card.long")}</p> : children && <div class="card-body" dir="auto">{children}</div>}
      {where && (
        <div class="card-where" dir="ltr">
          {where}
        </div>
      )}
      <footer class="card-foot">{foot}</footer>
      {(state === "open" && kind !== "trust") || state === "long" ? (
        <div class="card-hint" aria-hidden="true">
          <span class="hint-keys">{t("card.hint")}</span>
          <span class="hint-touch">{t("card.hint_touch")}</span>
        </div>
      ) : null}
    </section>
  );
}
