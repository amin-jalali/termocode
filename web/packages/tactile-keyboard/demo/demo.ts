// The keyboard's preview harness: a code-ish text area driven only by the
// Tactile keyboard (the system keyboard stays closed). Build and open:
//   go run github.com/evanw/esbuild/cmd/esbuild web/packages/tactile-keyboard/demo/demo.ts \
//     --bundle --outfile=web/packages/tactile-keyboard/demo/demo.js
// then open demo/index.html.
import { autoPair, editorSuggestions, mountDock, newlineIndent, padStepper, threeFingerToggle, type KeyEvent } from "../src/index";

const ta = document.getElementById("code") as HTMLTextAreaElement;
const log = document.getElementById("log") as HTMLElement;
const say = (m: string): void => {
  log.textContent = m;
};
const caret = (): number => ta.selectionStart;
const put = (s: string, back = 0): void => {
  ta.setRangeText(s, ta.selectionStart, ta.selectionEnd, "end");
  ta.selectionStart = ta.selectionEnd = ta.selectionEnd - back;
};
const move = (d: number): void => {
  ta.selectionStart = ta.selectionEnd = Math.max(0, Math.min(ta.value.length, caret() + d));
};

const dock = mountDock(document.getElementById("dock")!, {
  layout: (new URLSearchParams(location.search).get("layout") as "compact" | "full" | null) ?? "auto",
  onKey,
  onAI: () => say("✨ AI key"),
  onMedia: (m) => say(`media: ${m}`),
  onToggleSound: (on) => say(`sound ${on ? "on" : "off"}`),
  onPadMove: padStepper((s) => (s === "left" ? move(-1) : s === "right" ? move(1) : undefined)),
  onSystemKeyboard: () => {
    ta.inputMode = "text";
    dock.detach(ta);
    ta.blur();
    ta.focus();
  },
});

function refresh(): void {
  const items = editorSuggestions(ta.value, caret(), "go");
  if (!items.length) return dock.suggestions.clear();
  dock.suggestions.show(items, (it) => {
    const w = /\w*$/.exec(ta.value.slice(0, caret()))![0];
    ta.setRangeText("", caret() - w.length, caret(), "end");
    put(it.insertText, it.caretOffset === undefined ? 0 : it.insertText.length - it.caretOffset);
  });
}

function onKey(e: KeyEvent): void {
  if (e.key.length === 1 && !e.ctrl && !e.alt) {
    const p = autoPair(e.key, ta.value[caret()] ?? "");
    if (p?.skip) move(p.caretBackBy);
    else if (p) put(p.text, p.caretBackBy);
    else put(e.key);
  } else if (e.key === "Backspace") {
    if (ta.selectionStart === ta.selectionEnd && caret() > 0) ta.setRangeText("", caret() - 1, caret(), "end");
    else put("");
  } else if (e.key === "Delete") ta.setRangeText("", caret(), caret() + 1, "end");
  else if (e.key === "Enter") put("\n" + newlineIndent(ta.value.slice(0, caret())));
  else if (e.key === "Tab") put("  ");
  else if (e.key === "ArrowLeft") move(-1);
  else if (e.key === "ArrowRight") move(1);
  else say([e.ctrl && "Ctrl", e.alt && "Alt", e.shift && "Shift", e.key].filter(Boolean).join("+"));
  refresh();
}

ta.inputMode = "none";
ta.addEventListener("focus", () => {
  if (ta.inputMode === "none") dock.attach(ta);
});
ta.addEventListener("blur", () => {
  if (ta.inputMode !== "none") ta.inputMode = "none";
});
threeFingerToggle(document.body, dock);
visualViewport?.addEventListener("resize", () => dock.refit());
ta.focus();
dock.attach(ta);
