// "Remember this browser" (WEBP-12): off until ticked. Ticked, the
// sign-in or pairing asks the laptop for a bookmark key, so next time a
// bookmark opens the panel without the passkey (TOTP is still asked when
// the laptop wants it).
import { h } from "preact";
import { signal } from "@preact/signals";
import { t } from "../strings";

/** remember is the box's value; the screens send it as remember. */
export const remember = signal(false);

export function RememberBox() {
  return (
    <label class="remember">
      <input
        type="checkbox"
        checked={remember.value}
        onChange={(e: Event) => (remember.value = (e.currentTarget as HTMLInputElement).checked)}
        aria-describedby="remember-d"
      />
      <span class="remember-text">
        <span class="remember-label">{t("remember.label")}</span>
        <span class="remember-hint" id="remember-d">
          {t("remember.body")}
        </span>
      </span>
    </label>
  );
}
