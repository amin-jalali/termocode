// A link from chat text never opens on a tap: it asks first (LinkSheet),
// and Open is a real link to a new tab with no opener and no referrer.
import { signal } from "@preact/signals";

/** linkAsk is the URL waiting for the user's yes, or null. */
export const linkAsk = signal<string | null>(null);

export function askLink(href: string): void {
  linkAsk.value = href;
}
