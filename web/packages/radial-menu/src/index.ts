// @termocode/radial-menu: the mobocode radial menu as a floating button
// for touch pages. Framework-free; see README.md.
import { RadialMenu, type MenuOptions } from "./menu";

export { mount, type MountOptions, type RadialFab } from "./fab";
export { RadialMenu, SAVED, STRINGS, type Category, type Clock, type Item, type MenuOptions, type Strings, type Theme } from "./menu";
export * from "./geometry";
export { DUR, SLOP, BLOOM_WINDOW, cubic, softBack, menuIn, easeOutBack, entranceFrame, ringsFrame, petalFrame, sparkFrame, collapseFrame, pulseFrame, breathFrame, breathAt, holdScale, flashScale, springAt } from "./motion";
export { ICONS, iconNodes, iconSVG } from "./icons";
export { SAMPLE, SAMPLE_FAVORITES } from "./sample";

/** mountMenu draws the menu inline in el (the design's own page: closed
 * hub first, the hub opens and closes the rings in place). el should be
 * square; the menu fills it. */
export function mountMenu(el: HTMLElement, opts: MenuOptions): RadialMenu {
  const stage = (opts.doc ?? document).createElement("div");
  stage.classList.add("rm-inline");
  el.appendChild(stage);
  const m = new RadialMenu(stage, { ...opts, onClosed: undefined });
  const measure = (): void => {
    const r = stage.getBoundingClientRect();
    m.box = { left: r.left, top: r.top, side: Math.min(r.width, r.height) };
  };
  measure();
  // The box moves with the page: measure on every press.
  stage.addEventListener("pointerdown", measure, { capture: true } as AddEventListenerOptions);
  return m;
}
