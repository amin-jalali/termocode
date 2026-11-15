// The words of the phone Term page's AI parts that the page's strings do
// not have. They ship in pterm.js. Same voice as strings.ts. Pure.
const S = {
  ask: "Ask AI to write a command",
  fix: "✦ Fix it",
  explain: "Explain this",
  run: "Run {name}",
  sent: "Sent to the AI. The answer comes in Chat.",
  line: "{n} line · terminal {name}",
  lines: "{n} lines · terminal {name}",
  ask_about: "✦ Ask AI about this",
  to_chat: "Add to chat ↩",
  added: "Added to the chat.",
  copy1: "Copy {n} line",
  copy: "Copy {n} lines",
} as const;

export type PKey = keyof typeof S;

/** s is one of these strings with its {vars} filled in. */
export function s(key: PKey, vars?: Record<string, string | number>): string {
  let out: string = S[key];
  if (vars) for (const k of Object.keys(vars)) out = out.split(`{${k}}`).join(String(vars[k]));
  return out;
}
