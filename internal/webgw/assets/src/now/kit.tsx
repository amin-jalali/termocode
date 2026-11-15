// The parts of now.js its pages share: the requests, the one toast with
// Undo, the page head and the box's draft.
import { H, h } from "./jsx";
import { s } from "./strings";

/** initKit makes the toast's signal (init, once the host is in). */
export function initKit(): void {
  note = H.signal<Note | null>(null);
}

export const req = (op: string, args: unknown) => H.link.request(op as never, args as never) as Promise<unknown>;

/** Note is the one toast Now shows: what was done, with Undo (5 s). */
interface Note {
  text: string;
  bad?: boolean;
  undo?: () => void;
}
let note: { value: Note | null } = { value: null };
let noteTimer: ReturnType<typeof setTimeout> | undefined;
export function say(text: string, undo?: () => void, bad?: boolean): void {
  clearTimeout(noteTimer);
  note.value = { text, undo, bad };
  noteTimer = setTimeout(() => (note.value = null), 5000);
}
export const fail = (e: unknown) => say(H.errText(e), undefined, true);

export function Toast() {
  const n = note.value;
  if (!n) return null;
  return (
    <div class="toasts now-toasts" role="status" aria-live="polite">
      <div class={"toast sev-" + (n.bad ? "error" : "ok")}>
        <div class="toast-body">
          <div class="toast-title" dir="auto">
            {n.text}
          </div>
        </div>
        {n.undo && (
          <button
            type="button"
            class="toast-act"
            onClick={() => {
              n.undo?.();
              note.value = null;
            }}
          >
            {s("undo")}
          </button>
        )}
      </div>
    </div>
  );
}
export const go = (rest: string) => H.go({ tab: "home", rest, query: {} });


/** draft puts text in the Home's box and goes to the Home: nothing is
 * sent until the user sends it. */
export function draft(text: string): void {
  const c = H.readChat(H.store.topic("chat").peek()?.data);
  if (c) H.setDraft(c.id, text);
  go("");
}

export function Head({ title, back, gear }: { title: string; back: string; gear?: boolean }) {
  return (
    <header class="now-head">
      <button type="button" class="now-back" aria-label={s("back")} onClick={() => go(back)}>
        ‹
      </button>
      <h1>{title}</h1>
      {gear && (
        <button type="button" class="now-gear" aria-label={s("settings")} onClick={() => go("now/rules")}>
          ⚙
        </button>
      )}
    </header>
  );
}

