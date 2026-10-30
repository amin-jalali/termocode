// Discard asks the laptop (web UX 3.9). First an in-page sheet: "Discard
// changes in <path>? This cannot be undone." with Cancel first and
// focused; "Ask the laptop" arms after 600 ms. Then the waiting state:
// the laptop shows its own card, the phone counts up and offers Cancel,
// which cancels the laptop's card too. Nothing is discarded until the
// laptop says yes.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { cancelAsk, onAnswered } from "../../lib/asks";
import { toast } from "../../lib/toasts";
import { link, live } from "../../session";
import { useArming } from "../../ui/arming";
import { Sheet } from "../../ui/Sheet";
import { WaitingLaptop } from "../../ui/Waiting";
import { t } from "../../strings";
import { gitFailed } from "./errors";

type Step = { s: "ask" } | { s: "sending" } | { s: "wait"; card: string; since: number };

export function DiscardFlow({ paths, onDone }: { paths: string[]; onDone: () => void }) {
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
    link.request("git.discard", { paths }).then(
      (r) => {
        const card = String((r as { card?: unknown } | null)?.card ?? "");
        // Outlives the sheet: the outcome is told even after it closed.
        onAnswered(card, (o) => {
          if (o.confirmed) toast("ok", t("wait.done"));
          else if (o.by === "laptop") toast("info", t("wait.cancelled"));
          else toast("info", t("git.cancelled_here"));
          onDone();
        });
        setStep({ s: "wait", card, since: Date.now() });
      },
      (e: unknown) => {
        gitFailed(e);
        onDone();
      },
    );
  };

  const cancel = () => {
    if (step.s === "wait") cancelAsk(step.card).catch(gitFailed);
  };

  return (
    <Sheet onLater={onDone} label={t("git.discard")}>
      {step.s === "wait" ? (
        <WaitingLaptop since={step.since} onCancel={cancel} />
      ) : (
        <div class="discard" ref={box}>
          <p class="discard-text">{t("git.discard_body", { path: paths.join(", ") })}</p>
          <p class="discard-note">{t("wait.e14")}</p>
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
