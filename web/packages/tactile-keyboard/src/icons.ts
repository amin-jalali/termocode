// The keyboard's glyphs: small inline SVGs drawn with currentColor (no
// icon font, no image requests), on a 24×24 grid.
export type Glyph =
  | "volume_off"
  | "volume_mute"
  | "volume_up"
  | "play_arrow"
  | "keyboard"
  | "brightness_low"
  | "brightness_high"
  | "desktop_windows"
  | "chevron_left"
  | "chevron_right"
  | "arrow_up"
  | "arrow_down"
  | "menu"
  | "sparkle"
  | "keyboard_hide"
  | "touch"
  | "code"
  | "system_keyboard";

const S = 'fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"';
const speaker = '<path d="M4 9h4l5-4v14l-5-4H4z" fill="currentColor"/>';
const kbd = '<rect x="2.5" y="6" width="19" height="12" rx="2" ' + S + '/><path d="M6 10h.01M10 10h.01M14 10h.01M18 10h.01M8 14h8" ' + S + "/>";
const sun = (rays: number): string => {
  let d = "";
  for (let i = 0; i < 8; i++) {
    const a = (i * Math.PI) / 4;
    const r1 = 7.5;
    const r2 = 7.5 + rays;
    d += `M${(12 + r1 * Math.cos(a)).toFixed(1)} ${(12 + r1 * Math.sin(a)).toFixed(1)}L${(12 + r2 * Math.cos(a)).toFixed(1)} ${(12 + r2 * Math.sin(a)).toFixed(1)}`;
  }
  return `<circle cx="12" cy="12" r="4" ${S}/><path d="${d}" ${S}/>`;
};

const BODY: Record<Glyph, string> = {
  volume_off: speaker + `<path d="M16 9l5 6M21 9l-5 6" ${S}/>`,
  volume_mute: speaker + `<path d="M16 12h2" ${S}/>`,
  volume_up: speaker + `<path d="M16 9.5a4 4 0 010 5M18.5 7a7.5 7.5 0 010 10" ${S}/>`,
  play_arrow: '<path d="M8 5v14l11-7z" fill="currentColor"/>',
  keyboard: kbd,
  brightness_low: sun(1.5),
  brightness_high: sun(3),
  desktop_windows: `<rect x="3" y="4" width="18" height="12" rx="1.5" ${S}/><path d="M9 20h6M12 16v4" ${S}/>`,
  chevron_left: `<path d="M15 6l-6 6 6 6" ${S}/>`,
  chevron_right: `<path d="M9 6l6 6-6 6" ${S}/>`,
  arrow_up: `<path d="M7 14l5-5 5 5" ${S}/>`,
  arrow_down: `<path d="M7 10l5 5 5-5" ${S}/>`,
  menu: `<path d="M5 7h14M5 12h14M5 17h14" ${S}/>`,
  sparkle: '<path d="M10 2.5l1.9 5.6 5.6 1.9-5.6 1.9L10 17.5l-1.9-5.6L2.5 10l5.6-1.9z" fill="currentColor"/><path d="M18.5 13l.95 2.55L22 16.5l-2.55.95L18.5 20l-.95-2.55L15 16.5l2.55-.95z" fill="currentColor"/>',
  keyboard_hide: kbd.replace('y="6"', 'y="3"').replace("M6 10h.01M10 10h.01M14 10h.01M18 10h.01M8 14h8", "M6 7h.01M10 7h.01M14 7h.01M18 7h.01M8 11h8") + `<path d="M9 18l3 3 3-3" ${S}/>`,
  touch: `<path d="M9 11V5.5a1.5 1.5 0 013 0V11m0-1.5a1.5 1.5 0 013 0V12m0-1a1.5 1.5 0 013 0v4a6 6 0 01-6 6h-1a6 6 0 01-5-2.7L4 15.5a1.5 1.5 0 012.5-1.6L9 16" ${S}/>`,
  code: `<path d="M9 7l-5 5 5 5M15 7l5 5-5 5" ${S}/>`,
  // a globe: "next keyboard" (the system keyboard, for other languages and dictation)
  system_keyboard: `<circle cx="12" cy="12" r="9" ${S}/><path d="M3 12h18M12 3c3 3.2 3 14.8 0 18M12 3c-3 3.2-3 14.8 0 18" ${S}/>`,
};

const SVG_NS = "http://www.w3.org/2000/svg";

/** svg returns the markup of a glyph (for tests and server-side use). */
export function svg(g: Glyph): string {
  return `<svg viewBox="0 0 24 24" width="1em" height="1em" aria-hidden="true" focusable="false">${BODY[g]}</svg>`;
}

/** setIcon draws a glyph into el as SVG elements built one by one from
 * this file's constant shapes (no markup parsing: the page requires
 * Trusted Types for HTML). */
export function setIcon(el: Element, g: Glyph): void {
  const doc = el.ownerDocument ?? document;
  const root = doc.createElementNS(SVG_NS, "svg");
  for (const [k, v] of [["viewBox", "0 0 24 24"], ["width", "1em"], ["height", "1em"], ["aria-hidden", "true"], ["focusable", "false"]]) root.setAttribute(k, v);
  for (const m of BODY[g].matchAll(/<(\w+)([^>]*)\/>/g)) {
    const shape = doc.createElementNS(SVG_NS, m[1]);
    for (const a of m[2].matchAll(/([\w-]+)="([^"]*)"/g)) shape.setAttribute(a[1], a[2]);
    root.appendChild(shape);
  }
  el.replaceChildren(root);
}
