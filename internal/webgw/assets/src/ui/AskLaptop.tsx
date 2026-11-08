// A request that needs a confirm (web UX 3.9), for any op whose row says
// LaptopCard. The page approves (meta.approve_here, owner 2026-11-08):
// the request goes at once, and the laptop's answer is its exact card
// (the ask ev: what runs, the paths, the argv, the SQL), Cancel first and
// focused, the action armed after 600 ms; this page confirms it. With
// web_approve_on_laptop on, first an in-page sheet with the question
// ("Ask the laptop" armed), then the waiting state, whose Cancel cancels
// the laptop's card too. The outcome is told even after the sheet closed.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { AskCard } from "../../../../webproto/proto";
import { cancelAsk, onAnswered } from "../lib/asks";
import { toast } from "../lib/toasts";
import { link, live } from "../session";
import { t } from "../strings";
import { gitFailed as failed } from "../views/git/errors";
import { useArming } from "./arming";
import { Sheet } from "./Sheet";
import { WaitingLaptop } from "./Waiting";

type Step = { s: "ask" } | { s: "sending" } | { s: "wait"; card: string; since: number } | { s: "here"; card: string; text: string; verb: string };

interface Props {
  label: string;
  /** text is the question: what will happen, and that it cannot be undone. */
  text: string;
  /** send makes the request; it answers with the card's id. */
  send(): Promise<unknown>;
  onDone(): void;
  onConfirmed?(): void;
  /** cls names the question's box (its -text and -note too). */
  cls?: string;
}

export function AskLaptop({ label, text, send, onDone, onConfirmed, cls = "ask" }: Props) {
  const here = link.meta.value?.approve_here === true;
  const [step, setStep] = useState<Step>({ s: here ? "sending" : "ask" });

  const ask = () => {
    setStep({ s: "sending" });
    let got: AskCard | undefined;
    const off = link.onEvent((k, d) => k === "ask" && (got = d as AskCard));
    send().then(
      (r) => {
        off();
        const card = String((r as { card?: unknown } | null)?.card ?? "");
        onAnswered(card, (o) => {
          if (o.confirmed) {
            toast("ok", t(o.by === "laptop" ? "wait.done" : "done"));
            onConfirmed?.();
          } else toast("info", t(o.by === "laptop" ? "wait.cancelled" : "git.cancelled_here"));
          onDone();
        });
        setStep(got?.id === card ? { s: "here", card, text: got.body, verb: got.action } : { s: "wait", card, since: Date.now() });
      },
      (e: unknown) => {
        off();
        failed(e);
        onDone();
      },
    );
  };
  useEffect(() => {
    if (here) ask();
  }, []);

  const cancel = () => {
    if (step.s === "wait" || step.s === "here")
      cancelAsk(step.card).catch((e: unknown) => {
        failed(e);
        onDone();
      });
  };

  return (
    <Sheet onLater={step.s === "here" ? cancel : onDone} label={label}>
      {step.s === "wait" ? (
        <WaitingLaptop since={step.since} onCancel={cancel} />
      ) : step.s === "here" ? (
        <Ask key="here" cls={cls} text={step.text} verb={step.verb} onNo={cancel} onYes={() => link.request("cards.answer", { id: step.card, choice: "confirm" }).catch(failed)} />
      ) : here ? null : (
        <Ask cls={cls} text={text} note={t("wait.e14")} verb={t("git.ask_laptop")} off={step.s !== "ask"} onNo={onDone} onYes={ask} />
      )}
    </Sheet>
  );
}

interface AskProps {
  cls: string;
  text: string;
  note?: string;
  verb: string;
  off?: boolean;
  onYes(): void;
  onNo(): void;
}

/** Ask is the question: Cancel first and focused, the action armed after a moment. */
function Ask({ cls, text, note, verb, off, onYes, onNo }: AskProps) {
  const box = useRef<HTMLDivElement | null>(null);
  const safe = useRef<HTMLButtonElement | null>(null);
  const arm = useArming(box);
  useEffect(() => safe.current?.focus(), []);
  return (
    <div class={cls} ref={box}>
      <p class={cls + "-text"}>{text}</p>
      {note && <p class={cls + "-note"}>{note}</p>}
      <div class="choices">
        <button type="button" class="btn btn-neutral" ref={safe} onClick={onNo}>
          {t("cancel")}
        </button>
        <button
          type="button"
          class={`btn btn-danger arm${arm.armed ? " is-armed" : ""}`}
          aria-disabled={arm.armed && live.value && !off ? undefined : "true"}
          onPointerDown={arm.pressBegan}
          onKeyDown={(e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && arm.pressBegan()}
          onClick={() => arm.pressOK() && live.value && !off && onYes()}
        >
          <span class="arm-fill" aria-hidden="true" />
          <span class="arm-label">{live.value ? verb : t("offline.input")}</span>
        </button>
      </div>
    </div>
  );
}
