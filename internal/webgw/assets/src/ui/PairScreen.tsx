// Pairing a phone or tablet (WEB-32/33): the page opened from the
// laptop's QR code (#p=<secret>, already out of the URL). The user taps
// once to start; the page shows a 6-digit code that the laptop's card
// shows too; the owner picks there. Then a second tap saves a passkey
// (the browser needs a tap for it) and the page opens the workspace.
// Nothing here can be done by a link preview: the secret is spent only
// on the first tap.
import { h } from "preact";
import { signal } from "@preact/signals";
import { keepIssuedToken, postJSON, type Reply } from "../net/auth";
import { createPasskey, deviceName, passkeysHere } from "../net/passkey";
import { t, type StringKey } from "../strings";
import { Button } from "./Button";

/** Done opens the workspace once this device has a session. */
type Done = () => void;

export type PairState =
  | "ready"
  | "starting"
  | "code"
  | "approved"
  | "saving"
  | "denied"
  | "expired"
  | "failed"
  | "nopasskey"
  | "cancelled";

const state = signal<PairState>(passkeysHere() ? "ready" : "nopasskey");
const code = signal("");
const guest = signal(false);
let pairID = "";
let options: unknown = null;

const lineFor: Record<PairState, StringKey> = {
  ready: "pair.body_intro",
  starting: "pair.starting",
  code: "pair.body_check",
  approved: "pair.body_approved",
  saving: "pair.saving",
  denied: "pair.denied",
  expired: "pair.body_expired",
  failed: "pair.body_failed",
  nopasskey: "pair.body_nopasskey",
  cancelled: "pair.cancelled",
};

const isError = (s: PairState): boolean => s === "denied" || s === "expired" || s === "failed" || s === "nopasskey";

/** failState maps a refused reply to what the page says. */
function failState(r: Reply): PairState {
  const e = r.data.error;
  if (r.status === 401 && (e === "used" || e === "expired")) return "expired";
  if (r.status === 410) return "expired";
  if (r.status === 403 && e === "denied") return "denied";
  return "failed";
}

async function start(secret: string): Promise<void> {
  state.value = "starting";
  const r = await postJSON("/pair/start", { p: secret, name: deviceName() });
  if (r.status !== 200 || typeof r.data.pair_id !== "string" || typeof r.data.code !== "string") {
    state.value = failState(r);
    return;
  }
  pairID = r.data.pair_id;
  code.value = r.data.code;
  guest.value = r.data.guest === true;
  state.value = "code";
  await wait();
}

/** wait long-polls the owner's answer on the laptop. */
async function wait(): Promise<void> {
  for (;;) {
    const r = await postJSON("/pair/wait", { pair_id: pairID });
    if (r.status === 200 && r.data.state === "waiting") continue;
    if (r.status === 200 && r.data.state === "approved") {
      options = r.data.options;
      state.value = "approved";
      return;
    }
    state.value = r.status === 0 ? "failed" : failState(r);
    return;
  }
}

async function save(onDone: Done) {
  state.value = "saving";
  let credential: unknown;
  try {
    credential = await createPasskey(options);
  } catch {
    state.value = "cancelled"; // the user may tap Save passkey again
    return;
  }
  const r = await postJSON("/pair/finish", { pair_id: pairID, credential });
  if (!keepIssuedToken(r)) {
    state.value = failState(r);
    return;
  }
  onDone();
}

/** spaced writes a 6-digit code as two groups of three. */
export function spaced(c: string): string {
  return c.length === 6 ? c.slice(0, 3) + " " + c.slice(3) : c;
}

export function PairScreen({ secret, onDone }: { secret: string; onDone: Done }) {
  const s = state.value;
  const err = isError(s);
  const line: StringKey = s === "code" && guest.value ? "pair.body_check_guest" : lineFor[s];
  return (
    <main class="connect pair">
      <div class="connect-top" />
      <div class="connect-mid">
        <div class={`connect-mark${err ? "" : " is-live"}`} aria-hidden="true">✦</div>
        <h1 class="connect-title">{t("pair.title")}</h1>
        {(s === "code" || s === "approved" || s === "saving" || s === "cancelled") && (
          <p class="pair-code" dir="ltr" aria-label={t("pair.code_label")}>
            {spaced(code.value)}
          </p>
        )}
        <p class={`connect-line${err ? " is-error" : ""}`} role={err ? "alert" : "status"}>
          {t(line)}
        </p>
        {s === "code" && <p class="pair-wait">{t("wait.title")}</p>}
      </div>
      {(s === "ready" || s === "starting") && (
        <div class="connect-foot">
          <Button kind="primary" wide autoFocus disabled={s === "starting"} onClick={() => void start(secret)}>
            {t("pair.start")}
          </Button>
        </div>
      )}
      {(s === "approved" || s === "saving" || s === "cancelled") && (
        <div class="connect-foot">
          <Button kind="primary" wide autoFocus disabled={s === "saving"} onClick={() => void save(onDone)}>
            {t("pair.save")}
          </Button>
        </div>
      )}
    </main>
  );
}
