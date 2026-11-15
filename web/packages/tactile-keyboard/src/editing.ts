// Small helpers a host editor uses with the keyboard.
import { currentWord, suggestLocal, type Suggestion } from "./completion";
import { expandSnippet } from "./smarts";

/** editorSuggestions: the strip for the caret: the keyword snippet chip
 * first ("for…"), then buffer words. Merge language-server items later
 * with suggest() if the host has them. */
export function editorSuggestions(text: string, offset: number, languageId: string | null | undefined): Suggestion[] {
  const word = currentWord(text, offset);
  const snip = word ? expandSnippet(word, languageId) : null;
  const out: Suggestion[] = [];
  if (snip) out.push({ label: `${word}…`, insertText: snip.text, detail: "snippet", caretOffset: snip.caretOffset });
  return out.concat(suggestLocal(text, offset));
}

export type Step = "left" | "right" | "up" | "down";

/** padStepper turns spacebar-trackpad motion into caret steps: one per
 * 9px across, one per 22px down, keeping the remainder. */
export function padStepper(onStep: (s: Step) => void, charW = 9, lineH = 22): (dx: number, dy: number) => void {
  let ax = 0;
  let ay = 0;
  return (dx, dy) => {
    ax += dx;
    ay += dy;
    while (Math.abs(ax) >= charW) {
      const right = ax > 0;
      ax -= right ? charW : -charW;
      onStep(right ? "right" : "left");
    }
    while (Math.abs(ay) >= lineH) {
      const down = ay > 0;
      ay -= down ? lineH : -lineH;
      onStep(down ? "down" : "up");
    }
  };
}
