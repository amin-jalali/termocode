// Rows with columns that look secret do not go to the AI unasked (Add to
// AI Chat). When the next message carries such rows, Send (Enter or the
// button) stops and a card in the page names those columns: Cancel
// (first and focused) sends nothing; Mask and send puts ••• in their
// values; Send all sends them as they are. There is no laptop step: the
// page has the same trust as the laptop. Once chosen, the send goes on
// as the user started it.
import type { Attach } from "../../../../webproto/proto";
import type { ItemSpec } from "./api";
import { h, H } from "./jsx";

/** SECRET is a column name that may hold a secret (any case). */
const SECRET = /pass(word|wd|phrase)|pwd|secret|token|api_?key|private_?key|access_?key|hash|salt|credential|otp|pin_?code|cvv|ssn/i;

/** MASK stands for a masked value (the laptop reads it so). */
export const MASK = "•••";

/** asked are the item attaches the user already decided on. */
const asked = new WeakSet<Attach>();

function spec(a: Attach): ItemSpec | null {
  try {
    return a.kind === "item" && a.spec ? (JSON.parse(a.spec) as ItemSpec) : null;
  } catch {
    return null;
  }
}

/** secretCols are a's columns whose values go and look secret. */
function secretCols(a: Attach): string[] {
  const s = spec(a);
  if (!s?.rows?.length || asked.has(a)) return [];
  return (s.columns ?? []).filter((c) => SECRET.test(c) && !s.masked?.includes(c));
}

/** masked is a with its secret-looking values as •••. */
function masked(a: Attach): Attach {
  const s = spec(a) as ItemSpec;
  const cols = secretCols(a);
  const at = cols.map((c) => (s.columns ?? []).indexOf(c));
  s.rows = (s.rows ?? []).map((r) => r.map((v, j) => (v !== null && at.includes(j) ? MASK : v)));
  s.masked = [...(s.masked ?? []), ...cols];
  return { ...a, spec: JSON.stringify(s) };
}

let el: HTMLElement | null = null;

/** gate stops a send whose rows hold secret-looking columns and asks. */
function gate(e: Event, box: HTMLTextAreaElement, chat: string): void {
  const list = H.attaches.peek()[chat] ?? [];
  const cols = [...new Set(list.flatMap(secretCols))];
  if (!cols.length || !box.value.trim()) return;
  e.preventDefault();
  e.stopImmediatePropagation();
  if (!el) el = document.body.appendChild(document.createElement("div"));
  const close = () => H.render(null, el as HTMLElement);
  const go = (mask: boolean) => {
    close();
    const now = H.attaches.peek()[chat] ?? [];
    const next = now.map((a) => {
      if (!secretCols(a).length) return a;
      const b = mask ? masked(a) : a;
      asked.add(b);
      return b;
    });
    H.attaches.value = { ...H.attaches.peek(), [chat]: next };
    box.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
  };
  const { Sheet } = H;
  H.render(
    <Sheet onLater={close} label={H.t("ai.secret_title")}>
      <div class="ai-secret">
        <h2 class="side-title">{H.t("ai.secret_title")}</h2>
        <p dir="auto">{H.t("ai.secret_body")}</p>
        <p class="ai-secret-cols" dir="ltr">
          {cols.join(", ")}
        </p>
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={close}>
            {H.t("cancel")}
          </button>
          <button type="button" class="btn" onClick={() => go(true)}>
            {H.t("ai.secret_mask")}
          </button>
          <button type="button" class="btn btn-neutral" onClick={() => go(false)}>
            {H.t("ai.secret_all")}
          </button>
        </div>
      </div>
    </Sheet>,
    el,
  );
}

/** watchSends checks every send of the chat composer (chat: its chat). */
export function watchSends(chat: () => string): void {
  document.addEventListener(
    "keydown",
    (e) => {
      const b = e.target;
      if (e.key === "Enter" && !e.shiftKey && !e.isComposing && b instanceof HTMLTextAreaElement && b.matches(".composer-box")) gate(e, b, chat());
    },
    true,
  );
  document.addEventListener(
    "click",
    (e) => {
      const box = (e.target as Element).closest?.(".composer .send button")?.closest(".composer")?.querySelector<HTMLTextAreaElement>(".composer-box");
      if (box) gate(e, box, chat());
    },
    true,
  );
}
