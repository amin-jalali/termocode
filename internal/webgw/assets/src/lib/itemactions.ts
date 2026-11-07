// Lane, run and log actions of the phone's Home (MOB-17, plan 2.4): the
// action sheet of a lane (a worktree of the worktrees topic), an AI run
// (runs topic) and a task's output (tasks topic). Every action is one of
// four kinds: open (routes to an existing view), ref (adds a chip to the
// composer), ask (fills the box with a question and a reference) and op
// (one termocode op with its own role, rate and laptop-card rules).
// A viewer sees open and ref only. Safe ones come first; an op that
// stops, ends or removes something asks inside the sheet first (Cancel
// first and focused) or waits for a laptop card. Pure: no DOM, timers
// or network, so the tables stay easy to read and to check.
import type { WorktreeItem } from "../../../../webproto/proto";
import type { RunView, TaskView } from "../state/tasks";
import type { StringKey } from "../strings";

export type ActionKind = "open" | "ref" | "ask" | "op";

/** ItemRef names a row for the composer: an id the laptop knows, never
 * a path or a file's contents (the laptop resolves it, MOB-5). */
export interface ItemRef {
  kind: "worktree" | "run" | "task";
  id: string;
  title: string;
}

/** Input is the one inline field an op needs before it can be sent. */
export type ActionInput = "note" | "link";

export interface ItemAction {
  /** id is stable ("lane-pin"): tests and the kit name actions by it. */
  id: string;
  kind: ActionKind;
  label: StringKey;
  /** label vars, e.g. {label} of a task. */
  vars?: Record<string, string>;
  /** op and args of an op action. */
  op?: string;
  args?: Record<string, unknown>;
  /** confirm asks inside the sheet first (Cancel first and focused). */
  confirm?: StringKey;
  /** card: the op waits for a laptop card; the phone only asks. */
  card?: boolean;
  /** risky: drawn last, in danger style, armed after 600 ms. */
  risky?: boolean;
  /** input: the op needs one short field first (its args are filled then). */
  input?: ActionInput;
  /** route of an open action (a hash route of the page). */
  route?: { tab: "more" | "term"; rest: string };
  /** ask: the question put in the box, with ref attached. */
  ask?: StringKey;
}

export type Role = "viewer" | "controller" | string;

const canAct = (role: Role) => role !== "viewer";

/** order puts open, ref and ask first, then plain ops, then cards and
 * risky ones, keeping each group's own order. */
export function order(list: ItemAction[]): ItemAction[] {
  const rank = (a: ItemAction) => (a.kind === "open" ? 0 : a.kind === "ref" ? 1 : a.kind === "ask" ? 2 : a.risky || a.card ? 4 : 3);
  return list.map((a, i) => ({ a, i })).sort((x, y) => rank(x.a) - rank(y.a) || x.i - y.i).map((x) => x.a);
}

/** forRole drops what a viewer may not do (op and ask). */
function forRole(list: ItemAction[], role: Role): ItemAction[] {
  return order(canAct(role) ? list : list.filter((a) => a.kind === "open" || a.kind === "ref"));
}

export function laneRef(w: WorktreeItem): ItemRef {
  return { kind: "worktree", id: w.id, title: w.title };
}

/** laneActions is the sheet of a lane (worktree). The main checkout and
 * a gone or archived one get no ops that change it. */
