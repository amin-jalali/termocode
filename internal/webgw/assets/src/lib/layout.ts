// Which layout the page uses (web UX 4.1–4.3): phone below 720 px, tablet
// up to 1199 px, desktop from 1200 px.
import { signal } from "@preact/signals";

export type Layout = "phone" | "tablet" | "desktop";

const tablet = matchMedia("(min-width: 720px)");
const desktop = matchMedia("(min-width: 1200px)");

function current(): Layout {
  return desktop.matches ? "desktop" : tablet.matches ? "tablet" : "phone";
}

export const layout = signal<Layout>(current());

for (const mq of [tablet, desktop]) mq.addEventListener("change", () => (layout.value = current()));
