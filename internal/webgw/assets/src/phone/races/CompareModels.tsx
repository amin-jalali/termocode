// Compare models from the phone (MOB-63, mobile plan 2.4 "Compare
// models"): the task (filled from the task or chat it came from), one
// agent whose CLI takes a model, 2–4 models picked from its list (or
// typed), and an optional verify command. "Compare models" checks the
// form as the laptop will, then asks the laptop (AskLaptop: Cancel first,
// armed after 600 ms); nothing starts until the laptop says yes. The
// lanes then show in the races view like any race. Offline or as a
// viewer the start button is off. 44 px targets; a picked model shows a
// check mark as well as its color.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { AIRaceModelAgent, AIRaceModelsArgs } from "../../../../../webproto/proto";
import { hubSays } from "../../lib/ai";
import { link, live } from "../../session";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { MODELS_ID_MAX, MODELS_MAX, MODELS_TASK_MAX, MODELS_VERIFY_MAX, compare, compareArgs, modelList, modelOK, toggle } from "./models";

interface Props {
  task?: string;
  onDone(): void;
  onStarted?(): void;
}

export function CompareModels({ task = "", onDone, onStarted }: Props) {
  const [list, setList] = useState<AIRaceModelAgent[] | null>(null);
  const [err, setErr] = useState("");
  const [text, setText] = useState(task);
  const [agent, setAgent] = useState("");
  const [picks, setPicks] = useState<string[]>([]);
  const [other, setOther] = useState("");
  const [check, setCheck] = useState("");
  const [why, setWhy] = useState("");
  const [ask, setAsk] = useState<AIRaceModelsArgs | null>(null);
  const on = live.value;
  const canAct = on && link.meta.value?.role === "controller";

  useEffect(() => {
    if (!on) return;
    let stop = false;
    modelList(link).then(
      (r) => {
        if (stop) return;
        setErr("");
        setList(r.agents);
        const first = r.agents.find((a) => a.installed);
        if (first) setAgent((cur) => cur || first.id);
      },
      (e: unknown) => !stop && setErr(hubSays(e)),
    );
    return () => {
      stop = true;
    };
  }, [on]);

  const cur = list?.find((a) => a.id === agent);
  const pickAgent = (id: string) => {
    setAgent(id);
    setPicks([]);
  };
  const addOther = () => {
    const id = other.trim();
    if (!modelOK(id)) {
      setWhy(t("models.bad_model"));
      return;
    }
    setWhy("");
    if (!picks.includes(id)) setPicks(toggle(picks, id));
    setOther("");
  };
  const submit = (e: Event) => {
    e.preventDefault();
    const a = compareArgs(text, agent, picks, check);
    if (typeof a === "string") {
      setWhy(t(a));
      return;
    }
    setWhy("");
    setAsk(a);
  };

  if (err) return <p class="empty refused">{err}</p>;
  if (!list) return <p class="empty">{t("models.loading")}</p>;
  const known = new Set((cur?.models ?? []).map((m) => m.id));
  const typed = picks.filter((p) => !known.has(p));

  return (
    <form class="race-start race-models" onSubmit={submit}>
      <label class="race-field">
        <span>{t("models.task")}</span>
        <textarea rows={3} maxLength={MODELS_TASK_MAX} value={text} onInput={(e) => setText((e.target as HTMLTextAreaElement).value)} />
      </label>
      <fieldset class="race-field">
        <legend>{t("models.agent")}</legend>
        <div class="race-row" role="radiogroup" aria-label={t("models.agent")}>
          {list.map((a) => (
            <button
              type="button"
              key={a.id}
              role="radio"
              aria-checked={a.id === agent ? "true" : "false"}
              disabled={!a.installed}
              class={`btn btn-neutral race-btn${a.id === agent ? " is-on" : ""}`}
              title={a.installed ? a.title : t("models.not_installed")}
              onClick={() => pickAgent(a.id)}
            >
              <span aria-hidden="true">{a.id === agent ? "● " : "○ "}</span>
              {a.title}
            </button>
          ))}
        </div>
        {!list.some((a) => a.installed) && <p class="race-note">{t("models.none_installed")}</p>}
      </fieldset>
      {cur && (
        <fieldset class="race-field">
          <legend>{t("models.pick", { n: picks.length, max: MODELS_MAX })}</legend>
          <div class="race-chips">
            {[...cur.models, ...typed.map((id) => ({ id, title: "" }))].map((m) => {
              const on = picks.includes(m.id);
              return (
                <button
                  type="button"
                  key={m.id}
                  aria-pressed={on ? "true" : "false"}
                  disabled={!on && picks.length >= MODELS_MAX}
                  class={`chip race-btn${on ? " is-on" : ""}`}
                  onClick={() => setPicks(toggle(picks, m.id))}
                >
                  <span aria-hidden="true">{on ? "✓ " : "+ "}</span>
                  {m.title || m.id}
                </button>
              );
            })}
          </div>
          <div class="race-lane">
            <input
              type="text"
              aria-label={t("models.other")}
              placeholder={t("models.other")}
              maxLength={MODELS_ID_MAX}
              spellcheck={false}
              autocapitalize="off"
              value={other}
              onInput={(e) => setOther((e.target as HTMLInputElement).value)}
            />
            <button type="button" class="btn btn-neutral race-btn" disabled={!other.trim() || picks.length >= MODELS_MAX} onClick={addOther}>
              {t("models.add")}
            </button>
          </div>
        </fieldset>
      )}
      <label class="race-field">
        <span>{t("models.verify_cmd")}</span>
        <input type="text" maxLength={MODELS_VERIFY_MAX} spellcheck={false} autocapitalize="off" value={check} onInput={(e) => setCheck((e.target as HTMLInputElement).value)} />
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
        <button type="submit" class="btn btn-primary race-btn" disabled={!canAct || !cur}>
          {t("models.start")}
        </button>
      </div>
      {ask && (
        <AskLaptop
          label={t("models.start")}
          text={t("models.start_body", { n: ask.models.length, agent: cur?.title ?? ask.agent })}
          send={() => compare(link, ask)}
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
