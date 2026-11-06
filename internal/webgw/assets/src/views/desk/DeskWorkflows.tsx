// The desktop's Workflows view (WFX, plan §5.7): the side bar lists the
// laptop's workflows with their state and schedule and the recent runs; a
// run opens as an editor tab with its counts, the last lines it wrote and
// the write card that waits for it, inline, with the cards' own rules:
// Deny first and focused, Allow once arms first, no "Allow for this
// workflow". Stop asks inside the tab, Cancel first and focused. Run,
// on/off, edit and the allow-list stay on the laptop.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useState } from "preact/hooks";
import type { Card as CardData, WorkflowItem, WorkflowRun } from "../../../../../webproto/proto";
import { registerSide } from "../../lib/views";
import { toast } from "../../lib/toasts";
import { wfTab } from "../../lib/vtabs";
import { showFile } from "../../lib/workbench";
import { OfflineError, ReqError } from "../../net/link";
import { cards, link, live, store } from "../../session";
import { riskyChoice, safeChoice } from "../../state/cards";
import { hhmm } from "../../state/more";
import { readWorkflows, runCards } from "../../state/workflows";
import { t, type StringKey } from "../../strings";
import { Card } from "../../ui/Card";
import { errText } from "../../ui/CardSheet";
import { Cod } from "../../ui/icons";
import { Ask, PaneSection, PaneView, Row, Tree } from "../../ui/pane";

const shut = signal<ReadonlySet<string>>(new Set());
function toggle(id: string): void {
  const n = new Set(shut.peek());
  if (n.has(id)) n.delete(id);
  else n.add(id);
  shut.value = n;
}

const wfIcon: Record<WorkflowItem["state"], [string, string]> = {
  running: ["sync", "c-ok"],
  on: ["circle-filled", "c-ok"],
  off: ["circle-outline", ""],
  untrusted: ["shield", "c-warn"],
  broken: ["error", "c-err"],
};

/** runKey names the run word the phone's runs view shows too (views/more/RunsView.tsx). */
const runKey = (state: string) => `runs.state.${state}` as StringKey;

const wfWord: Record<WorkflowItem["state"], StringKey> = {
  running: runKey("running"),
  on: "wfv.on",
  off: "wfv.off",
  untrusted: "wfv.untrusted",
  broken: "wfv.broken",
};

function runIcon(r: WorkflowRun): [string, string] {
  if (r.waiting) return ["warning", "c-warn"];
  switch (r.state) {
    case "running":
      return ["sync", "c-ok"];
    case "done":
      return ["pass", "c-ok"];
    case "failed":
      return ["error", "c-err"];
    case "paused":
      return ["debug-pause", "c-warn"];
    case "interrupted":
      return ["debug-disconnect", "c-warn"];
  }
  return ["circle-slash", ""];
}

const RUN_STATES: ReadonlySet<string> = new Set(["running", "done", "failed", "cancelled", "paused"]);

function runWord(r: WorkflowRun): string {
  if (r.state === "interrupted") return t("wfv.interrupted");
  return RUN_STATES.has(r.state) ? t(runKey(r.state)) : r.state;
}

/** runCounts is "3 items · 1 failed · wrote 1". */
function runCounts(r: WorkflowRun): string {
  const out: string[] = [];
  if (r.items) out.push(r.items === 1 ? t("wfv.item_one") : t("wfv.items", { n: r.items }));
  if (r.failed) out.push(t("wfv.failed", { n: r.failed }));
  if (r.wrote) out.push(t("wfv.wrote", { n: r.wrote }));
  return out.join(" · ");
}

function openRun(r: WorkflowRun): void {
  showFile(wfTab(r.workflow, r.id), true);
}

export function DeskWorkflows() {
  useEffect(() => link.subscribe("workflows"), []);
  const st = store.topic("workflows").value;
  const { workflows, runs } = readWorkflows(st?.data);
  return (
    <PaneView id="workflows" title={t("more.workflows")}>
      <div class="pv-scroll">
        {!st ? (
          <p class="pv-msg">{t("loading")}</p>
        ) : (
          <>
            <PaneSection id="list" title={t("wfv.list")} open={!shut.value.has("list")} onToggle={() => toggle("list")} count={workflows.length} fixed>
              {workflows.length === 0 ? (
                <p class="pv-msg">{t("wf.none")}</p>
              ) : (
                <Tree label={t("wfv.list")}>
                  {workflows.map((w) => {
                    const [icon, tone] = wfIcon[w.state] ?? ["circle-outline", ""];
                    const newest = runs.find((r) => r.workflow === w.name);
                    return (
                      <Row
                        key={w.name}
                        level={0}
                        icon={
                          <span class={tone}>
                            <Cod name={icon} />
                          </span>
                        }
                        label={<bdi dir="auto">{w.name}</bdi>}
                        desc={[t(wfWord[w.state] ?? "wfv.off"), w.when, w.next ? hhmm(w.next) : ""].filter(Boolean).join(" · ")}
                        title={w.title || w.name}
                        data={{ workflow: w.name }}
                        onClick={newest ? () => openRun(newest) : undefined}
                      />
                    );
                  })}
                </Tree>
              )}
            </PaneSection>
            <PaneSection id="runs" title={t("more.runs")} open={!shut.value.has("runs")} onToggle={() => toggle("runs")} count={runs.length}>
              {runs.length === 0 ? (
                <p class="pv-msg">{t("wfv.no_runs")}</p>
              ) : (
                <Tree label={t("more.runs")}>
                  {runs.map((r) => {
                    const [icon, tone] = runIcon(r);
                    return (
                      <Row
                        key={r.id}
                        level={0}
                        icon={
                          <span class={tone}>
                            <Cod name={icon} />
                          </span>
                        }
                        label={<bdi dir="auto">{r.workflow}</bdi>}
                        desc={[r.waiting ? t("wf.waiting", { s: r.waiting }) : runWord(r), r.dry ? t("wfv.dry") : "", runCounts(r)].filter(Boolean).join(" · ")}
                        title={r.id}
                        data={{ run: r.id }}
                        end={<span class="tr-age">{hhmm(r.started)}</span>}
                        onClick={() => openRun(r)}
                      />
                    );
                  })}
                </Tree>
              )}
            </PaneSection>
            <p class="pv-msg">{t("wfv.body_laptop")}</p>
          </>
        )}
      </div>
    </PaneView>
  );
}

