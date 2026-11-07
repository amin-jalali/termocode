// The "↩ to chat" button (MOBUX-19) on every row and block of the phone:
// a tap puts the thing's reference into the composer, a second tap takes
// it out again (aria-pressed). Only the reference is sent, never what the
// row shows; a secret-looking file stays on the laptop.
import { h } from "preact";
import { addRef, hasRef, refLabel, removeRef, type ChatRef } from "../lib/tochat";
import { toast } from "../lib/toasts";
import { t } from "../strings";

export function ToChat({ to, title }: { to: ChatRef | null; title?: string }) {
  if (!to) return null;
  const on = hasRef(to);
  const name = refLabel(to, title);
  const tap = (e: Event) => {
    e.stopPropagation(); // the row under the button keeps its own tap
    if (on) {
      removeRef(to);
      return;
    }
    const r = addRef(to, title);
    if (r === "secret") toast("warn", t("tochat.secret"));
    else if (r === "full") toast("warn", t("tochat.full"));
    else if (r === "bad") toast("warn", t("tochat.bad"));
  };
  return (
    <button type="button" class={`tochat${on ? " is-on" : ""}`} aria-label={t("tochat.add", { name })} aria-pressed={on ? "true" : "false"} title={t("tochat.add", { name })} onClick={tap}>
      <span aria-hidden="true">{on ? "✓" : "↩"}</span>
    </button>
  );
}
