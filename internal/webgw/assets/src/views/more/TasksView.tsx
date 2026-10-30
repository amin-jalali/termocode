// Tasks (WEB-25, web UX 5.8): the tasks the laptop can run, each with
// its last state and time. Run asks inside the row first ("Run test on
// the laptop?", Cancel first and focused); so does Stop. The output of a
// task's newest run shows under its row (its last rows, read again every
// 2 s while it runs) with "Open in Term". No command text: "Use the
// terminal for a command." #/more/tasks
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { go } from "../../lib/router";
import { toast } from "../../lib/toasts";
import { link, live, store } from "../../session";
import { age } from "../../state/git";
import { readTasks, type TaskView } from "../../state/tasks";
import { Button } from "../../ui/Button";
import { TermRow } from "../../ui/rows";
import { SkeletonRows } from "../../ui/Skeleton";
import { t } from "../../strings";
import { gitFailed as failed } from "../git/errors";
import { SubView } from "./SubView";

const TAIL_EVERY_MS = 2000;

function Ask({ text, verb, onYes, onNo }: { text: string; verb: string; onYes: () => void; onNo: () => void }) {
  const safe = useRef<HTMLButtonElement | null>(null);
  useEffect(() => safe.current?.focus(), []);
  return (
    <div class="row-ask" role="group" aria-label={text}>
      <p class="row-ask-text">{text}</p>
      <div class="choices">
        <button type="button" class="btn btn-neutral" ref={safe} onClick={onNo}>
          {t("cancel")}
        </button>
        <Button kind="primary" needsLink onClick={onYes}>
          {verb}
        </Button>
      </div>
    </div>
  );
}

function Tail({ task }: { task: TaskView }) {
  const [rows, setRows] = useState<{ lines: string[]; truncated: boolean } | null>(null);
  const running = task.last?.state === "running";
  useEffect(() => {
    if (!live.value) return;
    let stop = false;
    const read = () =>
      link.request("tasks.tail", { label: task.label }).then(
        (r) => {
          const d = (r ?? {}) as { lines?: unknown; truncated?: unknown };
          if (!stop) setRows({ lines: Array.isArray(d.lines) ? d.lines.map(String) : [], truncated: d.truncated === true });
        },
        () => !stop && setRows({ lines: [], truncated: false }),
      );
    read();
    const id = running ? setInterval(read, TAIL_EVERY_MS) : undefined;
    return () => {
      stop = true;
      clearInterval(id);
    };
  }, [task.label, task.last?.buf, running, live.value]);
  return (
    <div class="task-tail">
      {!rows ? (
        <SkeletonRows rows={4} />
      ) : rows.lines.length === 0 ? (
        <p class="sheet-note">{t("tasks.no_output")}</p>
      ) : (
        <div class="task-rows" dir="ltr">
          {rows.truncated && <p class="sheet-note">{t("tasks.older")}</p>}
          {rows.lines.map((l, i) => (
            <TermRow key={i} text={l} />
          ))}
        </div>
      )}
      {task.last && (
        <Button small onClick={() => go({ tab: "term", rest: String(task.last?.buf), query: {} })}>
          {t("tasks.open_term")}
        </Button>
      )}
    </div>
  );
}

function TaskRow({ task, canAct, open, onToggle }: { task: TaskView; canAct: boolean; open: boolean; onToggle: () => void }) {
  const [ask, setAsk] = useState<"" | "run" | "stop">("");
  const running = task.last?.state === "running";
  const state = task.last
    ? task.last.state === "failed"
      ? t("tasks.state.failed", { code: task.last.code })
      : t(task.last.state === "running" ? "tasks.state.running" : "tasks.state.ok")
    : "";
  const run = () => {
    setAsk("");
    link.request("tasks.run", { label: task.label }).then(() => {
      toast("ok", t("tasks.started", { label: task.label }));
      if (!open) onToggle();
    }, failed);
  };
  const stop = () => {
    setAsk("");
    link.request("tasks.terminate", { label: task.label }).catch(failed);
  };
  return (
    <section class="task" data-label={task.label}>
      <div class="task-line">
        <button type="button" class="task-open" aria-expanded={open ? "true" : "false"} onClick={onToggle} disabled={!task.last}>
          <span class={`task-dot st-${task.last?.state ?? "none"}`} aria-hidden="true" />
          <span class="task-name">
            <bdi class="task-label">{task.label}</bdi>
            <span class="task-meta" dir="ltr">
              {task.source}
              {task.group && ` · ${task.group}`}
              {task.last && ` · ${state} · ${age(task.last.when, Date.now())}`}
            </span>
          </span>
        </button>
        {canAct &&
          (running ? (
            <Button small kind="neutral" needsLink onClick={() => setAsk("stop")}>
              {t("stop")}
            </Button>
          ) : (
            <Button small kind="primary" needsLink onClick={() => setAsk("run")}>
              {t("tasks.run")}
            </Button>
          ))}
      </div>
      {ask === "run" && <Ask text={t("tasks.confirm", { label: task.label })} verb={t("tasks.run")} onYes={run} onNo={() => setAsk("")} />}
      {ask === "stop" && <Ask text={t("tasks.stop_confirm", { label: task.label })} verb={t("stop")} onYes={stop} onNo={() => setAsk("")} />}
      {open && task.last && <Tail task={task} />}
    </section>
  );
}

export function TasksView() {
  useEffect(() => link.subscribe("tasks"), []);
  const st = store.topic("tasks").value;
  const { tasks, error } = readTasks(st?.data);
  const canAct = link.meta.value?.role !== "viewer";
  const [open, setOpen] = useState("");
  return (
    <SubView title={t("more.tasks")}>
      {!st ? (
        <SkeletonRows rows={6} />
      ) : (
        <div class="tasks">
          {error && <p class="search-note">{t("tasks.error", { msg: error })}</p>}
          {tasks.length === 0 && <p class="empty">{t("tasks.none")}</p>}
          {tasks.map((x) => (
            <TaskRow key={x.label} task={x} canAct={canAct} open={open === x.label} onToggle={() => setOpen(open === x.label ? "" : x.label)} />
          ))}
          <p class="tasks-free">{t("tasks.no_free")}</p>
        </div>
      )}
    </SubView>
  );
}
