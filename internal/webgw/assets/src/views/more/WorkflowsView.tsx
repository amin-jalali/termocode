// Workflows (WFX-19, plan §5.7): the laptop's workflows and their recent
// runs, a running run's last lines and the step whose write waits. The
// write itself is answered on its card (Deny first, Allow once). Stop
// asks inside the row first, Cancel first and focused. #/more/workflows
//
// MOB-60: a trusted workflow can be dry run from here (asked inside the
// row first: plan steps cost AI money; nothing is written) or run for
// real, which only the laptop confirms (AskLaptop: Cancel first, the
// action arms after 600 ms). A file nobody trusted yet says so: trust
// stays on the laptop, where the file is read.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { WorkflowItem, WorkflowRun, WorkflowsSnap } from "../../../../../webproto/proto";
import { toast } from "../../lib/toasts";
import { link, store } from "../../session";
import { AskLaptop } from "../../ui/AskLaptop";
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

// canStart: the workflow may start from the phone (the laptop checks again).
const canStart = (w: WorkflowItem) => w.state === "on" || w.state === "off";

function WorkflowRow({ w, canAct, onRun }: { w: WorkflowItem; canAct: boolean; onRun(w: WorkflowItem): void }) {
  const [asking, setAsking] = useState(false);
  const safe = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (asking) safe.current?.focus();
  }, [asking]);
  const dry = () => {
    setAsking(false);
    link.request("workflows.dry_run", { name: w.name }).then(() => toast("ok", t("wf.dry_started", { s: w.name })), failed);
  };
  return (
    <li class="row wf-row" dir="ltr" data-key={w.name}>
      <p class="run-title">
        <span aria-hidden="true">{mark[w.state] ?? "·"}</span> {w.name}
        <span class="sr-only"> {w.state}</span>
        {w.when ? ` · ${w.when}` : ""}
      </p>
      {w.state === "untrusted" && <p class="run-why">{t("wf.trust_laptop")}</p>}
      {w.state === "broken" && <p class="run-why">{t("wf.broken")}</p>}
      {canAct &&
        canStart(w) &&
        (asking ? (
          <div class="row-ask" role="group" aria-label={t("wf.dry_confirm")}>
            <p class="row-ask-text">{t("wf.dry_confirm")}</p>
            <p class="ask-note">{t("wf.dry_note")}</p>
            <div class="choices">
              <button type="button" class="btn btn-neutral" ref={safe} onClick={() => setAsking(false)}>
                {t("cancel")}
              </button>
              <Button needsLink onClick={dry}>
                {t("wf.dry_run")}
              </Button>
            </div>
          </div>
        ) : (
          <div class="choices">
            <Button small needsLink onClick={() => setAsking(true)}>
              {t("wf.dry_run")}
            </Button>
            <Button small needsLink onClick={() => onRun(w)}>
              {t("wf.run")}
            </Button>
          </div>
        ))}
    </li>
  );
}

export function WorkflowsView() {
  useEffect(() => link.subscribe("workflows"), []);
  const st = store.topic("workflows").value;
  const d = st?.data as WorkflowsSnap | undefined;
  const canAct = link.meta.value?.role !== "viewer";
  const [run, setRun] = useState<WorkflowItem | null>(null);
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
              <WorkflowRow key={w.name} w={w} canAct={canAct} onRun={setRun} />
            ))}
          </ul>
          {(d.runs ?? []).map((r) => (
            <RunRow key={r.id} r={r} canAct={canAct} />
          ))}
        </div>
      )}
      {run && (
        <AskLaptop
          label={t("wf.run_label", { s: run.name })}
          text={t("wf.run_body", { s: run.name })}
          send={() => link.request("workflows.run", { name: run.name })}
          onDone={() => setRun(null)}
        />
      )}
    </SubView>
  );
}
