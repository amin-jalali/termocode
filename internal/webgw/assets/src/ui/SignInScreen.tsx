// Sign in again (WEB-33): a paired device that has no session in this tab
// (a new tab, the browser was closed) signs in with its passkey alone.
// The page asks only after a tap; Face ID, a fingerprint or the device
// PIN is the browser's own step.
import { h } from "preact";
import { signal } from "@preact/signals";
import { keepIssuedToken, postJSON } from "../net/auth";
import { usePasskey } from "../net/passkey";
import { t, type StringKey } from "../strings";
import { Button } from "./Button";

/** Done opens the workspace once this device has a session. */
type Done = () => void;

type SignInState = "ready" | "busy" | "failed" | "removed" | "clone" | "expired" | "code" | "checking" | "wrong";

const state = signal<SignInState>("ready");
const code = signal("");
let ticket = "";

const errorText: Partial<Record<SignInState, StringKey>> = {
  failed: "signin.failed",
  removed: "e.device_removed",
  clone: "signin.body_clone",
  expired: "signin.expired",
  wrong: "signin.wrong_code",
};

async function signIn(onDone: Done) {
  state.value = "busy";
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
  const r = await postJSON("/login/finish", { login_id: start.data.login_id, credential });
  if (keepIssuedToken(r)) {
    onDone();
    return;
  }
  if (r.status === 200 && r.data.totp === true && typeof r.data.ticket === "string") {
    ticket = r.data.ticket; // the laptop wants a sign-in code too (WEB-48)
    code.value = "";
    state.value = "code";
    return;
  }
  const e = r.data.error;
  state.value = e === "removed" ? "removed" : e === "clone_suspected" ? "clone" : e === "expired" ? "expired" : "failed";
}

/** sendCode sends the authenticator app's code with the ticket. */
async function sendCode(onDone: Done) {
  if (!/^[0-9]{6}$/.test(code.value)) return;
  state.value = "checking";
  const r = await postJSON("/login/totp", { ticket, code: code.value });
  if (keepIssuedToken(r)) {
    onDone();
    return;
  }
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
          {t(err ?? "signin.body_hint")}
        </p>
      </div>
      {s !== "removed" && s !== "clone" && (
        <div class="connect-foot">
          <Button kind="primary" wide autoFocus disabled={s === "busy"} onClick={() => void signIn(onDone)}>
            {s === "busy" ? t("signin.busy") : t("signin.button")}
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
