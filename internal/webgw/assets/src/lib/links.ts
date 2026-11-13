// A link from chat text never opens on a tap: it asks first (LinkSheet),
// and Open is a real link to a new tab with no opener and no referrer.
import { signal } from "@preact/signals";
import { t } from "../strings";
import type { MenuItem } from "../ui/RowMenu";

/** linkAsk is the URL waiting for the user's yes, or null. */
export const linkAsk = signal<string | null>(null);

/** linkReady: linkAsk is a forwarded port's address (fwd.ready), which
 * opens as it is; never a second forward. */
export const linkReady = signal(false);

export function askLink(href: string): void {
  linkReady.value = false;
  linkAsk.value = href;
}

/** URL_RE finds web addresses in a row of text. */
const URL_RE = /https?:\/\/[^\s<>"'`]+/g;

/** rowLinks are the web addresses in a row of text, in order, without
 * the punctuation that ends a sentence. */
export function rowLinks(row: string): string[] {
  return [...row.matchAll(URL_RE)].map((m) => m[0].replace(/[.,;:!?)\]}>]+$/, ""));
}

/** linkItems are a row menu's entries for the row's links: each asks
 * first (a localhost link offers Forward port). At most 3. */
export function linkItems(row: string): MenuItem[] {
  return [...new Set(rowLinks(row))].slice(0, 3).map((url) => ({ label: t("link.open") + " " + url, pick: () => askLink(url) }));
}
