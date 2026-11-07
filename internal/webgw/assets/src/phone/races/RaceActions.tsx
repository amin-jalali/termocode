// The race block's buttons on the phone (MOB-62, mobile plan 2.6):
// Summarise · Verify · Stop lanes. A button that cannot act is greyed
// with a line that says why. Stop opens a sheet with Cancel first and
// focused, Stop armed after 600 ms; the work stays in the worktrees. The
// summary shows here as soon as it comes back (the races topic brings it
// too). Offline or as a viewer every button is off. 44 px targets.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { link, live } from "../../session";
import { t } from "../../strings";
import { rs } from "./strings";
import { useArming } from "../../ui/arming";
import { Sheet } from "../../ui/Sheet";
import { gitErrText } from "../../views/git/errors";
import { canSummarise, canVerify, hint, stop, summarise, verify, working, type RaceLike, type RaceSummary } from "./raceops";

type Busy = "" | "summary" | "verify" | "stop";

function StopSheet({ n, onCancel, onStop }: { n: number; onCancel: () => void; onStop: () => void }) {
  const box = useRef<HTMLDivElement | null>(null);
  const safe = useRef<HTMLButtonElement | null>(null);
  const arm = useArming(box);
  useEffect(() => {
    safe.current?.focus();
  }, []);
  return (
    <Sheet onLater={onCancel} label={rs("race.stop")}>
      <div class="ask" ref={box} data-lanes={n}>
        <p class="ask-text">{rs("race.stop_body")}</p>
        <div class="choices">
          <button type="button" class="btn btn-neutral race-btn" ref={safe} onClick={onCancel}>
            {t("cancel")}
          </button>
          <button
            type="button"
            class={`btn btn-danger arm race-btn${arm.armed ? " is-armed" : ""}`}
            aria-disabled={arm.armed && live.value ? undefined : "true"}
            onPointerDown={arm.pressBegan}
            onKeyDown={(e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && arm.pressBegan()}
            onClick={() => arm.pressOK() && live.value && onStop()}
          >
            <span class="arm-fill" aria-hidden="true" />
            <span class="arm-label">{rs("race.stop")}</span>
          </button>
        </div>
      </div>
    </Sheet>
  );
}

export function RaceActions({ race }: { race: RaceLike }) {
  const [busy, setBusy] = useState<Busy>("");
  const [asking, setAsking] = useState(false);
  const [note, setNote] = useState("");
  const [sum, setSum] = useState<RaceSummary | null>(null);
  const canAct = live.value && link.meta.value?.role === "controller";
  const n = working(race);

  const run = (what: Busy, p: () => Promise<{ message?: string } | RaceSummary>) => {
    if (busy) return;
    setBusy(what);
    setNote("");
    p().then(
      (r) => {
        if ("lines" in r) setSum(r);
        else setNote(r.message ?? "");
        setBusy("");
      },
      (e: unknown) => {
        setNote(gitErrText(e));
        setBusy("");
      },
    );
  };

  const why = [hint(race, "summary"), hint(race, "verify")].filter(Boolean);
  return (
    <div class="race-acts" data-race={race.id}>
      <div class="race-row">
        <button
          type="button"
          class="btn btn-neutral race-btn"
          disabled={!canAct || !canSummarise(race) || busy !== ""}
          onClick={() => run("summary", () => summarise(link, race.id))}
        >
          {busy === "summary" ? rs("race.summarising") : rs("race.summarise")}
        </button>
        <button
          type="button"
          class="btn btn-neutral race-btn"
          disabled={!canAct || !canVerify(race) || busy !== ""}
          onClick={() => run("verify", () => verify(link, race.id))}
        >
          {rs("race.verify")}
        </button>
        <button type="button" class="btn btn-danger race-btn" disabled={!canAct || n === 0 || busy !== ""} onClick={() => setAsking(true)}>
          <span aria-hidden="true">■ </span>
          {rs("race.stop")}
        </button>
      </div>
      {why.map((k) => (
        <p class="race-hint" key={k}>
          {k ? rs(k) : ""}
        </p>
      ))}
      {note && (
        <p class="race-note" role="status">
          {note}
        </p>
      )}
      {sum && sum.lines.length > 0 && (
        <section class="race-sum" aria-label={rs("race.summary")}>
          <ul>
            {sum.lines.map((l, i) => (
              <li key={i}>{l}</li>
            ))}
          </ul>
          {sum.cost && <p class="race-cost">{sum.cost}</p>}
        </section>
      )}
      {asking && (
        <StopSheet
          n={n}
          onCancel={() => setAsking(false)}
          onStop={() => {
            setAsking(false);
            run("stop", () => stop(link, race.id));
          }}
        />
      )}
    </div>
  );
}
