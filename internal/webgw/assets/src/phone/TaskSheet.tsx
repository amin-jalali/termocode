// TaskSheet (MOBUX-8, mobile plan 2.4): what to do with one task of a
// project. Talk about it · Run a workflow · Many agents · Many models ·
// Just start; Cancel is the safe choice and has focus. Each choice that
// starts work shows its rough cost and lane count first (state/
// taskactions.ts: lanes × the median cost of the recent runs).
//
//   Talk about it   puts a reference to the task in the chat box (never
//                   a file's contents); nothing runs until Send.
//   Run a workflow  dry run (read-only, starts at once; its plan steps
//                   cost AI money, said first), run (laptop card), or
//                   ask the AI to suggest one (chat box).
//   Many agents     a race of 2–4 lanes; one installed agent per lane;
//                   the laptop card confirms (AskLaptop).
//   Many models     2–4 trusted profiles; the request goes in the chat
//                   box and the AI asks the laptop (MOB-63 adds its op).
//   Just start      the AI starts on the task in the chat now.
//
// A viewer sees only Talk about it. Phone code: the phone's lazy chunk.
import { Fragment, h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { AISnap, RunsSnap, WorkflowItem, WorkflowsSnap } from "../../../../webproto/proto";
import { pendingDraft } from "../lib/drafts";
import { go } from "../lib/router";
import { toast } from "../lib/toasts";
import { link, store } from "../session";
import {
  HINTS,
  RACE_MAX_LANES,
  RACE_MIN_LANES,
  actionsFor,
  clampLanes,
  costLine,
  defaultLanes,
  estimate,
  resizeLanes,
  taskRef,
  toggleProfile,
  type TaskActionID,
  type TaskLike,
} from "../state/taskactions";
import { t, tn } from "../strings";
import { AskLaptop } from "../ui/AskLaptop";
import { gitFailed as failed } from "../views/git/errors";
import { PhoneSheet } from "./Sheet";

type Step =
  | { s: "menu" }
  | { s: "workflow" }
  | { s: "wfrun"; name: string }
  | { s: "agents" }
  | { s: "race"; agents: string[] }
  | { s: "models" }
  | { s: "start" };

const refWords = {
  issue: (n: number) => t("ph.task.ref_issue", { n }),
  pr: (n: number) => t("ph.task.ref_pr", { n }),
  mr: (n: number) => t("ph.task.ref_mr", { n }),
};

// Built on each draw, so the words follow the current strings.
const costWords = () => ({
  free: t("ph.task.cost_free"),
  lanes: (n: number) => tn("ph.task.lanes", n),
  known: (usd: string, lanes: string) => t("ph.task.cost", { usd, lanes }),
  unknown: (lanes: string) => t("ph.task.cost_unknown", { lanes }),
});

/** toChat puts text in the chat box (nothing is sent) and opens Chat. */
function toChat(text: string): void {
  pendingDraft.value = { text, notChat: "" };
  go({ tab: "chat", rest: "", query: {} });
}

function Cost({ action, lanes }: { action: TaskActionID; lanes: number }) {
  const runs = (store.topic("runs").value?.data as RunsSnap | undefined)?.runs ?? [];
  return <p class="ph-task-cost">{costLine(estimate(action, lanes, runs), costWords())}</p>;
}

const startable = (w: WorkflowItem) => w.state === "on" || w.state === "off";

function WorkflowStep({ ref_, onRun }: { ref_: string; onRun(name: string): void }) {
  const d = store.topic("workflows").value?.data as WorkflowsSnap | undefined;
  const list = (d?.workflows ?? []).filter(startable);
  const dry = (name: string) =>
    link.request("workflows.dry_run", { name }).then(() => toast("ok", t("wf.dry_started", { s: name })), failed);
  return (
    <>
      <p class="ph-task-hint">{t("wf.dry_note")}</p>
      <Cost action="workflow" lanes={1} />
      {!d ? (
        <p class="ph-task-hint">{t("quick.loading")}</p>
      ) : list.length === 0 ? (
        <p class="ph-task-hint">{t("wf.none")}</p>
      ) : (
        <ul class="ph-task-wfs">
          {list.map((w) => (
            <li key={w.name} class="ph-task-wf" data-key={w.name}>
              <bdi class="ph-task-wf-name" dir="ltr">
                {w.name}
              </bdi>
              <button type="button" class="ph-pick" onClick={() => dry(w.name)}>
                {t("wf.dry_run")}
              </button>
              <button type="button" class="ph-pick" onClick={() => onRun(w.name)}>
                {t("tasks.run")}
              </button>
            </li>
          ))}
        </ul>
      )}
      <button type="button" class="ph-pick" onClick={() => toChat(t("ph.task.draft_suggest", { task: ref_ }))}>
        {t("ph.task.suggest")}
      </button>
    </>
  );
}

function AgentsStep({ picked, setPicked }: { picked: string[]; setPicked(p: string[]): void }) {
  const ai = store.topic("ai").value?.data as AISnap | undefined;
  const agents = ai?.raceAgents ?? [];
  const ids = agents.map((a) => a.id);
  const n = picked.length;
  if (agents.length === 0) return <p class="ph-task-hint">{ai ? t("ph.task.no_agents") : t("quick.loading")}</p>;
  const size = (k: number) => setPicked(resizeLanes(picked, ids, clampLanes(k)));
  return (
    <>
      <div class="ph-lanes" role="group" aria-label={t("ph.race.lanes")}>
        <button type="button" class="ph-lane-step" aria-label={t("ph.task.lanes_fewer")} aria-disabled={n <= RACE_MIN_LANES ? "true" : undefined} onClick={() => n > RACE_MIN_LANES && size(n - 1)}>
          −
        </button>
        <span class="ph-lanes-n" aria-live="polite">
          {tn("ph.task.lanes", n)}
        </span>
        <button type="button" class="ph-lane-step" aria-label={t("ph.task.lanes_more")} aria-disabled={n >= RACE_MAX_LANES ? "true" : undefined} onClick={() => n < RACE_MAX_LANES && size(n + 1)}>
          +
        </button>
      </div>
      {picked.map((id, i) => (
        <label key={i} class="ph-lane">
          <span class="ph-lane-label">{t("ph.task.lane_n", { n: i + 1 })}</span>
          <select
            class="ph-pick"
            value={id}
            onChange={(e) => {
              const next = [...picked];
              next[i] = (e.currentTarget as HTMLSelectElement).value;
              setPicked(next);
            }}
          >
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.title}
              </option>
            ))}
          </select>
        </label>
      ))}
      <Cost action="agents" lanes={n} />
    </>
  );
}

