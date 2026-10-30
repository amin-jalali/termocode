// The tasks and runs topics as the More pages read them (web design
// 2.8). Pure: no DOM, timers or network.

export interface TaskLast {
  state: "running" | "ok" | "failed";
  code: number;
  when: number;
  buf: number;
}

export interface TaskView {
  label: string;
  group: string;
  source: string;
  detail: string;
  last: TaskLast | null;
}

export interface RunView {
  key: string;
  title: string;
  chat: string;
  profile: string;
  state: string;
  why: string;
  step: number;
  steps: number;
  stepTitle: string;
  elapsedMs: number;
  live: boolean;
  cost: number;
  needsYou: boolean;
  can: string[];
}

type Obj = Record<string, unknown>;
const obj = (v: unknown): Obj => (typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : {});
const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);
const str = (v: unknown): string => (typeof v === "string" ? v : "");
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);

const LAST: ReadonlySet<string> = new Set(["running", "ok", "failed"]);

/** readTasks reads the tasks topic. */
export function readTasks(data: unknown): { tasks: TaskView[]; error: string } {
  const d = obj(data);
  return {
    error: str(d.error),
    tasks: list(d.tasks)
      .map(obj)
      .filter((x) => str(x.label) !== "")
      .map((x) => {
        const l = obj(x.last);
        return {
          label: str(x.label),
          group: str(x.group),
          source: str(x.source),
          detail: str(x.detail),
          last: LAST.has(str(l.state)) ? { state: str(l.state) as TaskLast["state"], code: num(l.code), when: num(l.when), buf: num(l.buf) } : null,
        };
      }),
  };
}

/** readRuns reads the runs topic. */
export function readRuns(data: unknown): RunView[] {
  return list(obj(data).runs)
    .map(obj)
    .filter((r) => str(r.key) !== "")
    .map((r) => ({
      key: str(r.key),
      title: str(r.title),
      chat: str(r.chat),
      profile: str(r.profile),
      state: str(r.state),
      why: str(r.why),
      step: num(r.step),
      steps: num(r.steps),
      stepTitle: str(r.step_title),
      elapsedMs: num(r.elapsed_ms),
      live: r.live === true,
      cost: num(r.cost_usd),
      needsYou: r.needs_you === true,
      can: list(r.can).map(str),
    }));
}

/** clock writes a duration as "41 s", "3 m 05 s" or "1 h 02 m". */
export function clock(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s} s`;
  if (s < 3600) return `${Math.floor(s / 60)} m ${String(s % 60).padStart(2, "0")} s`;
  return `${Math.floor(s / 3600)} h ${String(Math.floor(s / 60) % 60).padStart(2, "0")} m`;
}
