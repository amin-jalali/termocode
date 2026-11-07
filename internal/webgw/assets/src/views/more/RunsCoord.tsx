// Coordinator runs from the phone (MOB-64, mobile plan §3.9). "Plan and
// run" takes a task and a planner profile, then asks the laptop
// (ai.run_start is a laptop card, Cancel first): nothing starts before
// the laptop's yes, and the planner's plan comes back as a "plan" card
// (Reject first). "Message" sends a working step of a run a note
// (runs.send); the step reads it after its current tool call.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { toast } from "../../lib/toasts";
import { link } from "../../session";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Button } from "../../ui/Button";
import { Sheet } from "../../ui/Sheet";
import { gitFailed as failed } from "../git/errors";

const TASK_MAX = 8000; // webproto.MaxRunTask
const SEND_MAX = 4000; // webproto.MaxRunSend

/** The planner profiles the phone offers, safest first. */
const PROFILES = [
  { name: "plan", label: "coord.plan" },
  { name: "until-done", label: "coord.until_done" },
] as const;

/** CoordStart is the "Plan and run" sheet, then the laptop's question. */
export function CoordStart({ onDone }: { onDone: () => void }) {
  const [task, setTask] = useState("");
  const [profile, setProfile] = useState<string>(PROFILES[0].name);
  const [asking, setAsking] = useState(false);
  const area = useRef<HTMLTextAreaElement | null>(null);
  useEffect(() => area.current?.focus(), []);
  if (asking) {
    const label = PROFILES.find((p) => p.name === profile)?.label ?? "coord.plan";
    return (
      <AskLaptop
        label={t("coord.title")}
        text={t("coord.ask", { profile: t(label) })}
        send={() => link.request("ai.run_start", { task: task.trim(), profile })}
        onDone={onDone}
      />
    );
  }
  const empty = task.trim() === "";
  return (
    <Sheet onLater={onDone} label={t("coord.title")}>
      <div class="coord">
        <p class="row-ask-text">{t("coord.title")}</p>
        <textarea
          ref={area}
          class="run-text"
          rows={4}
          value={task}
          maxLength={TASK_MAX}
          aria-label={t("coord.task")}
          placeholder={t("coord.task")}
          onInput={(e) => setTask((e.currentTarget as HTMLTextAreaElement).value)}
        />
        <div class="choices" role="radiogroup" aria-label={t("coord.profile")}>
          {PROFILES.map((p) => (
            <button
              key={p.name}
              type="button"
              role="radio"
              aria-checked={profile === p.name ? "true" : "false"}
              class={`btn btn-small btn-neutral${profile === p.name ? " is-pressed" : ""}`}
              onClick={() => setProfile(p.name)}
            >
              {profile === p.name ? "● " : "○ "}
              {t(p.label)}
            </button>
          ))}
        </div>
        <p class="ask-note">{t("coord.note")}</p>
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={onDone}>
            {t("cancel")}
          </button>
          <Button kind="primary" needsLink disabled={empty} onClick={() => setAsking(true)}>
            {t("coord.next")}
          </Button>
        </div>
      </div>
    </Sheet>
  );
}

/** RunSend is a run card's inline message box (Cancel first). */
export function RunSend({ runKey, steps, onDone }: { runKey: string; steps: { id: string; title: string }[]; onDone: () => void }) {
  const [text, setText] = useState("");
  const [step, setStep] = useState(steps[0]?.id ?? "");
  const [sending, setSending] = useState(false);
  const area = useRef<HTMLTextAreaElement | null>(null);
  useEffect(() => area.current?.focus(), []);
  const send = () => {
    if (sending || text.trim() === "") return;
    setSending(true);
    link.request("runs.send", { key: runKey, step, text: text.trim() }).then(
      () => {
        toast("ok", t("runs.sent"));
        onDone();
      },
      (e: unknown) => {
        setSending(false);
        failed(e);
      },
    );
  };
  return (
    <div class="row-ask" role="group" aria-label={t("runs.message")}>
      {steps.length > 1 && (
        <div class="choices" role="radiogroup" aria-label={t("runs.to_step")}>
          {steps.map((s) => (
            <button
              key={s.id}
              type="button"
              role="radio"
              aria-checked={step === s.id ? "true" : "false"}
              class={`btn btn-small btn-neutral${step === s.id ? " is-pressed" : ""}`}
              onClick={() => setStep(s.id)}
            >
              {step === s.id ? "● " : "○ "}
              {s.title || s.id}
            </button>
          ))}
        </div>
      )}
      <textarea
        ref={area}
        class="run-text"
        rows={3}
        value={text}
        maxLength={SEND_MAX}
        aria-label={t("runs.message")}
        placeholder={t("runs.message_hint")}
        onInput={(e) => setText((e.currentTarget as HTMLTextAreaElement).value)}
      />
      <div class="choices">
        <button type="button" class="btn btn-neutral" onClick={onDone}>
          {t("cancel")}
        </button>
        <Button kind="primary" needsLink disabled={sending || text.trim() === ""} onClick={send}>
          {t("send")}
        </Button>
      </div>
    </div>
  );
}
