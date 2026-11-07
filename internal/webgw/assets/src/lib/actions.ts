// Row and block actions (MOB-5, plan §2.4): what the action sheet offers
// for one item. Four kinds:
//   op    one termocode op; its args come from the item's Ref through the
//         one table below (never free text from the page)
//   ask   fills the composer with a short request + the item as a chip
//   ref   adds the item as a chip to the composer ("↩ to chat")
//   open  routes to an existing view of the page
// A viewer sees open and ref only (its link may not call ops or send).
// Pure: no DOM, timers or network.
import type { Attach, OpArgs, Role } from "../../../../webproto/proto";

export type ActionKind = "op" | "ask" | "ref" | "open";

/** Ref names the item a row or block stands for (the laptop resolves it). */
export interface Ref {
  kind: Attach["kind"];
  id?: string;
  path?: string;
  from?: number;
  to?: number;
  label?: string;
  spec?: string;
  ws?: string;
}

export interface Action {
  id: string;
  label: string;
  kind: ActionKind;
  /** op: the op name (a key of OP_ARGS). */
  op?: keyof typeof OP_ARGS;
  /** ask: the request put in the box ("Explain why this failed"). */
  ask?: string;
  /** open: the page route ("#/git"). */
  route?: string;
  /** op: it changes something; the sheet asks once more (Cancel first). */
  danger?: boolean;
}

type ArgsOf<K extends keyof OpArgs> = (r: Ref) => OpArgs[K] | null;

const label: ArgsOf<"tasks.run"> = (r) => (r.kind === "task" && r.id ? { label: r.id } : null);
const runKey: ArgsOf<"runs.stop"> = (r) => (r.kind === "run" && r.id ? { key: r.id } : null);
const filePath: ArgsOf<"buf.open"> = (r) => ((r.kind === "file" || r.kind === "lines") && r.path ? { path: r.path } : null);

/** OP_ARGS maps a Ref to an op's args: the only ops an action may call. */
export const OP_ARGS = {
  "tasks.run": label,
  "tasks.terminate": label,
  "tasks.tail": label,
  "runs.stop": runKey,
  "runs.pause": runKey,
  "runs.resume": runKey,
  "buf.open": filePath,
  "tree.reveal_on_laptop": filePath,
  "git.stage": ((r) => ((r.kind === "file" || r.kind === "diff") && r.path ? { paths: [r.path] } : null)) as ArgsOf<"git.stage">,
} satisfies { [K in keyof OpArgs]?: ArgsOf<K> };

/** opArgs is the args of action's op for ref (null: it does not fit). */
export function opArgs(a: Action, ref: Ref): unknown | null {
  if (a.kind !== "op" || !a.op) return null;
  const fn = OP_ARGS[a.op] as ((r: Ref) => unknown | null) | undefined;
  return fn ? fn(ref) : null;
}

/** refAttach is the reference chat.send carries for ref: never content. */
export function refAttach(ref: Ref): Attach {
  const out: Attach = { kind: ref.kind };
  if (ref.id) out.id = ref.id;
  if (ref.path) out.path = ref.path;
  if (ref.kind === "lines") {
    out.from = ref.from;
    out.to = ref.to;
  }
  if (ref.kind === "view" && ref.spec) out.spec = ref.spec;
  if (ref.label) out.label = ref.label.slice(0, 120);
  return out;
}

/** actionsFor is what role may use of actions, in order: an op that does
 * not fit ref is left out. Every item can go to the chat (ref). */
export function actionsFor(role: Role, ref: Ref, actions: readonly Action[]): Action[] {
  const out = actions.filter((a) => {
    if (a.kind === "op") return role === "controller" && opArgs(a, ref) !== null;
    if (a.kind === "ask") return role === "controller";
    return true;
  });
  if (!out.some((a) => a.kind === "ref")) out.push({ id: "ref", label: "Add to message", kind: "ref" });
  return out;
}

/** defaultActions are the actions of an item whose block gave none. */
export function defaultActions(ref: Ref): Action[] {
  const out: Action[] = [];
  switch (ref.kind) {
    case "task":
      out.push({ id: "explain", label: "Explain the last run", kind: "ask", ask: "Explain why this task failed and how to fix it." });
      out.push({ id: "run", label: "Run again", kind: "op", op: "tasks.run" });
      out.push({ id: "tail", label: "Show output", kind: "open", route: "#/more/tasks" });
      break;
    case "run":
      out.push({ id: "summary", label: "Summarise", kind: "ask", ask: "Summarise this run: what it did, what is left." });
      out.push({ id: "stop", label: "Stop", kind: "op", op: "runs.stop", danger: true });
      break;
    case "file":
    case "lines":
      out.push({ id: "explain", label: "Explain", kind: "ask", ask: "Explain this code." });
      out.push({ id: "open", label: "Open", kind: "op", op: "buf.open" });
      break;
    case "diff":
      out.push({ id: "review", label: "Review the change", kind: "ask", ask: "Review this change. List problems first." });
      break;
    default:
      out.push({ id: "explain", label: "Explain / summarise", kind: "ask", ask: "Explain this." });
  }
  out.push({ id: "ref", label: "Add to message", kind: "ref" });
  return out;
}
