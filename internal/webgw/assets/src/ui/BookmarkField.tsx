// Bookmark sign-in on "This device" (WEBP-12): says whether this browser
// is remembered, and forgets it here and on the laptop with one tap (the
// safe direction: no question). The laptop's device card can forget it
// too.
import { h } from "preact";
import { signal } from "@preact/signals";
import { forgetThisBrowser, remembered } from "../net/devicekey";
import { t } from "../strings";
import { Button } from "./Button";

const forgotten = signal(false);

export function BookmarkField({ desk = false }: { desk?: boolean }) {
  const on = remembered.value;
  const text = t(on ? "bookmark.body_on" : forgotten.value ? "bookmark.body_forgotten" : "bookmark.body_off");
  return (
    <div class={desk ? "set-item set-bookmark" : "field bookmark-field"}>
      <span class={desk ? "set-name" : "field-label"}>{t("bookmark.title")}</span>
      <p class={desk ? "set-desc" : "group-text"} role="status">
        {text}
      </p>
      {on && (
        <Button
          small
          onClick={() =>
            void forgetThisBrowser().then(() => {
              forgotten.value = true;
            })
          }
        >
          {t("bookmark.forget")}
        </Button>
      )}
    </div>
  );
}
