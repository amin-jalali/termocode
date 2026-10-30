// The desktop's Tasks and AI runs view, a tree as the laptop's editor
// draws one: TASKS lists the tasks the laptop can run, each with its
// state's icon (running, done, failed, not run yet), where it comes from
// and when it ran; on hover Run or Stop and Show Output (its terminal in
// the panel). AI RUNS lists the agent runs with their state, step, time
// and cost; on hover Pause, Resume and Stop. Running and stopping ask in
// the view first (Cancel first and focused). No command line is sent:
// "Use the terminal for a command."
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useState } from "preact/hooks";
import { go } from "../../lib/router";
import { useNow } from "../../lib/time";
import { toast } from "../../lib/toasts";
import { setPanes } from "../../lib/workbench";
import { link, store } from "../../session";
import { age } from "../../state/git";
import { clock, readRuns, readTasks, type RunView, type TaskView } from "../../state/tasks";
import { t, type StringKey } from "../../strings";
import { Cod } from "../../ui/icons";
import { Act, Ask, PaneSection, PaneView, Row, Tree } from "../../ui/pane";
import { gitFailed as failed } from "../git/errors";

const shut = signal<ReadonlySet<string>>(new Set());
function toggle(id: string): void {
  const n = new Set(shut.peek());
  if (n.has(id)) n.delete(id);
  else n.add(id);
  shut.value = n;
}

/** taskIcon is a task's state as the tree shows it. */
function taskIcon(x: TaskView) {
  const s = x.last?.state;
  if (s === "running") return <Cod name="loading" />;
  if (s === "ok") return <span class="c-ok"><Cod name="pass" /></span>;
  if (s === "failed") return <span class="c-err"><Cod name="error" /></span>;
  return <span class="c-dim"><Cod name="circle-outline" /></span>;
}

function output(x: TaskView): void {
  if (!x.last) return;
  setPanes({ panel: true });
  go({ tab: "term", rest: String(x.last.buf), query: {} });
}

type TaskAsk = { label: string; op: "run" | "stop" } | null;

function TaskRows({ tasks, canAct }: { tasks: TaskView[]; canAct: boolean }) {
  const [ask, setAsk] = useState<TaskAsk>(null);
  const run = (label: string) => {
    setAsk(null);
    link.request("tasks.run", { label }).then(() => toast("ok", t("tasks.started", { label })), failed);
  };
  const stop = (label: string) => {
    setAsk(null);
    link.request("tasks.terminate", { label }).catch(failed);
  };
  return (
    <>
      {tasks.map((x) => {
        const running = x.last?.state === "running";
        const state = x.last ? (x.last.state === "failed" ? t("tasks.state.failed", { code: x.last.code }) : t(running ? "tasks.state.running" : "tasks.state.ok")) : "";
        return (
          <Fragment key={x.label}>
            <Row
              level={0}
              icon={taskIcon(x)}
              label={x.label}
              desc={[x.source, x.group].filter(Boolean).join(" · ")}
              title={[x.label, x.detail, state].filter(Boolean).join(" — ")}
              data={{ label: x.label }}
              onClick={() => output(x)}
              end={x.last && <span class="tr-age">{age(x.last.when, Date.now())}</span>}
              actions={
                <>
                  {x.last && <Act icon="output" label={t("rv.output")} onClick={() => output(x)} />}
                  {canAct && (running ? <Act icon="debug-stop" label={t("stop")} onClick={() => setAsk({ label: x.label, op: "stop" })} /> : <Act icon="play" label={t("rv.run_task")} onClick={() => setAsk({ label: x.label, op: "run" })} />)}
                </>
              }
            />
            {ask?.label === x.label && (
              <Ask
                text={t(ask.op === "run" ? "tasks.confirm" : "tasks.stop_confirm", { label: x.label })}
                verb={ask.op === "run" ? t("tasks.run") : t("stop")}
                onYes={() => (ask.op === "run" ? run(x.label) : stop(x.label))}
                onNo={() => setAsk(null)}
              />
            )}
          </Fragment>
        );
      })}
    </>
  );
}

