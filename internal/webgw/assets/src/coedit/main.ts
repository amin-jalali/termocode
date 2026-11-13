// Live editing together (coedit.js, G-COL): the editor's sync with the
// laptop's buffer, the merge of both sides' typing and the other
// people's carets. The page loads it with the editor (lib/coedit.ts) and
// gives it what it needs (init); it imports no value of the page.
export { setHost as init } from "./host";
export { LiveSync } from "./sync";
export { caret, peersOf } from "./peers";