function ModelsStep({ picked, setPicked }: { picked: string[]; setPicked(p: string[]): void }) {
  const ai = store.topic("ai").value?.data as AISnap | undefined;
  const profiles = (ai?.profiles ?? []).filter((p) => p.trust === "use");
  if (!ai) return <p class="ph-task-hint">{t("quick.loading")}</p>;
  if (profiles.length < RACE_MIN_LANES) return <p class="ph-task-hint">{t("ph.task.no_profiles")}</p>;
  return (
    <>
      <p class="ph-task-hint">{t("ph.task.pick_profiles")}</p>
      <div class="ph-picks" role="group" aria-label={t("ph.task.pick_profiles")}>
        {profiles.map((p) => {
          const on = picked.includes(p.name);
          return (
            <button key={p.name} type="button" class={`ph-pick${on ? " is-on" : ""}`} aria-pressed={on ? "true" : "false"} title={p.hint} onClick={() => setPicked(toggleProfile(picked, p.name))}>
              <span aria-hidden="true">{on ? "✓ " : "○ "}</span>
              <bdi dir="auto">{p.name}</bdi>
            </button>
          );
        })}
      </div>
      <Cost action="models" lanes={Math.max(picked.length, RACE_MIN_LANES)} />
    </>
  );
}

export function TaskSheet({ task, onClose }: { task: TaskLike; onClose: () => void }) {
  const viewer = link.meta.value?.role === "viewer";
  const [step, setStep] = useState<Step>({ s: "menu" });
  const [lanes, setLanes] = useState<string[]>([]);
  const [profiles, setProfiles] = useState<string[]>([]);
  const ref_ = taskRef(task, refWords);
  const work = t("ph.task.draft_start", { task: ref_ });

  // The cost line reads the runs; the steps read the ai and workflows topics.
  useEffect(() => link.subscribe("runs"), []);
  useEffect(() => (viewer ? undefined : link.subscribe("ai")), [viewer]);
  useEffect(() => (viewer ? undefined : link.subscribe("workflows")), [viewer]);

  // The first lanes: one installed agent each, in the laptop's order.
  const ai = store.topic("ai").value?.data as AISnap | undefined;
  const agentIDs = (ai?.raceAgents ?? []).map((a) => a.id);
  useEffect(() => {
    if (lanes.length === 0 && agentIDs.length > 0) setLanes(defaultLanes(agentIDs, RACE_MIN_LANES));
  }, [agentIDs.join(",")]);

  const back = { label: t("back"), safe: true, onClick: () => setStep({ s: "menu" }) };

  const choose = (id: TaskActionID) => {
    if (id === "talk") {
      toChat(t("ph.task.draft_talk", { task: ref_ }));
      onClose();
      return;
    }
    setStep({ s: id });
  };

  const startNow = () => {
    link.request("chat.send", { text: work }).then(() => {
      toast("ok", t("ph.task.started"));
      go({ tab: "chat", rest: "", query: {} });
      onClose();
    }, failed);
  };

  if (step.s === "race") {
    const agents = step.agents;
    return (
      <AskLaptop
        label={t("ph.task.race")}
        text={t("ph.task.race_body", { lanes: tn("ph.task.lanes", agents.length), task: ref_ })}
        send={() => link.request("ai.race_start", { task: work, agents })}
        onDone={onClose}
      />
    );
  }
  if (step.s === "wfrun") {
    const name = step.name;
    return (
      <AskLaptop
        label={t("wf.run_label", { s: name })}
        text={t("wf.run_body", { s: name })}
        send={() => link.request("workflows.run", { name })}
        onDone={onClose}
      />
    );
  }

  if (step.s === "menu") {
    return (
      <PhoneSheet title={ref_} onLater={onClose} actions={[{ label: t("cancel"), safe: true, onClick: onClose }]}>
        <div class="ph-task-acts">
          {actionsFor(viewer).map((a) => (
            <button key={a.id} type="button" class="ph-sheet-act ph-task-act" data-act={a.id} onClick={() => choose(a.id)}>
              <span class="ph-task-act-label">{t(a.label)}</span>
              <span class="ph-task-act-hint">{t(HINTS[a.id])}</span>
            </button>
          ))}
        </div>
      </PhoneSheet>
    );
  }

  if (step.s === "workflow") {
    return (
      <PhoneSheet title={t("ph.task.workflow")} onLater={onClose} actions={[back]}>
        <WorkflowStep ref_={ref_} onRun={(name) => setStep({ s: "wfrun", name })} />
      </PhoneSheet>
    );
  }

  if (step.s === "agents") {
    return (
      <PhoneSheet
        title={t("ph.task.agents")}
        onLater={onClose}
        actions={[back, { label: t("ph.task.race"), primary: true, disabled: lanes.length < RACE_MIN_LANES, onClick: () => setStep({ s: "race", agents: lanes }) }]}
      >
        <AgentsStep picked={lanes} setPicked={setLanes} />
      </PhoneSheet>
    );
  }

  if (step.s === "models") {
    const ok = profiles.length >= RACE_MIN_LANES;
    return (
      <PhoneSheet
        title={t("ph.task.models")}
        onLater={onClose}
        actions={[
          back,
          {
            label: t("ph.task.compare"),
            primary: true,
            disabled: !ok,
            onClick: () => {
              toChat(t("ph.task.draft_models", { task: ref_, list: profiles.join(", ") }));
              onClose();
            },
          },
        ]}
      >
        <ModelsStep picked={profiles} setPicked={setProfiles} />
      </PhoneSheet>
    );
  }

  return (
    <PhoneSheet title={t("ph.task.start")} onLater={onClose} actions={[back, { label: t("ph.task.go"), primary: true, onClick: startNow }]}>
      <p class="ph-task-hint">{t("ph.task.start_hint")}</p>
      <Cost action="start" lanes={1} />
    </PhoneSheet>
  );
}
