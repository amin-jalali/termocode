// Workflows (WFX-19, plan §5.7): the laptop's workflows and their recent
// runs, a running run's last lines and the step whose write waits. The
// write itself is answered on its card (Deny first, Allow once). Stop
// asks inside the row first, Cancel first and focused. #/more/workflows
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { WorkflowRun, WorkflowsSnap } from "../../../../../webproto/proto";
import { link, store } from "../../session";
import { Button } from "../../ui/Button";
import { SkeletonRows } from "../../ui/Skeleton";
import { t } from "../../strings";
import { gitFailed as failed } from "../git/errors";
import { SubView } from "./SubView";

const mark: Record<string, string> = { running: "◉", on: "●", off: "○", untrusted: "◌", broken: "✗" };

function RunRow({ r, canAct }: { r: WorkflowRun; canAct: boolean }) {
  const [asking, setAsking] = useState(false);
  const safe = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (asking) safe.current?.focus();
  }, [asking]);
  const stop = () => {
    setAsking(false);
    link.request("workflows.stop", { run: r.id }).catch(failed);
  };
  return (
    <section class={`run st-${r.state}`} data-key={r.id}>
      <p class="run-title" dir="auto">
        {r.workflow}
        {r.dry ? " · dry" : ""}
      </p>
      <p class="run-meta" dir="ltr">
        {r.state}
        {r.items ? ` · ${r.items} items${r.failed ? ` (${r.failed} ✗)` : ""}` : ""}
        {r.wrote ? ` · wrote ${r.wrote}` : ""}
        {r.resumed ? " · ↻" : ""}
      </p>
      {r.waiting && <p class="run-why">{t("wf.waiting", { s: r.waiting })}</p>}
      {r.tail && r.tail.length > 0 && (
        <pre class="card-subject" dir="ltr">
          {r.tail.slice(-6).join("\n")}
        </pre>
      )}
      {r.state === "running" &&
        canAct &&
        (asking ? (
          <div class="row-ask" role="group" aria-label={t("wf.stop_confirm")}>
            <p class="row-ask-text">{t("wf.stop_confirm")}</p>
            <div class="choices">
              <button type="button" class="btn btn-neutral" ref={safe} onClick={() => setAsking(false)}>
                {t("cancel")}
              </button>
              <Button kind="danger" needsLink onClick={stop}>
                {t("stop")}
              </Button>
            </div>
          </div>
        ) : (
          <div class="choices">
            <Button small needsLink onClick={() => setAsking(true)}>
              {t("stop")}
            </Button>
          </div>
        ))}
    </section>
  );
}

export function WorkflowsView() {
  useEffect(() => link.subscribe("workflows"), []);
  const st = store.topic("workflows").value;
  const d = st?.data as WorkflowsSnap | undefined;
  const canAct = link.meta.value?.role !== "viewer";
  return (
    <SubView title={t("more.workflows")}>
      {!d ? (
        <SkeletonRows rows={4} />
      ) : (d.workflows ?? []).length === 0 ? (
        <p class="empty">{t("wf.none")}</p>
      ) : (
        <div class="runs">
          <ul class="rows">
            {d.workflows.map((w) => (
              <li key={w.name} class="row" dir="ltr">
                {mark[w.state] ?? "·"} {w.name}
                {w.when ? ` · ${w.when}` : ""}
              </li>
            ))}
          </ul>
          {(d.runs ?? []).map((r) => (
            <RunRow key={r.id} r={r} canAct={canAct} />
          ))}
        </div>
      )}
    </SubView>
  );
}
