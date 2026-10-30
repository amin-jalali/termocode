// A request that waits for the laptop (web UX 3.9), for any op whose row
// says LaptopCard: first an in-page sheet with the question, Cancel first
// and focused and "Ask the laptop" armed after 600 ms; then the waiting
// state, whose Cancel cancels the laptop's card too. Nothing happens
// until the laptop says yes; the outcome is told even after the sheet
// closed.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { cancelAsk, onAnswered } from "../lib/asks";
import { toast } from "../lib/toasts";
import { live } from "../session";
import { t } from "../strings";
import { gitFailed as failed } from "../views/git/errors";
import { useArming } from "./arming";
import { Sheet } from "./Sheet";
import { WaitingLaptop } from "./Waiting";

type Step = { s: "ask" } | { s: "sending" } | { s: "wait"; card: string; since: number };

interface Props {
  label: string;
  /** text is the question: what will happen, and that it cannot be undone. */
  text: string;
  /** send makes the request; it answers with the laptop card's id. */
  send(): Promise<unknown>;
  onDone(): void;
  onConfirmed?(): void;
}

export function AskLaptop({ label, text, send, onDone, onConfirmed }: Props) {
  const [step, setStep] = useState<Step>({ s: "ask" });
  const box = useRef<HTMLDivElement | null>(null);
  const safe = useRef<HTMLButtonElement | null>(null);
  const arm = useArming(box);

  useEffect(() => {
    safe.current?.focus();
  }, []);

  const ask = () => {
    if (!arm.pressOK() || step.s !== "ask" || !live.value) return;
    setStep({ s: "sending" });
    send().then(
      (r) => {
        const card = String((r as { card?: unknown } | null)?.card ?? "");
        onAnswered(card, (o) => {
          if (o.confirmed) {
            toast("ok", t("wait.done"));
            onConfirmed?.();
          } else if (o.by === "laptop") toast("info", t("wait.cancelled"));
          else toast("info", t("git.cancelled_here"));
          onDone();
        });
        setStep({ s: "wait", card, since: Date.now() });
      },
      (e: unknown) => {
        failed(e);
        onDone();
      },
    );
  };

  const cancel = () => {
    if (step.s === "wait") cancelAsk(step.card).catch(failed);
  };

  return (
    <Sheet onLater={onDone} label={label}>
      {step.s === "wait" ? (
        <WaitingLaptop since={step.since} onCancel={cancel} />
      ) : (
        <div class="ask" ref={box}>
          <p class="ask-text">{text}</p>
          <p class="ask-note">{t("wait.e14")}</p>
          <div class="choices">
            <button type="button" class="btn btn-neutral" ref={safe} onClick={onDone}>
              {t("cancel")}
            </button>
            <button
              type="button"
              class={`btn btn-danger arm${arm.armed ? " is-armed" : ""}`}
              aria-disabled={arm.armed && live.value && step.s === "ask" ? undefined : "true"}
              onPointerDown={arm.pressBegan}
              onKeyDown={(e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && arm.pressBegan()}
              onClick={ask}
            >
              <span class="arm-fill" aria-hidden="true" />
              <span class="arm-label">{live.value ? t("git.ask_laptop") : t("offline.input")}</span>
            </button>
          </div>
        </div>
      )}
    </Sheet>
  );
}