/** runIcon is an AI run's state as the tree shows it. */
function runIcon(r: RunView) {
  if (r.needsYou) return <span class="c-warn"><Cod name="warning" /></span>;
  switch (r.state) {
    case "running":
    case "pausing":
      return <span class="c-info"><Cod name="loading" /></span>;
    case "paused":
    case "resumable":
      return <Cod name="debug-pause" />;
    case "done":
      return <span class="c-ok"><Cod name="pass" /></span>;
    case "failed":
      return <span class="c-err"><Cod name="error" /></span>;
    case "stalled":
      return <span class="c-warn"><Cod name="warning" /></span>;
    default:
      return <span class="c-dim"><Cod name="circle-slash" /></span>;
  }
}

function RunRows({ runs, canAct }: { runs: RunView[]; canAct: boolean }) {
  const anyLive = runs.some((r) => r.live);
  const now = useNow(anyLive, 1000);
  const [since, setSince] = useState(() => Date.now());
  const seq = store.topic("runs").value?.seq;
  useEffect(() => setSince(Date.now()), [seq]);
  const [asking, setAsking] = useState("");
  const act = (op: string, key: string) => {
    setAsking("");
    link.request(op, { key }).catch(failed);
  };
  return (
    <>
      {runs.map((r) => {
        const elapsed = r.elapsedMs + (r.live ? Math.max(0, now - since) : 0);
        const can = canAct ? r.can : [];
        const desc = [r.profile, t(`runs.state.${r.state}` as StringKey), r.steps > 0 ? t("runs.step", { n: r.step, m: r.steps }) : "", clock(elapsed), r.cost > 0 ? `$${r.cost.toFixed(2)}` : ""].filter(Boolean).join(" · ");
        return (
          <Fragment key={r.key}>
            <Row
              level={0}
              icon={runIcon(r)}
              label={r.title}
              desc={desc}
              title={[r.title, r.chat, r.stepTitle, r.why].filter(Boolean).join(" — ")}
              data={{ key: r.key, state: r.state }}
              onClick={() => go({ tab: "chat", rest: "", query: {} })}
              actions={
                can.length > 0 && (
                  <>
                    {can.includes("pause") && <Act icon="debug-pause" label={t("runs.pause")} onClick={() => act("runs.pause", r.key)} />}
                    {can.includes("resume") && <Act icon="debug-continue" label={t("runs.resume")} onClick={() => act("runs.resume", r.key)} />}
                    {can.includes("stop") && <Act icon="debug-stop" label={t("stop")} onClick={() => setAsking(r.key)} />}
                  </>
                )
              }
            />
            {r.why && <p class="tr-note c-warn rv-why">{r.why}</p>}
            {asking === r.key && <Ask text={t("runs.stop_confirm")} verb={t("stop")} danger onYes={() => act("runs.stop", r.key)} onNo={() => setAsking("")} />}
          </Fragment>
        );
      })}
    </>
  );
}

/** DeskRun is the Run activity's tasks and AI runs; head false when it
 * sits under the debugger's title (views/run/index.tsx). */
export function DeskRun({ head = true }: { head?: boolean } = {}) {
  useEffect(() => link.subscribe("tasks", "runs"), []);
  const ts = store.topic("tasks").value;
  const rs = store.topic("runs").value;
  const { tasks, error } = readTasks(ts?.data);
  const runs = readRuns(rs?.data);
  const canAct = link.meta.value?.role !== "viewer";
  return (
    <PaneView id="run" title={t("act.run")} head={head}>
      <PaneSection id="tasks" title={t("more.tasks")} open={!shut.value.has("tasks")} onToggle={() => toggle("tasks")} count={ts ? tasks.length : undefined}>
        {!ts ? (
          <p class="pv-msg">{t("loading")}</p>
        ) : (
          <>
            {error && <p class="tr-note is-err">{t("tasks.error", { msg: error })}</p>}
            {tasks.length === 0 ? (
              <p class="pv-msg">{t("tasks.none")}</p>
            ) : (
              <Tree label={t("more.tasks")}>
                <TaskRows tasks={tasks} canAct={canAct} />
              </Tree>
            )}
            <p class="pv-msg">{t("tasks.no_free")}</p>
          </>
        )}
      </PaneSection>
      <PaneSection id="runs" title={t("more.runs")} open={!shut.value.has("runs")} onToggle={() => toggle("runs")} count={rs ? runs.length : undefined}>
        {!rs ? (
          <p class="pv-msg">{t("loading")}</p>
        ) : runs.length === 0 ? (
          <p class="pv-msg">{t("runs.none")}</p>
        ) : (
          <Tree label={t("more.runs")}>
            <RunRows runs={runs} canAct={canAct} />
          </Tree>
        )}
      </PaneSection>
    </PaneView>
  );
}