registerSide("workflows", () => <DeskWorkflows />);

/** WriteCard is a run's waiting write card, inline: one cards.answer;
 * the first answer wins. */
function WriteCard({ c, canAct }: { c: CardData; canAct: boolean }) {
  const [sending, setSending] = useState(false);
  const [gone, setGone] = useState(false);
  const answer = (which: "safe" | "risky") => {
    const choice = which === "safe" ? safeChoice(c) : riskyChoice(c);
    if (!choice || sending) return;
    setSending(true);
    link.request("cards.answer", { id: c.id, choice: choice.id }).then(
      () => {
        setSending(false);
        if (which === "safe") toast("info", t("toast.denied"));
        else toast("ok", t("toast.allowed"));
      },
      (e: unknown) => {
        setSending(false);
        if (e instanceof ReqError && e.code === "gone") setGone(true);
        else if (!(e instanceof OfflineError)) toast("error", errText(e));
      },
    );
  };
  const state = gone ? "gone" : !live.value ? "offline" : !canAct ? "viewonly" : "open";
  return (
    <Card id={c.id} kind="workflow" mode="inline" state={state} subject={c.title} onAnswer={answer} onClose={() => setGone(false)}>
      {c.body && <p class="card-text">{c.body}</p>}
    </Card>
  );
}

/** WorkflowRunTab is one run's page in an editor tab. */
export function WorkflowRunTab({ run }: { run: string; slot: string }) {
  useEffect(() => link.subscribe("workflows"), []);
  const [asking, setAsking] = useState(false);
  const { runs } = readWorkflows(store.topic("workflows").value?.data);
  const r = runs.find((x) => x.id === run);
  const canAct = link.meta.value?.role === "controller";
  if (!r) {
    return (
      <section class="view mcpd wfr" data-view="workflow-run" data-run={run}>
        <p class="mcpd-empty">{store.topic("workflows").value ? t("wfv.gone") : t("loading")}</p>
      </section>
    );
  }
  const [icon, tone] = runIcon(r);
  const waiting = runCards(cards.value, r);
  const stop = () => {
    setAsking(false);
    link.request("workflows.stop", { run: r.id }).catch((e: unknown) => {
      if (!(e instanceof OfflineError)) toast("error", errText(e));
    });
  };
  return (
    <section class="view mcpd wfr" data-view="workflow-run" data-run={r.id} aria-labelledby="wfr-h">
      <header class="mcpd-head">
        <span class={`mcpd-icon ${tone}`}>
          <Cod name={icon} />
        </span>
        <div>
          <h1 id="wfr-h">{r.workflow}</h1>
          <p class="mcpd-sub" dir="ltr">
            {[runWord(r), r.dry ? t("wfv.dry") : "", t("wfv.started", { time: hhmm(r.started) }), r.id].filter(Boolean).join(" · ")}
          </p>
          {runCounts(r) && <p class="mcpd-sub">{runCounts(r)}</p>}
          {r.resumed ? <p class="mcpd-sub">{t("wfv.resumed")}</p> : null}
        </div>
      </header>
      {r.state === "running" && canAct && !asking && (
        <div class="wfr-acts">
          <button type="button" class="pv-btn is-2 wfr-stop" data-act="stop" onClick={() => setAsking(true)}>
            <Cod name="debug-stop" /> {t("stop")}
          </button>
        </div>
      )}
      {asking && <Ask text={t("wf.stop_confirm")} verb={t("stop")} danger onYes={stop} onNo={() => setAsking(false)} />}
      {(r.waiting || waiting.length > 0) && (
        <>
          <h2 class="mcpd-title">{t("wfv.needs")}</h2>
          {r.waiting && <p class="mcpd-sub">{t("wf.waiting", { s: r.waiting })}</p>}
          {waiting.map((c) => (
            <WriteCard key={c.id} c={c} canAct={canAct} />
          ))}
        </>
      )}
      {r.tail && r.tail.length > 0 && (
        <>
          <h2 class="mcpd-title">{t("wfv.last_lines")}</h2>
          <pre class="wfr-log" dir="ltr">
            {r.tail.join("\n")}
          </pre>
        </>
      )}
      <p class="mcpd-empty">{t("wfv.body_laptop")}</p>
    </section>
  );
}
