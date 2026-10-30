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

type SignInState = "ready" | "busy" | "failed" | "removed" | "clone" | "expired";

const state = signal<SignInState>("ready");

const errorText: Partial<Record<SignInState, StringKey>> = {
  failed: "signin.failed",
  removed: "e.device_removed",
  clone: "signin.body_clone",
  expired: "signin.expired",
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
  const e = r.data.error;
  state.value = e === "removed" ? "removed" : e === "clone_suspected" ? "clone" : e === "expired" ? "expired" : "failed";
}

export function SignInScreen({ onDone }: { onDone: Done }) {
  const s = state.value;
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
