// The connect screen (web UX 3.1): the ✦ mark, the name, one line and one
// button. Errors replace the button with the one thing to do, on the
// laptop. Nothing here can be done by a link preview: the secret is only
// spent when the user taps.
import { h } from "preact";
import { t, type StringKey } from "../strings";
import { Button } from "./Button";

export type ConnectState = "ready" | "opening" | "expired" | "busy" | "nolink";

const errorText: Partial<Record<ConnectState, StringKey>> = {
  expired: "e.link_expired",
  busy: "e.auth_busy",
  nolink: "e.no_link",
};

export function ConnectScreen({ state, onOpen }: { state: ConnectState; onOpen?: () => void }) {
  const err = errorText[state];
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
          {t(err ?? "connect.once")}
        </p>
      </div>
      {(!err || state === "busy") && (
        <div class="connect-foot">
          <Button kind="primary" wide autoFocus disabled={state === "opening"} onClick={onOpen}>
            {state === "opening" ? t("connect.opening") : t("connect.open")}
          </Button>
        </div>
      )}
    </main>
  );
}
