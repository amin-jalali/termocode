// Sign in again (WEB-33): a paired device that has no session in this tab
// (a new tab, the browser was closed) signs in with its passkey alone.
// The page asks only after a tap; Face ID, a fingerprint or the device
// PIN is the browser's own step.
import { h } from "preact";
import { signal } from "@preact/signals";
import { keepIssuedToken, postJSON, takeRelayTicket, type Reply } from "../net/auth";
import { keepDeviceKey, useDeviceKey } from "../net/devicekey";
import { passkeysHere, usePasskey } from "../net/passkey";
import { appKeyLogin, inApp } from "../net/appkey";
import { t, type StringKey } from "../strings";
import { Button } from "./Button";
import { RememberBox, remember } from "./RememberBox";

/** Done opens the workspace once this device has a session. */
type Done = () => void;

type SignInState = "ready" | "busy" | "failed" | "removed" | "clone" | "expired" | "code" | "checking" | "wrong" | "bookmark" | "bookmark_gone";

const state = signal<SignInState>("ready");
const code = signal("");
let ticket = "";

// Over the relay (NEW-10) relay.js signed in with the phone's relay key
// and the laptop wants the code: its ticket waits here, once.
const relayTicket = takeRelayTicket();
if (relayTicket) {
  ticket = relayTicket;
  state.value = "code";
}
/** relayCode: the page opens on the code step of a relay sign-in. */
export const relayCode = !!relayTicket;

const errorText: Partial<Record<SignInState, StringKey>> = {
  failed: "signin.failed",
  removed: "e.device_removed",
  clone: "signin.body_clone",
  expired: "signin.expired",
  wrong: "signin.wrong_code",
  bookmark_gone: "signin.body_bookmark_gone",
};

/** done keeps what a good reply brought (the session token, and the next
 * bookmark key) and opens the workspace; false: no token in it. */
function done(r: Reply, onDone: Done): boolean {
  if (!keepIssuedToken(r)) return false;
  keepDeviceKey(r);
  onDone();
  return true;
}

/** askCode moves to the code step when the reply wants one (WEB-48). */
function askCode(r: Reply): boolean {
  if (r.status !== 200 || r.data.totp !== true || typeof r.data.ticket !== "string") return false;
  ticket = r.data.ticket;
  code.value = "";
  state.value = "code";
  return true;
}

/** signInWithBookmark opens the workspace with the kept bookmark key
 * (WEBP-12), still asking for the code when the laptop wants it. A key
 * the laptop refused is gone; the passkey button stays. False: not
 * signed in and no code step. */
export async function signInWithBookmark(onDone: Done): Promise<boolean> {
  state.value = "bookmark";
  const r = await useDeviceKey();
  if (r === null) {
    state.value = "ready";
    return false;
  }
  if (done(r, onDone) || askCode(r)) return true;
  // A removed device says so: the way back is pairing again, not a link.
  state.value = r.status === 401 && r.data.error === "removed" ? "removed" : r.status === 401 || r.status === 400 ? "bookmark_gone" : "ready";
  return false;
}

/** bookmarkRemoved: the laptop said this device was removed. */
export function bookmarkRemoved(): boolean {
  return state.value === "removed";
}

/** after handles a refused or good sign-in reply (passkey or app key). */
function after(r: Reply, onDone: Done): void {
  if (done(r, onDone) || askCode(r)) return;
  const e = r.data.error;
  state.value = e === "removed" ? "removed" : e === "clone_suspected" ? "clone" : e === "expired" ? "expired" : "failed";
}

async function signIn(onDone: Done) {
  state.value = "busy";
  if (inApp()) {
    // MOB-27: the app's key first; null (off on the laptop, cancelled)
    // falls back to the passkey.
    const a = await appKeyLogin();
    if (a !== null) {
      after(a, onDone);
      return;
    }
    if (!passkeysHere()) {
      state.value = "ready";
      return;
    }
  }
  const start = await postJSON("/login/start", {});
  if (start.status !== 200 || typeof start.data.login_id !== "string") {
    state.value = "failed";
    return;
  }
  let credential: unknown;
  try {
    credential = await usePasskey(start.data.options);
  } catch {
    state.value = "ready"; // cancelled: nothing happened
    return;
  }
  const r = await postJSON("/login/finish", { login_id: start.data.login_id, credential, remember: remember.value });
  after(r, onDone);
}

/** sendCode sends the authenticator app's code with the ticket. */
async function sendCode(onDone: Done) {
  if (!/^[0-9]{6}$/.test(code.value)) return;
  state.value = "checking";
  const r = await postJSON("/login/totp", { ticket, code: code.value });
  if (done(r, onDone)) return;
  code.value = "";
  state.value = r.status === 401 ? "wrong" : "expired";
}

function CodeForm({ onDone }: { onDone: Done }) {
  const s = state.value;
  return (
    <form
      class="connect-foot signin-code"
      onSubmit={(e: Event) => {
        e.preventDefault();
        void sendCode(onDone);
      }}
    >
      <label class="field">
        <span class="field-label">{t("signin.code_label")}</span>
        <input
          class="field-box signin-code-input"
          dir="ltr"
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={6}
          autoFocus
          value={code.value}
          onInput={(e: Event) => (code.value = (e.currentTarget as HTMLInputElement).value.replace(/[^0-9]/g, "").slice(0, 6))}
        />
      </label>
      <Button kind="primary" wide disabled={s === "checking" || code.value.length !== 6} onClick={() => void sendCode(onDone)}>
        {s === "checking" ? t("signin.busy") : t("signin.code_send")}
      </Button>
    </form>
  );
}

export function SignInScreen({ onDone }: { onDone: Done }) {
  const s = state.value;
  if (s === "code" || s === "checking" || s === "wrong") return <CodeScreen onDone={onDone} />;
  const err = errorText[s];
  return (
    <main class="connect">
      <div class="connect-top" />
      <div class="connect-mid">
        <div class={`connect-mark${err ? "" : " is-live"}`} aria-hidden="true">✦</div>
        <h1 class="connect-title" dir="ltr">
          {t("connect.title")}
          <span class="cursor" aria-hidden="true">▍</span>
        </h1>
        <p class={`connect-line${err ? " is-error" : ""}`} role={err ? "alert" : undefined}>
          {t(err ?? (s === "bookmark" ? "signin.bookmark" : "signin.body_hint"))}
        </p>
      </div>
      {s !== "removed" && s !== "clone" && (passkeysHere() || inApp()) && (
        <div class="connect-foot">
          <RememberBox />
          <Button kind="primary" wide autoFocus disabled={s === "busy" || s === "bookmark"} onClick={() => void signIn(onDone)}>
            {s === "busy" || s === "bookmark" ? t("signin.busy") : t("signin.button")}
          </Button>
        </div>
      )}
    </main>
  );
}

/** CodeScreen asks for the authenticator app's code after the passkey. */
function CodeScreen({ onDone }: { onDone: Done }) {
  const wrong = state.value === "wrong";
  return (
    <main class="connect">
      <div class="connect-top" />
      <div class="connect-mid">
        <div class="connect-mark is-live" aria-hidden="true">✦</div>
        <h1 class="connect-title">{t("signin.code_title")}</h1>
        <p class={`connect-line${wrong ? " is-error" : ""}`} role={wrong ? "alert" : undefined}>
          {t(wrong ? "signin.wrong_code" : "signin.body_code")}
        </p>
      </div>
      <CodeForm onDone={onDone} />
    </main>
  );
}
