// Start a race from the phone (MOB-62, mobile plan 2.6 and 2.9): the
// task (filled from the task or chat it came from), 2–4 lanes, each an
// agent the laptop can race, and an optional verify command. "Start a
// race" first checks the form as the laptop will, then asks the laptop
// (AskLaptop: Cancel first, armed after 600 ms); nothing starts until
// the laptop says yes. The laptop checks again that each agent is
// installed and says why when one is not. 44 px targets.
import { h } from "preact";
import { useState } from "preact/hooks";
import { link, live } from "../../session";
import { t } from "../../strings";
import { rs } from "./strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { RACE_MAX_LANES, RACE_MIN_LANES, RACE_TASK_MAX, RACE_VERIFY_MAX, start, startArgs, type StartArgs } from "./raceops";

export interface AgentChoice {
  id: string;
  title: string;
}

interface Props {
  /** agents are the ones a lane may run (the laptop has the last word). */
  agents: readonly AgentChoice[];
  task?: string;
  onDone(): void;
  onStarted?(): void;
}

export function RaceStart({ agents, task = "", onDone, onStarted }: Props) {
  const first = agents[0]?.id ?? "";
  const second = agents[1]?.id ?? first;
  const [text, setText] = useState(task);
  const [lanes, setLanes] = useState<string[]>([first, second]);
  const [check, setCheck] = useState("");
  const [why, setWhy] = useState("");
  const [ask, setAsk] = useState<StartArgs | null>(null);
  const canAct = live.value && link.meta.value?.role === "controller";

  const setLane = (i: number, id: string) => setLanes(lanes.map((v, k) => (k === i ? id : v)));
  const submit = (e: Event) => {
    e.preventDefault();
    const a = startArgs(text, lanes, check);
    if (typeof a === "string") {
      setWhy(rs(a));
      return;
    }
    setWhy("");
    setAsk(a);
  };

  return (
    <form class="race-start" onSubmit={submit}>
      <label class="race-field">
        <span>{rs("race.task")}</span>
        <textarea rows={3} maxLength={RACE_TASK_MAX} value={text} onInput={(e) => setText((e.target as HTMLTextAreaElement).value)} />
      </label>
      <fieldset class="race-field">
        <legend>{rs("race.lanes")}</legend>
        {lanes.map((id, i) => (
          <div class="race-lane" key={i}>
            <select aria-label={rs("race.lane_n", { n: i + 1 })} value={id} onChange={(e) => setLane(i, (e.target as HTMLSelectElement).value)}>
              {agents.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.title}
                </option>
              ))}
            </select>
            {lanes.length > RACE_MIN_LANES && (
              <button
                type="button"
                class="btn btn-neutral race-btn"
                aria-label={rs("race.remove_lane", { n: i + 1 })}
                onClick={() => setLanes(lanes.filter((_, k) => k !== i))}
              >
                <span aria-hidden="true">✕</span>
              </button>
            )}
          </div>
        ))}
        {lanes.length < RACE_MAX_LANES && (
          <button type="button" class="btn btn-neutral race-btn" onClick={() => setLanes([...lanes, first])}>
            {rs("race.add_lane")}
          </button>
        )}
      </fieldset>
      <label class="race-field">
        <span>{rs("race.verify_cmd")}</span>
        <input type="text" maxLength={RACE_VERIFY_MAX} spellcheck={false} autocapitalize="off" value={check} onInput={(e) => setCheck((e.target as HTMLInputElement).value)} />
      </label>
      {why && (
        <p class="race-note" role="alert">
          {why}
        </p>
      )}
      <div class="race-row">
        <button type="button" class="btn btn-neutral race-btn" onClick={onDone}>
          {t("cancel")}
        </button>
        <button type="submit" class="btn btn-primary race-btn" disabled={!canAct || agents.length === 0}>
          {rs("race.start")}
        </button>
      </div>
      {ask && (
        <AskLaptop
          label={rs("race.start")}
          text={rs("race.start_body", { n: ask.agents.length })}
          send={() => start(link, ask)}
          onDone={() => {
            setAsk(null);
            onDone();
          }}
          onConfirmed={onStarted}
        />
      )}
    </form>
  );
}
