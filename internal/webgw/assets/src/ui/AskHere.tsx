// A question asked in the page before an action that loses work (the
// command list's Revert File): the question, Cancel first and focused,
// the action armed after a moment. Enter and Esc never do the action.
import { h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef } from "preact/hooks";
import { live } from "../session";
import { t } from "../strings";
import { useArming } from "./arming";
import { Sheet } from "./Sheet";

export interface HereAsk {
  label: string;
  text: string;
  verb: string;
  run(): void;
}

/** askHere is the question on screen (null: none). */
export const askHere = signal<HereAsk | null>(null);

export function AskHere({ ask }: { ask: HereAsk }) {
  const box = useRef<HTMLDivElement | null>(null);
  const safe = useRef<HTMLButtonElement | null>(null);
  const arm = useArming(box);
  useEffect(() => safe.current?.focus(), []);
  const close = () => (askHere.value = null);
  const yes = () => {
    if (!arm.pressOK() || !live.value) return;
    close();
    ask.run();
  };
  return (
    <Sheet onLater={close} label={ask.label}>
      <div class="ask ask-here" ref={box}>
        <p class="ask-text">{ask.text}</p>
        <div class="choices">
          <button type="button" class="btn btn-neutral" ref={safe} onClick={close}>
            {t("cancel")}
          </button>
          <button
            type="button"
            class={`btn btn-danger arm${arm.armed ? " is-armed" : ""}`}
            aria-disabled={arm.armed && live.value ? undefined : "true"}
            onPointerDown={arm.pressBegan}
            onKeyDown={(e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && arm.pressBegan()}
            onClick={yes}
          >
            <span class="arm-fill" aria-hidden="true" />
            <span class="arm-label">{ask.verb}</span>
          </button>
        </div>
      </div>
    </Sheet>
  );
}
