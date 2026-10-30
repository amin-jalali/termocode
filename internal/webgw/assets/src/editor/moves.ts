// The keys that move to the ends of a line and of the text. CodeMirror's
// own key list is not bundled, so without these the browser decides, and
// on a Mac Home/End only scroll and a Cmd+arrow sent to the page may do
// nothing. Shift with any of them selects up to there.
import { EditorSelection, type SelectionRange } from "@codemirror/state";
import { type EditorView, type KeyBinding } from "@codemirror/view";

type To = (v: EditorView, r: SelectionRange) => number;

const lineStart: To = (v, r) => v.moveToLineBoundary(r, false).head;
const lineEnd: To = (v, r) => v.moveToLineBoundary(r, true).head;
const docStart: To = () => 0;
const docEnd: To = (v) => v.state.doc.length;

const move =
  (to: To, select: boolean) =>
  (v: EditorView): boolean => {
    const ranges = v.state.selection.ranges.map((r) => (select ? EditorSelection.range(r.anchor, to(v, r)) : EditorSelection.cursor(to(v, r))));
    v.dispatch({ selection: EditorSelection.create(ranges, v.state.selection.mainIndex), scrollIntoView: true, userEvent: "select" });
    return true;
  };

const bind = (key: { key?: string; mac?: string }, to: To): KeyBinding => ({ ...key, run: move(to, false), shift: move(to, true), preventDefault: true });

/** moveKeys: Home/End and Mod-Home/Mod-End everywhere, Cmd+arrows on a Mac. */
export const moveKeys: readonly KeyBinding[] = [
  bind({ key: "Home" }, lineStart),
  bind({ key: "End" }, lineEnd),
  bind({ key: "Mod-Home" }, docStart),
  bind({ key: "Mod-End" }, docEnd),
  bind({ mac: "Cmd-ArrowLeft" }, lineStart),
  bind({ mac: "Cmd-ArrowRight" }, lineEnd),
  bind({ mac: "Cmd-ArrowUp" }, docStart),
  bind({ mac: "Cmd-ArrowDown" }, docEnd),
];
