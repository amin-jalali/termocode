// A request that needs a confirm (web UX 3.9), for any op whose row says
// LaptopCard. The page approves (meta.approve_here, owner 2026-11-08):
// the request goes at once and HereAsks shows the hub's exact card (the
// ask ev: what runs, the paths, the argv, the SQL), Cancel first and
// focused, the action armed after 600 ms; this page confirms it. With
// web_approve_on_laptop on, first an in-page sheet with the question
// ("Ask the laptop" armed), then the waiting state, whose Cancel cancels
// the laptop's card too. The outcome is told even after the sheet closed.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { asked, cancelAsk, ended, later, onAnswered } from "../lib/asks";
import { toast } from "../lib/toasts";
import { link, live } from "../session";
import { t } from "../strings";
import { gitFailed as failed } from "../views/git/errors";
import { useArming } from "./arming";
import { Sheet } from "./Sheet";
import { WaitingLaptop } from "./Waiting";
import { DBAsk } from "./DBAsk";
import type { DBNeed } from "../views/desk/dbstate";

type Step = { s: "ask" } | { s: "sending" } | { s: "here" } | { s: "wait"; card: string; since: number };

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
    send().then(
      (r) => {
        const card = String((r as { card?: unknown } | null)?.card ?? "");
        onAnswered(card, (o) => {
          if (o.confirmed) {
            toast("ok", t(o.by === "laptop" ? "wait.done" : "done"));
            onConfirmed?.();
          } else toast("info", t(o.by === "laptop" ? "wait.cancelled" : "git.cancelled_here"));
          onDone();
        });
        // The page approves: HereAsks shows the card (the ask ev came
        // first), or the TUI asks nothing and it runs.
        setStep(here || asked.peek().some((a) => a.id === card) ? { s: "here" } : { s: "wait", card, since: Date.now() });
      },
      (e: unknown) => {
        failed(e);
        onDone();
      },
    );
  };
  useEffect(() => {
    if (here) ask();
  }, []);

  if ((here && step.s === "sending") || step.s === "here") return null;
  const cancel = () => step.s === "wait" && cancelAsk(step.card).catch(failed);
  return (
    <Sheet onLater={onDone} label={label}>
      {step.s === "wait" ? (
        <WaitingLaptop since={step.since} onCancel={cancel} />
      ) : (
        <Ask cls={cls} text={text} note={t("wait.e14")} verb={t("git.ask_laptop")} off={step.s !== "ask"} onNo={onDone} onYes={ask} />
      )}
    </Sheet>
  );
}

/** HereAsks shows this page's own questions (ask evs), one at a time:
 * the card's exact text, Cancel first; the answer is this page's. A
 * question an AI db_* tool waits on (conn and need) is the Databases
 * view's own card (DBAsk): Trust and save, or a masked field; closing
 * its sheet puts it off (the tool waits, the chat's line opens it). */
export function HereAsks() {
  const a = asked.value.find((x) => x.id !== later.value);
  if (!a) return null;
  const end = (e: unknown) => {
    failed(e);
    ended(a.id);
  };
  const no = () => cancelAsk(a.id).catch(end);
  return (
    <Sheet onLater={a.need ? () => (later.value = a.id) : no} label={a.title}>
      {a.need && a.conn ? (
        <div class="ask">
          <p class="ask-note">{a.title}</p>
          <DBAsk key={a.id} conn={a.conn} need={a.need as DBNeed} msg={a.body} onDone={() => ended(a.id, { confirmed: true, by: "" })} onNo={no} />
        </div>
      ) : (
        <Ask key={a.id} cls="ask" text={a.body} verb={a.action} onNo={no} onYes={() => link.request("cards.answer", { id: a.id, choice: "confirm" }).catch(end)} />
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
