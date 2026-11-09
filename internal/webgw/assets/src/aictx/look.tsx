// The desktop chat's look (owner, 2026-11-09): an AI answer reads as a
// chat answer. A one-step profile's reply has no run or step card: under
// it one small muted line with its model and time ("gpt-6-sol · 54s"),
// the tokens, step and CLI in its tooltip. Each tool call is one short
// line ("✓ db_schema baaytona-db-1", ✗ when it failed) that opens on a
// click to show the call and its result. While the answer is written it
// says Thinking… with its time and a Stop. A run of several steps shows
// a strip of its steps above the answer, and its Run view on Show
// (runview.tsx). The rules file the turns used shows once, at the top
// of the chat (desk CSS hides its chip on each message). The page's Chat.tsx asks here
// through lib/toolblocks.ts look. The phone draws the same parts from
// home.js (phone/chatlook.tsx gives them the page's host, phone set).
import type { ChatMsg, ChatWait } from "../../../../webproto/proto";
import type { ChatLook } from "../lib/toolblocks";
import type { VNode } from "preact";
import { readChat } from "../state/chat";
import { Fragment, H, h } from "./jsx";
import { Picker } from "./picker";
import { Strip } from "./runview";

/** dur writes a time like the chat's run line: "12s", "2m 05s", "1h 03m". */
export function dur(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`;
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m`;
}

/** FAILED: a tool result that says the call failed. */
const FAILED = /^(error|failed|denied|refused|blocked)\b|exit (status |code )?[1-9]/i;

/** Tool is one tool call as a short line: its mark (✓, ✗, … while it
 * runs), the call, and on a click the whole call and its result. A line
 * that is no call (a note, a chip) shows as it is. */
export function Tool({ line, done }: { line: string; done: boolean }) {
  const [open, setOpen] = H.useState(false);
  const call = line.startsWith("⚙") ? line.slice(1).trim() : "";
  if (!call) return <div class="tool-row tool-note" dir="auto">{line}</div>;
  const at = call.indexOf(" → ");
  const what = at < 0 ? call : call.slice(0, at);
  const res = at < 0 ? "" : call.slice(at + 3);
  const bad = FAILED.test(res);
  const mark = res ? (bad ? "✗" : "✓") : done ? "•" : "…";
  return (
    <div class={`tool-row${open ? " is-open" : ""}${bad ? " is-bad" : ""}`} dir="ltr">
      <button type="button" class="tool-sum" aria-expanded={open ? "true" : "false"} title={what} onClick={() => setOpen(!open)}>
        {mark} {what}
      </button>
      {open && (
        <div class="tool-more">
          {what.length > 48 && <div class="tool-call">{what}</div>}
          {res && <div class="tool-res">{res}</div>}
        </div>
      )}
    </div>
  );
}

/** Wait is what the message's tool waits for from the user ("Waiting
 * for you: Database password for BaaytonaDB"), and Answer when this
 * page has that question: it opens it again (its sheet put off). */
function Wait({ w }: { w: ChatWait }) {
  const a = H.asked.value.find((x) => x.conn === w.conn && x.need?.need === w.need);
  return (
    <div class="msg-think tool-wait" dir="ltr">
      <span>⏳ {H.t("look.wait", { what: w.text })}</span>
      {a && (
        <button type="button" class="think-stop" onClick={() => (H.later.value = "")}>
          {H.t("look.wait_open")}
        </button>
      )}
    </div>
  );
}

/** Clock counts from a start (Unix ms) every second. */
export function Clock({ from }: { from: number }) {
  const [now, setNow] = H.useState(Date.now());
  H.useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  return <span class="clock">{dur(now - from)}</span>;
}

const stop = () => void H.link.request("chat.stop").catch(() => undefined); // stopping is answered ok

/** Thinking is the answer being written: its time and a Stop. */
function Thinking({ from }: { from?: number }) {
  const [start] = H.useState(() => from || Date.now());
  return (
    <div class="msg-think" dir="ltr">
      <span class="think-dot" aria-hidden="true" />
      {H.t("look.thinking")} <Clock from={start} />
      <button type="button" class="think-stop" onClick={stop}>
        {H.t("stop")}
      </button>
    </div>
  );
}

const num = (n = 0) => n.toLocaleString("en-US");

/** Meta is the line under an answer: model and time, the rest in its
 * tooltip (on a phone a tap shows the rest under it). */
function Meta({ m }: { m: ChatMsg }) {
  const [open, setOpen] = H.useState(false);
  const x = m.meta;
  const line = [m.model, x?.elapsed_ms ? dur(x.elapsed_ms) : ""].filter(Boolean).join(" · ");
  if (!line) return null;
  const tip = [x?.in || x?.out ? H.t("look.tokens", { in: num(x.in), out: num(x.out) }) : "", x?.step, x?.cli].filter(Boolean).join(" · ");
  if (!H.phone)
    return (
      <div class="msg-meta" dir="ltr" title={tip || undefined}>
        {line}
      </div>
    );
  return (
    <>
      <button type="button" class="msg-meta" dir="ltr" aria-expanded={open ? "true" : "false"} disabled={!tip} onClick={() => setOpen(!open)}>
        {line}
      </button>
      {open && (
        <div class="msg-meta-more" dir="ltr">
          {tip}
        </div>
      )}
    </>
  );
}

/** chatOf is the id of the chat on show: the old one the page reads,
 * else the laptop's. */
const chatOf = (): string => H.pinned.peek()?.id ?? readChat(H.store.topic("chat").peek()?.data)?.id ?? "";

/** answer draws an AI answer: a run of several steps as a strip of its
 * steps above it, a one-step reply with its tool lines and the line
 * under it. */
function answer(m: ChatMsg, body: VNode): VNode {
  const run = m.run;
  const tools = !run && m.steps?.length === 1 ? m.steps[0].tools ?? [] : [];
  return (
    <>
      <div class="msg-label">
        <span class="star" aria-hidden="true">
          ✦
        </span>
        <span>{H.t("chat.ai")}</span>
      </div>
      {run && <Strip m={m} chat={chatOf()} />}
      {tools.length > 0 && (
        <div class="tool-rows">
          {tools.map((l, i) => (
            <Tool key={i} line={l} done={!m.streaming} />
          ))}
        </div>
      )}
      {m.wait && <Wait w={m.wait} />}
      {body}
      {run ? null : m.streaming ? <Thinking from={m.meta?.started_ms} /> : <Meta m={m} />}
    </>
  );
}

export const look: ChatLook = {
  m: (m, body) =>
    body ? (
      answer(m, body)
    ) : (
      <div class="msg msg-tool" data-i={m.i}>
        <Tool line={m.text ?? ""} done={!m.wait} />
        {m.wait && <Wait w={m.wait} />}
      </div>
    ),
  top: (msgs) => {
    let rules = "";
    for (const m of msgs) rules = m.rules || rules; // the newest
    return rules ? (
      <div class="chat-rules" dir="ltr">
        {H.t("look.rules", { name: rules })}
      </div>
    ) : null;
  },
  pick: () => <Picker />,
};
