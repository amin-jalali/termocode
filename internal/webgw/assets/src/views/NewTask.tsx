// New task (CMP-49): start a background task on the laptop from the
// phone or the desktop page. The form reads what the laptop offers
// (worktrees.task_form: the start points, the installed agents, what the
// repository's setup does), then asks here first (Cancel first and
// focused) and sends worktrees.new_task. The laptop makes a worktree and
// its branch (drafted from the task when the name is empty) and starts
// the agent there with the task typed in. Used on a phone under More →
// New task (#/more/new-task) and on the desktop page from the Worktrees
// view's + button.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { WorktreeTaskDone, WorktreeTaskForm } from "../../../../webproto/proto";
import { toast } from "../lib/toasts";
import { link, live } from "../session";
import { t } from "../strings";
import { Button } from "../ui/Button";
import { gitErrText, gitFailed as failed } from "./git/errors";

/** readTaskForm is a task_form answer (null: odd). */
export function readTaskForm(data: unknown): WorktreeTaskForm | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as Partial<WorktreeTaskForm>;
  return (d.state === "ready" || d.state === "none") && Array.isArray(d.refs) && Array.isArray(d.agents) ? (d as WorktreeTaskForm) : null;
}

/** taskReady: the form can be sent (a task line and an agent). */
export function taskReady(task: string, agent: string): boolean {
  return task.trim() !== "" && !task.includes("\n") && agent !== "";
}

const MAX_TASK = 4096;
const MAX_NAME = 100;

export function NewTaskForm({ desk, onDone }: { desk?: boolean; onDone?: () => void }) {
  const [form, setForm] = useState<WorktreeTaskForm | null>(null);
  const [err, setErr] = useState("");
  const [task, setTask] = useState("");
  const [name, setName] = useState("");
  const [agent, setAgent] = useState("");
  const [from, setFrom] = useState("");
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const safe = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (!live.value || link.meta.peek()?.role === "viewer") return;
    let stop = false;
    link.request("worktrees.task_form", {}).then(
      (r) => {
        if (stop) return;
        const f = readTaskForm(r);
        setForm(f);
        if (f) {
          setAgent(f.agents[0]?.id ?? "");
          setFrom(f.from ?? "");
        }
      },
      (e) => !stop && setErr(gitErrText(e)),
    );
    return () => {
      stop = true;
    };
  }, [live.value]);
  useEffect(() => {
    if (asking) safe.current?.focus();
  }, [asking]);

  if (link.meta.value?.role === "viewer") return <p class="nt-msg">{t("nt.viewer_body")}</p>;
  if (err) return <p class="nt-msg">{err}</p>;
  if (!form) return <p class="nt-msg">{t("loading")}</p>;
  if (form.state === "none") return <p class="nt-msg">{t("nt.none_body")}</p>;
  if (form.agents.length === 0) return <p class="nt-msg">{t("nt.no_agents_body")}</p>;

  const title = form.agents.find((a) => a.id === agent)?.title ?? agent;
  const ready = taskReady(task, agent) && !busy;
  const send = () => {
    setAsking(false);
    setBusy(true);
    const args: Record<string, string> = { agent, task: task.trim() };
    if (name.trim()) args.name = name.trim();
    if (from && from !== form.from) args.from = from;
    link.request("worktrees.new_task", args).then(
      (r) => {
        setBusy(false);
        const d = (r ?? {}) as WorktreeTaskDone;
        toast("ok", t("nt.started", { branch: d.branch ?? "" }), d.note);
        setTask("");
        setName("");
        onDone?.();
      },
      (e) => {
        setBusy(false);
        failed(e);
      },
    );
  };
  const box = desk ? "pv-input" : "nt-input";
  return (
    <form
      class={`nt-form${desk ? " is-desk" : ""}`}
      aria-label={t("nt.title")}
      onSubmit={(e) => {
        e.preventDefault();
        if (ready) setAsking(true);
      }}
    >
      <p class="nt-hint">{t("nt.body")}</p>
      <label class="nt-field">
        <span class="nt-label">{t("nt.task")}</span>
        <span class={box}>
          <input value={task} maxLength={MAX_TASK} placeholder={t("nt.task_ph")} onInput={(e) => setTask((e.currentTarget as HTMLInputElement).value)} />
        </span>
      </label>
      <label class="nt-field">
        <span class="nt-label">{t("nt.agent")}</span>
        <select class="nt-sel" value={agent} onChange={(e) => setAgent((e.currentTarget as HTMLSelectElement).value)}>
          {form.agents.map((a) => (
            <option key={a.id} value={a.id}>
              {a.title}
            </option>
          ))}
        </select>
      </label>
      <label class="nt-field">
        <span class="nt-label">{t("nt.name")}</span>
        <span class={box}>
          <input value={name} maxLength={MAX_NAME} placeholder={t("nt.name_ph")} onInput={(e) => setName((e.currentTarget as HTMLInputElement).value)} />
        </span>
      </label>
      {form.refs.length > 0 && (
        <label class="nt-field">
          <span class="nt-label">{t("nt.from")}</span>
          <select class="nt-sel" value={from} onChange={(e) => setFrom((e.currentTarget as HTMLSelectElement).value)}>
            {form.refs.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
        </label>
      )}
      {form.base && <p class="nt-hint">{t("nt.where", { base: form.base })}</p>}
      {form.setup === "trusted" && <p class="nt-hint">{t("nt.setup_trusted")}</p>}
      {form.setup === "asks" && <p class="nt-hint">{t("nt.setup_asks")}</p>}
      {asking ? (
        <div class="row-ask" role="group" aria-label={t("nt.ask", { agent: title })}>
          <p class="row-ask-text">{t("nt.ask", { agent: title })}</p>
          <div class="choices">
            <button type="button" class={desk ? "pv-btn is-2" : "btn btn-neutral"} ref={safe} onClick={() => setAsking(false)}>
              {t("cancel")}
            </button>
            <Button kind="primary" needsLink onClick={send}>
              {t("nt.start")}
            </Button>
          </div>
        </div>
      ) : (
        <div class="choices">
          {onDone && (
            <button type="button" class={desk ? "pv-btn is-2" : "btn btn-neutral"} onClick={onDone}>
              {t("cancel")}
            </button>
          )}
          <Button kind="primary" needsLink disabled={!ready} onClick={() => setAsking(true)}>
            {t("nt.start")}
          </Button>
        </div>
      )}
    </form>
  );
}