export function laneActions(w: WorktreeItem, role: Role): ItemAction[] {
  const out: ItemAction[] = [
    { id: "lane-ref", kind: "ref", label: "item.add_ref" },
    { id: "lane-ask", kind: "ask", label: "item.ask_btn_lane", ask: "item.ask_lane" },
  ];
  const usable = !w.gone && w.group !== "archived";
  if (usable) {
    out.push({ id: "lane-pin", kind: "op", label: w.pinned ? "item.unpin" : "item.pin", op: "worktrees.pin", args: { id: w.id, on: !w.pinned } });
    out.push({ id: "lane-note", kind: "op", label: "item.note", op: "worktrees.note", input: "note", args: { id: w.id } });
    out.push({ id: "lane-link", kind: "op", label: "item.link", op: "worktrees.link", input: "link", args: { id: w.id } });
    if (w.link) out.push({ id: "lane-unlink", kind: "op", label: "item.unlink", op: "worktrees.link", args: { id: w.id, kind: "none", number: 0 } });
  }
  if (usable && w.kind !== "main") {
    if (w.state === "asleep") out.push({ id: "lane-wake", kind: "op", label: "item.wake", op: "worktrees.sleep", args: { id: w.id, on: false } });
    else out.push({ id: "lane-sleep", kind: "op", label: "item.sleep", op: "worktrees.sleep", args: { id: w.id, on: true }, confirm: "item.sleep_q", risky: true });
    if (!w.here) out.push({ id: "lane-archive", kind: "op", label: "item.archive", op: "worktrees.archive", args: { id: w.id }, card: true, risky: true });
  }
  return forRole(out, role);
}

export function runRef(r: RunView): ItemRef {
  return { kind: "run", id: r.key, title: r.title };
}

/** runActions is the sheet of an AI run: what its can[] allows now. */
export function runActions(r: RunView, role: Role): ItemAction[] {
  const waits = r.needsYou || r.state === "stalled" || r.state === "paused";
  const out: ItemAction[] = [
    { id: "run-open", kind: "open", label: "item.open_runs", route: { tab: "more", rest: "runs" } },
    { id: "run-ref", kind: "ref", label: "item.add_ref" },
    { id: "run-ask", kind: "ask", label: "item.ask_btn_run", ask: waits ? "item.ask_run_wait" : "item.ask_run" },
  ];
  if (r.can.includes("resume")) out.push({ id: "run-resume", kind: "op", label: "runs.resume", op: "runs.resume", args: { key: r.key } });
  if (r.can.includes("pause")) out.push({ id: "run-pause", kind: "op", label: "runs.pause", op: "runs.pause", args: { key: r.key } });
  if (r.can.includes("stop")) out.push({ id: "run-stop", kind: "op", label: "stop", op: "runs.stop", args: { key: r.key }, confirm: "runs.stop_confirm", risky: true });
  return forRole(out, role);
}

export function logRef(task: TaskView): ItemRef {
  return { kind: "task", id: task.label, title: task.label };
}

/** logActions is the sheet of a task's output (its log). */
export function logActions(task: TaskView, role: Role): ItemAction[] {
  const running = task.last?.state === "running";
  const failed = task.last?.state === "failed";
  const out: ItemAction[] = [];
  if (task.last) out.push({ id: "log-term", kind: "open", label: "tasks.open_term", route: { tab: "term", rest: String(task.last.buf) } });
  out.push({ id: "log-tasks", kind: "open", label: "item.open_tasks", route: { tab: "more", rest: "tasks" } });
  out.push({ id: "log-ref", kind: "ref", label: "item.add_ref" });
  if (task.last) out.push({ id: "log-ask", kind: "ask", label: "item.ask_btn_log", ask: failed ? "item.ask_log_failed" : "item.ask_log" });
  if (running) {
    out.push({ id: "log-stop", kind: "op", label: "stop", op: "tasks.terminate", args: { label: task.label }, confirm: "tasks.stop_confirm", risky: true });
  } else {
    out.push({ id: "log-run", kind: "op", label: task.last ? "item.run_again" : "tasks.run", op: "tasks.run", args: { label: task.label }, confirm: "tasks.confirm" });
  }
  return forRole(out, role);
}

/** MAX_LINK is the highest issue or PR number worktrees.link takes. */
export const MAX_LINK = 999_999_999;

/** linkArgs reads the link field ("412", "#412", "PR #12"): the args of
 * worktrees.link, or null when it is not a number in range. */
export function linkArgs(id: string, kind: "issue" | "pr", text: string): { id: string; kind: "issue" | "pr"; number: number } | null {
  const m = /^\s*(?:pr\s*)?#?\s*(\d{1,9})\s*$/i.exec(text);
  if (!m) return null;
  const n = Number(m[1]);
  return n >= 1 && n <= MAX_LINK ? { id, kind, number: n } : null;
}
