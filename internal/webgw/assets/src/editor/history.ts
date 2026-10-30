// The editor's own undo (WEB-39): steps of the user's changes, kept as
// their inverses. The laptop's changes are no steps; every step is moved
// over them (each step's change rebased on the ones above it), so undo
// takes back the user's own typing and never the laptop's. Typing within
// GROUP_MS joins one step. Pure: CodeMirror's change sets only.
import { ChangeSet, type ChangeDesc, type Text } from "@codemirror/state";

const GROUP_MS = 600;
const MAX_STEPS = 200;

interface Step {
  /** inv takes the step back; it applies to the text of its place in the stack. */
  inv: ChangeSet;
  at: number;
  /** end is where the step's change ended (-1: closed, never joined). */
  end: number;
}

export class History {
  private done: Step[] = [];
  private undone: Step[] = [];

  /** user records a change the user made on doc before it, at time now. */
  user(changes: ChangeSet, before: Text, now: number, join = true): void {
    const inv = changes.invert(before);
    let from = -1, toA = -1, end = -1;
    changes.iterChangedRanges((fA, tA, _fB, tB) => {
      if (from < 0) from = fA;
      toA = tA;
      end = tB;
    });
    const top = this.done[this.done.length - 1];
    if (join && top && top.end >= 0 && now - top.at < GROUP_MS && (from === top.end || toA === top.end)) {
      top.inv = inv.compose(top.inv);
      top.at = now;
      top.end = end;
    } else this.done.push({ inv, at: now, end: join ? end : -1 });
    if (this.done.length > MAX_STEPS) this.done.shift();
    this.undone = [];
  }

  /** remote moves every step over a change the laptop made. */
  remote(changes: ChangeSet): void {
    const top = this.done[this.done.length - 1];
    if (top) top.end = -1;
    rebase(this.done, changes);
    rebase(this.undone, changes);
  }

  canUndo(): boolean {
    return this.done.length > 0;
  }

  canRedo(): boolean {
    return this.undone.length > 0;
  }

  /** undo returns the change that takes the newest step back (null: none);
   * its inverse becomes a redo step. */
  undo(doc: Text): ChangeSet | null {
    return move(this.done, this.undone, doc);
  }

  redo(doc: Text): ChangeSet | null {
    return move(this.undone, this.done, doc);
  }
}

function move(from: Step[], to: Step[], doc: Text): ChangeSet | null {
  const s = from.pop();
  if (!s) return null;
  to.push({ inv: s.inv.invert(doc), at: 0, end: -1 });
  return s.inv;
}

/** rebase moves a stack (top last) over a change of the text the top
 * applies to: each step over the change, the change over the step. */
function rebase(stack: Step[], changes: ChangeSet): void {
  let c: ChangeSet = changes;
  for (let i = stack.length - 1; i >= 0; i--) {
    const s = stack[i];
    const desc: ChangeDesc = s.inv.desc;
    s.inv = s.inv.map(c);
    c = c.map(desc);
  }
}
