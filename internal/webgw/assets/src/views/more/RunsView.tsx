// Runs (WEB-25, web UX 5.8): the AI runs with their state, step, time
// and cost. Stop asks inside the card first ("Stop this run? It can be
// started again from the laptop.", Cancel first and focused); Pause and
// Resume act at once. A run lists what the web may do with it. #/more/runs
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { useNow } from "../../lib/time";
import { link, store } from "../../session";
import { clock, readRuns, type RunView } from "../../state/tasks";
import { Button } from "../../ui/Button";
import { SkeletonRows } from "../../ui/Skeleton";
import { t, type StringKey } from "../../strings";
import { gitFailed as failed } from "../git/errors";
import { SubView } from "./SubView";

function RunCard({ run, since, now, canAct }: { run: RunView; since: number; now: number; canAct: boolean }) {
  const [asking, setAsking] = useState(false);
  const safe = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (asking) safe.current?.focus();
  }, [asking]);
  const act = (op: string) => {
    setAsking(false);
    link.request(op, { key: run.key }).catch(failed);
  };
  const elapsed = run.elapsedMs + (run.live ? Math.max(0, now - since) : 0);
  const can = canAct ? run.can : [];
  return (
    <section class={`run st-${run.state}${run.needsYou ? " needs-you" : ""}`} data-key={run.key}>
      <p class="run-title" dir="auto">
        {run.title}
      </p>
      <p class="run-meta" dir="ltr">
        {run.profile && `${run.profile} · `}
        {t(`runs.state.${run.state}` as StringKey)}
        {run.steps > 0 && ` · ${t("runs.step", { n: run.step, m: run.steps })}`}
        {` · ${clock(elapsed)}`}
        {run.cost > 0 && ` · $${run.cost.toFixed(2)}`}
      </p>
      {run.why && (
        <p class="run-why" dir="auto">
          {run.why}
        </p>
      )}
      {asking ? (
        <div class="row-ask" role="group" aria-label={t("runs.stop_confirm")}>
          <p class="row-ask-text">{t("runs.stop_confirm")}</p>
          <div class="choices">
            <button type="button" class="btn btn-neutral" ref={safe} onClick={() => setAsking(false)}>
              {t("cancel")}
            </button>
            <Button kind="danger" needsLink onClick={() => act("runs.stop")}>
              {t("stop")}
            </Button>
          </div>
        </div>
      ) : (
        can.length > 0 && (
          <div class="choices">
            {can.includes("stop") && (
              <Button small needsLink onClick={() => setAsking(true)}>
                {t("stop")}
              </Button>
            )}
            {can.includes("pause") && (
              <Button small needsLink onClick={() => act("runs.pause")}>
                {t("runs.pause")}
              </Button>
            )}
            {can.includes("resume") && (
              <Button small kind="primary" needsLink onClick={() => act("runs.resume")}>
                {t("runs.resume")}
              </Button>
            )}
          </div>
        )
      )}
    </section>
  );
}

export function RunsView() {
  useEffect(() => link.subscribe("runs"), []);
  const st = store.topic("runs").value;
  const runs = readRuns(st?.data);
  const anyLive = runs.some((r) => r.live);
  const now = useNow(anyLive, 1000);
  const [since, setSince] = useState(() => Date.now());
  useEffect(() => setSince(Date.now()), [st?.seq]); // a new list: its clocks are fresh
  const canAct = link.meta.value?.role !== "viewer";
  return (
    <SubView title={t("more.runs")}>
      {!st ? (
        <SkeletonRows rows={6} />
      ) : runs.length === 0 ? (
        <p class="empty">{t("runs.none")}</p>
      ) : (
        <div class="runs">
          {runs.map((r) => (
            <RunCard key={r.key} run={r} since={since} now={now} canAct={canAct} />
          ))}
        </div>
      )}
    </SubView>
  );
}
