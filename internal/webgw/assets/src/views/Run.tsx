// The run of a pipeline answer (web UX 5.2 N): one mono line with the
// profile, state, time and cost, then one row per step that opens to show
// its tool lines, its answer and its error. Read only.
import { h } from "preact";
import type { ChatMsg, ChatStep } from "../../../../webproto/proto";
import { t } from "../strings";
import { Md } from "../ui/Markdown";

/** elapsed writes a duration like the TUI: "12s", "2m 05s", "1h 03m". */
export function elapsed(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`;
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m`;
}

function Step({ s, n }: { s: ChatStep; n: number }) {
  const facts = [String(n), s.title, s.model, s.state].filter((x): x is string => !!x);
  let answer = null;
  if (s.md) answer = <Md doc={s.md} />;
  else if (s.text) answer = <p class="msg-text" dir="auto">{s.text}</p>;
  // A step that failed opens by itself: its error should not hide.
  return (
    <details class={`step st-${s.state ?? "none"}`} open={!!s.error}>
      <summary class="step-head" dir="ltr">
        {facts.join(" · ")}
      </summary>
      <div class="step-body">
        {(s.tools ?? []).map((line, i) => (
          <div key={i} class="msg-tool" dir="ltr">
            {line}
          </div>
        ))}
        {answer}
        {s.error && (
          <p class="step-error" dir="auto">
            {s.error}
          </p>
        )}
      </div>
    </details>
  );
}

export function Run({ m }: { m: ChatMsg }) {
  const r = m.run;
  const steps = m.steps ?? [];
  if (!r && steps.length === 0) return null;
  const facts = r
    ? [r.profile, r.state, r.elapsed_ms ? elapsed(r.elapsed_ms) : "", r.cost_usd ? "$" + r.cost_usd.toFixed(2) : ""].filter((x): x is string => !!x)
    : [];
  return (
    <div class="run">
      {r && (
        <div class={`run-head st-${r.state}`} dir="ltr">
          <span class="run-mark" aria-hidden="true">▸</span>
          <span>{facts.join(" · ")}</span>
        </div>
      )}
      {r?.plan_wait && <p class="run-wait">{t("run.plan_wait")}</p>}
      {steps.map((s, i) => (
        <Step key={s.id || i} s={s} n={i + 1} />
      ))}
    </div>
  );
}
