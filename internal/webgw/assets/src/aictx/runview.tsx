// A run of several steps in the desktop chat (look.tsx): a small strip
// of its steps above the answer ("◉ Plan ─ ◉ Code ─ ○ Review  step 2 of
// 3 · 40s  Show"), and Show opens the Run view beside the chat: each
// step with its model, time, state, output, tool calls and error. The
// view follows the run live (it reads the chat it came from). On a phone
// the strip is one button ("◉ Plan ─ ◉ Code ─ ○ Review · 2/3") and the
// Run view a full-screen sheet.
import type { Signal } from "@preact/signals";
import type { ChatMsg, ChatStep } from "../../../../webproto/proto";
import { readChat } from "../state/chat";
import { H, h } from "./jsx";
import { Clock, Tool, dur } from "./look";

/** tone is a step state's dot: done, now, bad or wait. */
function tone(state = ""): string {
  if (state === "completed" || state === "done") return "done";
  if (state === "running") return "now";
  if (state === "failed" || state === "cancelled" || state === "interrupted") return "bad";
  return "wait";
}

/** time is a run's time after a " · ": counting while it runs, else
 * how long it took (null: not known). */
function time(m: ChatMsg) {
  const r = m.run!;
  if (r.state === "running" && r.started_ms) return [" · ", <Clock from={r.started_ms} />];
  return r.elapsed_ms ? " · " + dur(r.elapsed_ms) : null;
}

type At = { chat: string; i: number };

/** shown is the message whose Run view is open (made on the first Show). */
let shown: Signal<At | null> | null = null;

export function Strip({ m, chat }: { m: ChatMsg; chat: string }) {
  const r = m.run!;
  const steps = m.steps ?? [];
  const cur = steps.findIndex((s) => s.state === "running");
  const dots = (
    <span class="strip-steps">
      {steps.map((s, i) => [i > 0 && <span class="strip-sep" aria-hidden="true" />, <span class={`strip-step is-${tone(s.state)}`}>{s.title || s.id}</span>])}
    </span>
  );
  const wait = r.plan_wait && <span class="strip-wait">{H.t("run.plan_wait")}</span>;
  const show = () => openRun(chat, m.i);
  if (H.phone) {
    // The phone's strip is one button: "◉ Plan ─ ◉ Code ─ ○ Review · 2/3".
    const at = cur >= 0 ? cur + 1 : steps.filter((s) => tone(s.state) === "done").length;
    return (
      <button type="button" class={`run-strip st-${r.state}`} dir="ltr" aria-haspopup="dialog" onClick={show}>
        {dots}
        <span class="strip-where">
          {at}/{steps.length}
          {time(m)}
        </span>
        {wait}
      </button>
    );
  }
  const where =
    r.state === "running"
      ? cur >= 0
        ? H.t("look.step_of", { n: cur + 1, of: steps.length })
        : r.state
      : r.state === "done"
        ? H.t("look.steps", { n: steps.length })
        : r.state;
  return (
    <div class={`run-strip st-${r.state}`} dir="ltr">
      {dots}
      <span class="strip-where">
        {where}
        {time(m)}
      </span>
      {wait}
      <button type="button" class="strip-show" onClick={show}>
        {H.t("srv.show")}
      </button>
    </div>
  );
}

function openRun(chat: string, i: number): void {
  if (!shown) {
    shown = H.signal<At | null>(null);
    // Its own root, with the workbench's colours (.layout-desktop); on a
    // phone a full-screen sheet (.ph-runv).
    const el = document.body.appendChild(document.createElement("div"));
    el.className = H.phone ? "ph-runv" : "layout-desktop runv-host";
    H.render(<RunView />, el);
    addEventListener("keydown", (e) => e.key === "Escape" && shown?.peek() && (shown.value = null));
  }
  shown.value = { chat, i };
}

function StepRow({ s, n, m }: { s: ChatStep; n: number; m: ChatMsg }) {
  const facts = [s.model, s.elapsed_ms ? dur(s.elapsed_ms) : "", s.state].filter(Boolean).join(" · ");
  const last = n === (m.steps?.length ?? 0);
  // The step that wrote the answer sends no text: the message has it.
  const out = s.md ?? (!s.text && last && !m.streaming ? m.md : undefined);
  return (
    <li class={`runv-step is-${tone(s.state)}`}>
      <div class="runv-step-head">
        <span class="runv-name">
          {n}. {s.title || s.id}
        </span>
        <span class="runv-facts">{facts}</span>
      </div>
      {(s.tools ?? []).length > 0 && (
        <div class="tool-rows">
          {(s.tools ?? []).map((l, i) => (
            <Tool key={i} line={l} done={s.state !== "running"} />
          ))}
        </div>
      )}
      {out ? (
        <div class="runv-out">
          <H.Md doc={out} />
        </div>
      ) : (
        s.text && (
          <p class="runv-out msg-text" dir="auto">
            {s.text}
          </p>
        )
      )}
      {s.error && (
        <p class="step-error" dir="auto">
          {s.error}
        </p>
      )}
    </li>
  );
}

function RunView() {
  const at = shown?.value;
  if (!at) return null;
  const pin = H.pinned.value;
  const chat = pin?.id === at.chat ? pin : readChat(H.store.topic("chat").value?.data);
  const m = chat?.id === at.chat ? chat.messages.find((x) => x.i === at.i) : undefined;
  if (!m?.run) return null;
  const r = m.run;
  const col = document.querySelector(".chatcol")?.getBoundingClientRect();
  const close = () => shown && (shown.value = null);
  const view = (
    <aside class="run-view" role="complementary" aria-label={H.t("look.run_view")} style={col ? { right: innerWidth - col.left + "px", top: col.top + "px", bottom: innerHeight - col.bottom + "px" } : undefined}>
      <div class="runv-head">
        <span class="runv-title">{H.t("look.run_view")}</span>
        <span class="runv-sub" dir="ltr">
          {[r.profile, r.state].filter(Boolean).join(" · ")}
          {time(m)}
          {r.cost_usd ? ` · $${r.cost_usd.toFixed(2)}` : ""}
        </span>
        <button type="button" class="runv-close" title={H.t("close")} aria-label={H.t("close")} onClick={close}>
          <H.Cod name="close" />
        </button>
      </div>
      <ol class="runv-steps">
        {(m.steps ?? []).map((s, n) => (
          <StepRow key={s.id || n} s={s} n={n + 1} m={m} />
        ))}
      </ol>
    </aside>
  );
  return H.phone ? (
    <H.Sheet onLater={close} label={H.t("look.run_view")}>
      {view}
    </H.Sheet>
  ) : (
    view
  );
}
