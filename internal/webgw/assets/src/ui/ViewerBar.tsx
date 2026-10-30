// The viewer's bar (web UX 5.1 V, WEB-34): under the strip while this
// device may only watch. A paired device can ask for control: it signs a
// fresh challenge with its own passkey, then the laptop shows a card.
// On the owner's yes the gateway closes the link with 4013 and the page
// comes back as a controller; the bar goes away with the new role.
import { h } from "preact";
import { signal } from "@preact/signals";
import { ReqError } from "../net/link";
import { passkeysHere, usePasskey } from "../net/passkey";
import { link } from "../session";
import { t, type StringKey } from "../strings";
import { Button } from "./Button";

type AskState = "idle" | "signing" | "waiting" | "denied" | "failed";

const ask = signal<AskState>("idle");

const lineFor: Record<AskState, StringKey> = {
  idle: "viewonly",
  signing: "viewonly",
  waiting: "wait.title",
  denied: "control.denied",
  failed: "control.failed",
};

async function askForControl(): Promise<void> {
  ask.value = "signing";
  let options: unknown;
  try {
    const res = await link.request("control.challenge");
    options = typeof res === "object" && res !== null ? (res as { options?: unknown }).options : undefined;
  } catch {
    ask.value = "failed";
    return;
  }
  let credential: unknown;
  try {
    credential = await usePasskey(options);
  } catch {
    ask.value = "idle"; // cancelled on the device: nothing was asked
    return;
  }
  ask.value = "waiting";
  try {
    await link.request("control.ask", { credential: JSON.stringify(credential) });
    // Yes: the link closes with 4013 and comes back with control.
  } catch (e) {
    ask.value = e instanceof ReqError && e.code === "forbidden" ? "denied" : "failed";
  }
}

export function ViewerBar() {
  const meta = link.meta.value;
  if (!meta || meta.role !== "viewer") {
    if (ask.peek() !== "idle") ask.value = "idle";
    return null;
  }
  const s = ask.value;
  const canAsk = meta.device.startsWith("dev-") && passkeysHere();
  return (
    <div class={`hint viewer-bar${s === "denied" || s === "failed" ? " is-error" : ""}`} role="status">
      <span class="viewer-eye" aria-hidden="true">👁</span>
      <span class="hint-text">{t(lineFor[s])}</span>
      {canAsk && s !== "waiting" && (
        <Button small needsLink disabled={s === "signing"} onClick={() => void askForControl()}>
          {t("viewonly.ask")}
        </Button>
      )}
    </div>
  );
}
