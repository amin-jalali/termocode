// AI: Side Question… (WEBP-9): a quick question to the laptop's quick
// model beside the main chat (chat.side), even while the main chat
// answers. The question box is the quick input; the answers show in a
// sheet, Close first and focused, with Ask another. Nothing here goes
// into the main chat. Desktop workbench only; in aictx.js with the
// chat's other extras, loaded on first use. Types only are imported.
import type { ChatSide } from "../../../../webproto/proto";
import { h, H } from "./jsx";

/** SIDE_MAX is how many questions the sheet keeps (the laptop sends the
 * same short history to the model). */
const SIDE_MAX = 4;

interface QA {
  q: string;
  a: string;
}

/** list holds this page's side questions; open: the sheet shows. */
let list: readonly QA[] = [];
let asking = false;
let el: HTMLElement | null = null;

/** askSide opens the question box. */
export function askSide(): void {
  H.ask(H.t("side.ask"), sendSide);
}

function sendSide(q: string): void {
  if (asking) return void H.toast("info", H.t("side.busy"));
  asking = true;
  H.toast("info", H.t("side.wait"));
  H.link.request("chat.side", { text: q }).then(
    (r) => {
      asking = false;
      list = [...list, { q, a: (r as ChatSide).answer }].slice(-SIDE_MAX);
      show(true);
    },
    (e: unknown) => {
      asking = false;
      H.toast("warn", e instanceof Error ? e.message : H.t("cmd.failed"));
    },
  );
}

/** show draws the answers' sheet (or takes it away). */
function show(open: boolean): void {
  if (!el) el = document.body.appendChild(document.createElement("div"));
  H.render(open ? <SideChat /> : null, el);
}

const close = () => show(false);

function SideChat() {
  const { Sheet } = H;
  return (
    <Sheet onLater={close} label={H.t("side.title")}>
      <div class="side-chat">
        <h2 class="side-title">{H.t("side.title")}</h2>
        <div class="side-list">
          {list.map((x, i) => (
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
            {H.t("close")}
          </button>
          <button type="button" class="btn" onClick={() => (close(), askSide())}>
            {H.t("side.again")}
          </button>
        </div>
      </div>
    </Sheet>
  );
}
