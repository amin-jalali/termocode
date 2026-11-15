// pterm.js: the phone Term page's AI parts (page-designs 01-term), built
// alone (cmd/webbuild ptermChunk). home.js loads it with the terminal
// (lib/pterm.ts), calls init with its host, then draws AIBar above the
// keys and LineMenu on a long press. Terminal lines go to the chat as a
// term reference (the shell's buffer and rows): the laptop reads them.
import type { ChatRef } from "../lib/tochat";
import type { Line } from "../state/term";
import type { PTermHost } from "./api";
import { H, h, setHost } from "./jsx";
import { s } from "./strings";

/** init takes the page's host, once, before the first draw. */
export function init(host: PTermHost): void {
  setHost(host);
}

const ASK_ABOUT = "Explain these terminal lines.";
const FIX = "Fix the failure in this terminal's last output.";
const EXPLAIN = "Explain the error in this terminal's last output.";

/** termRef is rows from..to (from 1) of a shell, for the chat. */
const termRef = (buf: number, from: number, to: number): ChatRef => ({ kind: "term", id: String(buf), from, to: Math.min(to, from + 1999) });

/** ask sends a request about a shell's rows to the laptop's AI; the
 * answer comes in Chat. */
function ask(text: string, ref: ChatRef, label: string): void {
  const a = H.attach(ref, label);
  H.request("chat.send", { text, attach: a ? [a] : [] }).then(
    () => H.toast("ok", s("sent")),
    (e: unknown) => H.toast("warn", H.errText(e)),
  );
}

/** termChips are the suggestions the last output brings: a failure
 * brings Fix it and Explain this, a failed Go test Run <it>. */
export function termChips(rows: readonly string[]): { fix: boolean; test: string } {
  const tail = rows.slice(-40);
  const fix = tail.some((r) => /\bFAIL\b|\berror\b|\bpanic:|command not found|exit status [1-9]/i.test(r));
  let test = "";
  for (const r of tail) test = /--- FAIL: (\w+)/.exec(r)?.[1] ?? test;
  return { fix, test };
}

/** AIBar is the chips (none: no row) and the "Ask AI to write a command"
 * field above the keys: the AI types the command, never runs it. */
export function AIBar({ buf, label, rows, first }: { buf: number; label: string; rows: readonly string[]; first: number }) {
  const [text, setText] = H.useState("");
  const { fix, test } = termChips(rows);
  const tail = termRef(buf, Math.max(first + 1, first + rows.length - 199), first + rows.length);
  const { Chip } = H;
  return (
    <div class="pterm-ai">
      {(fix || test) && (
        <div class="pterm-chips" role="toolbar" aria-label={s("ask")}>
          {fix && (
            <Chip tone="ai" onClick={() => ask(FIX, tail, label)}>
              {s("fix")}
            </Chip>
          )}
          {fix && <Chip onClick={() => ask(EXPLAIN, tail, label)}>{s("explain")}</Chip>}
          {test && <Chip onClick={() => H.type(`go test -run '^${test}$' ./...\r`)}>{s("run", { name: test })}</Chip>}
        </div>
      )}
      <form
        class="pterm-ask"
        onSubmit={(e: Event) => {
          e.preventDefault();
          const v = text.trim();
          if (!v) return;
          ask(`Write one shell command for terminal ${label} and type it there without running it: ${v}`, tail, label);
          setText("");
        }}
      >
        <span class="pterm-ask-ic" aria-hidden="true">
          ✦
        </span>
        <input type="text" enterkeyhint="send" autocomplete="off" placeholder={s("ask")} aria-label={s("ask")} value={text} onInput={(e: Event) => setText((e.currentTarget as HTMLInputElement).value)} />
      </form>
    </div>
  );
}

/** LineMenu is a long press on lines (the selection, else the line):
 * Ask AI about them, add them to the chat, copy them, run a prompt line
 * again; then the page's own: Copy all, Paste, the line's links. */
export function LineMenu(p: { buf: number; label: string; lines: Line[]; end: number; at: [number, number]; canType: boolean; copyAll: () => void; paste: () => void; onClose: () => void }) {
  const [a, b] = p.at;
  const picked = p.lines.slice(a, b + 1);
  const text = picked.map((l) => l.text).join("\n");
  const n = picked.length;
  const title = s(n === 1 ? "line" : "lines", { n, name: p.label });
  const ref = termRef(p.buf, (p.lines[a]?.row ?? 0) + 1, p.lines[b + 1]?.row ?? p.end);
  const cmd = n === 1 ? /[$#%❯›]\s+(\S.*)$/.exec(text)?.[1].trim() : undefined;
  const add = () => H.addRef(ref, title) !== "bad";
  const { RowMenu } = H;
  const w = H.words();
  return (
    <RowMenu
      title={title}
      items={[
        { label: s("ask_about"), pick: () => add() && H.askChat(ASK_ABOUT) },
        { label: s("to_chat"), pick: () => add() && H.toast("ok", s("added")) },
        { label: s(n === 1 ? "copy1" : "copy", { n }), pick: () => H.copy(text) },
        ...(cmd && p.canType ? [{ label: w.again, pick: () => H.type(cmd + "\r") }] : []),
        { label: w.copyAll, pick: p.copyAll },
        ...(p.canType ? [{ label: w.paste, pick: p.paste }] : []),
        ...H.links(text),
      ]}
      onClose={p.onClose}
    />
  );
}
