// AI: Side Question… (WEBP-9): a quick question to the laptop's quick
// model beside the main chat (chat.side), even while the main chat
// answers. The question box is the quick input; the answers show in a
// sheet, Close first and focused, with Ask another. Nothing here goes
// into the main chat. Desktop workbench only.
import { h } from "preact";
import { signal } from "@preact/signals";
import type { ChatSide } from "../../../../../webproto/proto";
import { toast } from "../../lib/toasts";
import { quick } from "../../lib/workbench";
import { link } from "../../session";
import { t } from "../../strings";
import { Sheet } from "../../ui/Sheet";

/** SIDE_MAX is how many questions the sheet keeps (the laptop sends the
 * same short history to the model). */
const SIDE_MAX = 4;

interface QA {
  q: string;
  a: string;
}

/** side holds this page's side questions; open: the sheet shows. */
const side = signal<{ list: readonly QA[]; open: boolean }>({ list: [], open: false });
let asking = false;

/** askSide opens the question box. */
export function askSide(): void {
  quick.value = { mode: "ask", ask: { title: t("side.ask"), run: sendSide } };
}

function sendSide(q: string): void {
  if (asking) {
    toast("info", t("side.busy"));
    return;
  }
  asking = true;
  toast("info", t("side.wait"));
  link.request("chat.side", { text: q }).then(
    (r) => {
      asking = false;
      side.value = { list: [...side.peek().list, { q, a: (r as ChatSide).answer }].slice(-SIDE_MAX), open: true };
    },
    (e: unknown) => {
      asking = false;
      toast("warn", e instanceof Error ? e.message : t("cmd.failed"));
    },
  );
}

const close = () => (side.value = { ...side.peek(), open: false });

/** SideChat is the answers' sheet (nothing while it is closed). */
export function SideChat() {
  const s = side.value;
  if (!s.open) return null;
  return (
    <Sheet onLater={close} label={t("side.title")}>
      <div class="side-chat">
        <h2 class="side-title">{t("side.title")}</h2>
        <div class="side-list">
          {s.list.map((x, i) => (
            <div class="side-item" key={i}>
              <p class="side-q" dir="auto">
                {x.q}
              </p>
              <p class="side-a" dir="auto">
                {x.a}
              </p>
            </div>
          ))}
        </div>
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={close}>
            {t("close")}
          </button>
          <button type="button" class="btn" onClick={() => (close(), askSide())}>
            {t("side.again")}
          </button>
        </div>
      </div>
    </Sheet>
  );
}
