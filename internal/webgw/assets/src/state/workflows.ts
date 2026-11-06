// The workflows topic as the desktop views read it (WFX, plan §5.7).
// Pure: no signals, no DOM.
import type { Card, WorkflowItem, WorkflowRun, WorkflowsSnap } from "../../../../webproto/proto";

/** readWorkflows is the topic's data, with empty lists for a missing one. */
export function readWorkflows(data: unknown): { workflows: WorkflowItem[]; runs: WorkflowRun[] } {
  const d = (typeof data === "object" && data !== null ? data : {}) as Partial<WorkflowsSnap>;
  return { workflows: Array.isArray(d.workflows) ? d.workflows : [], runs: Array.isArray(d.runs) ? d.runs : [] };
}

/** workflowsBadge counts the runs that wait for an answer and the ones
 * that work. */
export function workflowsBadge(data: unknown): { waiting: number; running: number } {
  const out = { waiting: 0, running: 0 };
  for (const r of readWorkflows(data).runs) {
    if (r.waiting) out.waiting++;
    else if (r.state === "running") out.running++;
  }
  return out;
}

/** runCards are the waiting write cards of run r. One run of a workflow
 * works at a time, so the workflow's name in the title is enough. */
export function runCards(cards: readonly Card[], r: WorkflowRun): Card[] {
  return cards.filter((c) => c.kind === "workflow" && c.title === r.workflow + " wants to write");
}
