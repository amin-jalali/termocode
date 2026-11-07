// Compare models from the phone (MOB-63, mobile plan 2.4 and 3.9): a
// race of one agent on 2–4 different models. The rules here match the
// laptop's (internal/webproto/ops_ai_race_models.go); the laptop checks
// again, and nothing starts until its card says yes.
import type { AIRaceModelList, AIRaceModelsArgs } from "../../../../../webproto/proto";
import type { StringKey } from "../../strings";

export const MODELS_OPS = {
  list: "ai.race_model_list",
  start: "ai.race_models",
} as const;

export const MODELS_TASK_MAX = 2000;
export const MODELS_VERIFY_MAX = 300;
export const MODELS_ID_MAX = 128;
export const MODELS_MIN = 2;
export const MODELS_MAX = 4;

/** modelOK: a model id that is safe after --model (no space, no shell
 * character, no leading dash). */
export function modelOK(id: string): boolean {
  return id.length > 0 && id.length <= MODELS_ID_MAX && /^[A-Za-z0-9][A-Za-z0-9._:/@+-]*$/.test(id);
}

/** toggle adds or removes a model from the picks (never more than 4). */
export function toggle(picks: readonly string[], id: string): string[] {
  if (picks.includes(id)) return picks.filter((p) => p !== id);
  return picks.length >= MODELS_MAX ? [...picks] : [...picks, id];
}

/** compareArgs checks the form as the laptop will and gives the op's
 * args, or the string key of what is wrong. */
export function compareArgs(task: string, agent: string, models: readonly string[], verify: string): AIRaceModelsArgs | StringKey {
  const tk = task.split(/\s+/).filter(Boolean).join(" ");
  if (!tk || tk.length > MODELS_TASK_MAX) return "models.need_task";
  if (!agent) return "models.need_agent";
  const picked = models.map((m) => m.trim()).filter(Boolean);
  if (picked.length < MODELS_MIN || picked.length > MODELS_MAX) return "models.need_models";
  if (picked.some((m) => !modelOK(m)) || new Set(picked).size !== picked.length) return "models.bad_model";
  const v = verify.trim();
  if (/[\u0000-\u001f\u007f]/.test(v) || v.length > MODELS_VERIFY_MAX) return "models.one_line";
  return v ? { task: tk, agent, models: picked, verify: v } : { task: tk, agent, models: picked };
}

export interface Requester {
  request(op: string, args?: unknown): Promise<unknown>;
}

export function modelList(link: Requester): Promise<AIRaceModelList> {
  return link.request(MODELS_OPS.list) as Promise<AIRaceModelList>;
}

/** compare asks the laptop to start the race; it answers with its card. */
export function compare(link: Requester, args: AIRaceModelsArgs): Promise<unknown> {
  return link.request(MODELS_OPS.start, args);
}
