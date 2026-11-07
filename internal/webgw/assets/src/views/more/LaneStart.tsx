// Start an agent in a new lane (MOB-65, mobile plan §3.9): pick one of
// the laptop's agents (names and a sign-in word only), type the task and,
// if wanted, the branch or tag it starts from. Next shows the question;
// the laptop then asks on its own screen (Cancel first) and makes the
// worktree and starts the agent only after its yes. The new lane shows
// in the worktrees list. Signing in an agent stays on the laptop.
import { Fragment, h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { AILaneAgent, AILaneAgents } from "../../../../../webproto/proto";
import { hubSays } from "../../lib/ai";
import { link, live } from "../../session";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Sheet } from "../../ui/Sheet";

const TASK_MAX = 2000;

/** Each sign-in word has a shape as well as a color (MOBUX rules). */
const signMarks: Record<AILaneAgent["state"], string> = { ready: "●", signin: "◌", unknown: "○" };

export function LaneStart({ onDone }: { onDone: () => void }) {
  const [list, setList] = useState<AILaneAgents | null>(null);
  const [err, setErr] = useState("");
  const [agent, setAgent] = useState("");
  const [task, setTask] = useState("");
  const [from, setFrom] = useState("");
  const [asking, setAsking] = useState(false);
  useEffect(() => {
    let stop = false;
    link.request("ai.lane_agents").then(
      (r) => {
        if (stop) return;
        const l = r as AILaneAgents;
        setList(l);
        const first = l.items.find((a) => a.state === "ready") ?? l.items[0];
        if (first) setAgent(first.id);
      },
      (e: unknown) => !stop && setErr(hubSays(e)),
    );
    return () => {
      stop = true;
    };
  }, []);
  const picked = list?.items.find((a) => a.id === agent);
  const text = task.trim();
  const ok = !!picked && text !== "" && text.length <= TASK_MAX && live.value;

  if (asking && picked) {
    return (
      <AskLaptop
        label={t("lane.start")}
        text={t(from.trim() ? "lane.ask_from" : "lane.ask", { agent: picked.title, task: text, from: from.trim() })}
        send={() => link.request("ai.lane_start", from.trim() ? { agent: picked.id, task: text, from: from.trim() } : { agent: picked.id, task: text })}
        onDone={onDone}
      />
    );
  }
  return (
    <Sheet onLater={onDone} label={t("lane.start")}>
      <div class="ask lane-start">
        <p class="ask-text">{t("lane.what")}</p>
        {err ? (
          <p class="empty refused">{err}</p>
        ) : !list ? (
          <p class="empty">{t("lane.loading")}</p>
        ) : list.noRepo ? (
          <p class="empty">{t("lane.no_repo")}</p>
        ) : list.items.length === 0 ? (
          <p class="empty">{t("lane.no_agents")}</p>
        ) : (
          <>
            <fieldset class="lane-agents">
              <legend>{t("lane.agent")}</legend>
              {list.items.map((a) => (
                <label key={a.id} class="row lane-agent" data-id={a.id}>
                  <input type="radio" name="lane-agent" value={a.id} checked={a.id === agent} onChange={() => setAgent(a.id)} />
                  <span>{a.title}</span>
                  <span class={`lane-sign st-${a.state}`}>
                    <span aria-hidden="true">{signMarks[a.state]} </span>
                    {t(`lane.sign.${a.state}`)}
                  </span>
                </label>
              ))}
            </fieldset>
            {picked?.state === "signin" && <p class="view-note">{t("lane.sign_note")}</p>}
            <label class="lane-field">
              <span>{t("lane.task")}</span>
              <textarea rows={4} maxLength={TASK_MAX} dir="auto" value={task} onInput={(e) => setTask((e.target as HTMLTextAreaElement).value)} />
            </label>
            <label class="lane-field">
              <span>{t("lane.from")}</span>
              <input type="text" dir="ltr" autocapitalize="off" spellcheck={false} placeholder={t("lane.from_hint")} value={from} onInput={(e) => setFrom((e.target as HTMLInputElement).value)} />
            </label>
          </>
        )}
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={onDone}>
            {t("cancel")}
          </button>
          <button type="button" class="btn" aria-disabled={ok ? undefined : "true"} onClick={() => ok && setAsking(true)}>
            {live.value ? t("lane.next") : t("offline.input")}
          </button>
        </div>
      </div>
    </Sheet>
  );
}
