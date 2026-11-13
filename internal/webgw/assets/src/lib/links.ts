// A link from chat text never opens on a tap: it asks first (the sheet
// in link.js, WEB-54), and Open is a real link to a new tab with no
// opener and no referrer. The page's dynamic import loads link.js on the
// first tap and gives it the page's Preact, sheet, link, strings and
// toasts (one copy of each in the page); it draws the sheet itself; a failed load can be tried again.
import { h, render } from "preact";
import { signal } from "@preact/signals";
import type * as LinkModule from "../link";
import { link } from "../session";
import { t } from "../strings";
import type { MenuItem } from "../ui/RowMenu";
import { Sheet } from "../ui/Sheet";
import { toast } from "./toasts";

/** linkAsk is the URL waiting for the user's yes, or null. */
export const linkAsk = signal<string | null>(null);

let mod: Promise<void> | null = null;

/** askLink opens href's sheet, once link.js is in. */
export function askLink(href: string): void {
  (mod ??= import("/assets/link.js").then((m: typeof LinkModule) => {
    m.init({ h, render, Sheet, link, toast, t, ask: linkAsk });
  })).then(
    () => void (linkAsk.value = href),
    () => {
      mod = null;
      toast("warn", t("retry"));
    },
  );
}

/** rowLinks are the web addresses in a row of text, in order, each
 * without the punctuation that ends a sentence, and where it starts. */
export function rowLinks(row: string): { url: string; from: number }[] {
  return [...row.matchAll(/https?:\/\/[^\s<>"'`]+/g)].map((m) => ({ url: m[0].replace(/[.,;:!?)\]}>]+$/, ""), from: m.index ?? 0 }));
}

/** linkItems are a row menu's entries for the row's links: each asks
 * first (a localhost link offers Forward port). At most 3. */
export function linkItems(row: string): MenuItem[] {
  return [...new Set(rowLinks(row).map((l) => l.url))].slice(0, 3).map((url) => ({ label: t("link.open") + " " + url, pick: () => askLink(url) }));
}
