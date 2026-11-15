// The menu's icons: SVG drawn in a ±9 box around the origin, {c} is the
// colour. The first fifteen are the design's own, 1:1 (radial_menu.dart
// _icons, mobocode-radial-v6.html ICONS); the rest are drawn in the same
// hand (1.8 strokes, round joins) for the views termocode has.
const S = 'fill="none" stroke="{c}" stroke-width="1.8"';
const R = `${S} stroke-linecap="round" stroke-linejoin="round"`;

export const ICONS: Record<string, string> = {
  star: '<path d="M0,-9 L2.6,-2.8 L9,-2.8 L3.7,1.4 L5.6,8 L0,4 L-5.6,8 L-3.7,1.4 L-9,-2.8 L-2.6,-2.8 Z" fill="none" stroke="{c}" stroke-width="1.7" stroke-linejoin="round"/>',
  starFill: '<path d="M0,-9 L2.6,-2.8 L9,-2.8 L3.7,1.4 L5.6,8 L0,4 L-5.6,8 L-3.7,1.4 L-9,-2.8 L-2.6,-2.8 Z" fill="{c}" stroke="{c}" stroke-width="1.3" stroke-linejoin="round"/>',
  plus: '<path d="M0,-7 V7 M-7,0 H7" fill="none" stroke="{c}" stroke-width="1.9" stroke-linecap="round"/>',
  ask: '<path d="M0,-9 C.8,-3 3,-.8 9,0 C3,.8 .8,3 0,9 C-.8,3 -3,.8 -9,0 C-3,-.8 -.8,-3 0,-9 Z" fill="{c}"/>',
  open: `<path d="M-6,-8 H2 L6,-4 V8 H-6 Z" ${S} stroke-linejoin="round"/><path d="M2,-8 V-4 H6" ${S}/>`,
  find: `<circle cx="-2" cy="-2" r="5.5" ${S}/><path d="M2.2,2.2 L8,8" stroke="{c}" stroke-width="1.8" stroke-linecap="round"/>`,
  diff: `<rect x="-8" y="-8" width="10" height="10" rx="1.5" ${S}/><rect x="-2" y="-2" width="10" height="10" rx="1.5" ${S}/>`,
  shell: `<rect x="-9" y="-7" width="18" height="14" rx="2.5" ${S}/><path d="M-4,-2 L-1,.5 L-4,3" ${R}/><path d="M1,4 H5" stroke="{c}" stroke-width="1.8" stroke-linecap="round"/>`,
  set: '<path d="M-8,-5 H8 M-8,0 H8 M-8,5 H8" stroke="{c}" stroke-width="1.8" stroke-linecap="round"/><circle cx="-2" cy="-5" r="2.4" fill="{c}"/><circle cx="4" cy="0" r="2.4" fill="{c}"/><circle cx="-4" cy="5" r="2.4" fill="{c}"/>',
  check: `<path d="M-6,0 L-2,5 L7,-6" ${R}/>`,
  format: '<path d="M-7,-5 H7 M-7,0 H2 M-7,5 H5" stroke="{c}" stroke-width="1.8" stroke-linecap="round"/>',
  bolt: '<path d="M1.5,-8 L-5,1.5 H-.5 L-1.5,8 L6,-1.5 H1.5 Z" fill="{c}"/>',
  branch: '<circle cx="-5" cy="-6" r="2.6" fill="{c}"/><circle cx="-5" cy="6" r="2.6" fill="{c}"/><circle cx="6" cy="-1" r="2.6" fill="{c}"/><path d="M-5,-3 V3 M-5,1 H1 A4,4 0 0 0 5,-1 V-3" fill="none" stroke="{c}" stroke-width="1.7"/>',
  up: `<path d="M0,7 V-6 M-5,-1 L0,-7 L5,-1" ${R}/>`,
  down: `<path d="M0,-7 V6 M-5,1 L0,7 L5,1" ${R}/>`,
  // termocode's views, same hand
  pencil: `<path d="M-7,7 L-6,3 L4,-7 L7,-4 L-3,6 Z" ${R}/><path d="M2,-5 L5,-2" ${R}/>`,
  play: '<path d="M-5,-8 L7,0 L-5,8 Z" fill="{c}"/>',
  bug: `<ellipse cx="0" cy="1.5" rx="5" ry="6.5" ${S}/><path d="M-5,0 H-8.5 M5,0 H8.5 M-5,4.5 L-8,7 M5,4.5 L8,7 M-4.5,-3.5 L-7.5,-6 M4.5,-3.5 L7.5,-6 M0,-5 V8" ${R}/>`,
  flask: `<path d="M-3,-8 H3 M-2,-8 V-2 L-7,7 H7 L2,-2 V-8" ${R}/><path d="M-4.5,3 H4.5" ${R}/>`,
  db: `<ellipse cx="0" cy="-5.5" rx="7" ry="2.8" ${S}/><path d="M-7,-5.5 V5.5 C-7,9 7,9 7,5.5 V-5.5 M-7,0 C-7,3.6 7,3.6 7,0" ${S}/>`,
  box: `<path d="M0,-8.5 L8,-4.5 V4.5 L0,8.5 L-8,4.5 V-4.5 Z M-8,-4.5 L0,-.5 L8,-4.5 M0,-.5 V8.5" ${R}/>`,
  cloud: `<path d="M-5,6 H5.5 A3.8,3.8 0 0 0 5.2,-1.6 A5.6,5.6 0 0 0 -5.4,-1.2 A3.6,3.6 0 0 0 -5,6 Z" ${R}/>`,
  plug: `<path d="M-3,-8 V-3 M3,-8 V-3 M-6,-3 H6 V0 A6,6 0 0 1 -6,0 Z M0,6 V9" ${R}/>`,
  gear: `<circle cx="0" cy="0" r="3" ${S}/><path d="M0,-8.5 V-5.5 M0,5.5 V8.5 M-8.5,0 H-5.5 M5.5,0 H8.5 M-6,-6 L-3.9,-3.9 M3.9,3.9 L6,6 M6,-6 L3.9,-3.9 M-3.9,3.9 L-6,6" ${R}/>`,
  grid: `<rect x="-8" y="-8" width="6.5" height="6.5" rx="1.3" ${S}/><rect x="1.5" y="-8" width="6.5" height="6.5" rx="1.3" ${S}/><rect x="-8" y="1.5" width="6.5" height="6.5" rx="1.3" ${S}/><rect x="1.5" y="1.5" width="6.5" height="6.5" rx="1.3" ${S}/>`,
  phone: `<rect x="-5.5" y="-9" width="11" height="18" rx="2.5" ${S}/><path d="M-1.5,6 H1.5" ${R}/>`,
  home: `<path d="M-8,0 L0,-7.5 L8,0 M-5.5,-2 V8 H5.5 V-2" ${R}/><path d="M-1.8,8 V3 H1.8 V8" ${R}/>`,
  list: '<path d="M-3,-6 H8 M-3,0 H8 M-3,6 H8" stroke="{c}" stroke-width="1.8" stroke-linecap="round"/><circle cx="-7" cy="-6" r="1.4" fill="{c}"/><circle cx="-7" cy="0" r="1.4" fill="{c}"/><circle cx="-7" cy="6" r="1.4" fill="{c}"/>',
  flow: `<rect x="-9" y="-8" width="7" height="5" rx="1.2" ${S}/><rect x="2" y="-2.5" width="7" height="5" rx="1.2" ${S}/><rect x="-9" y="3" width="7" height="5" rx="1.2" ${S}/><path d="M-2,-5.5 H-.5 V5.5 H-2 M-.5,0 H2" ${R}/>`,
  flag: `<path d="M-6,9 V-8 M-6,-8 H6 L3,-4 L6,0 H-6" ${R}/>`,
  laptop: `<rect x="-7" y="-7" width="14" height="10" rx="1.5" ${S}/><path d="M-9.5,6.5 H9.5" ${R}/>`,
  chat: `<path d="M-8,-6 Q-8,-8 -6,-8 H6 Q8,-8 8,-6 V2 Q8,4 6,4 H-1 L-5,8 V4 H-6 Q-8,4 -8,2 Z" ${R}/>`,
  folder: `<path d="M-8.5,-6 Q-8.5,-7.5 -7,-7.5 H-2.5 L-.5,-5 H7 Q8.5,-5 8.5,-3.5 V6 Q8.5,7.5 7,7.5 H-7 Q-8.5,7.5 -8.5,6 Z" ${R}/>`,
  board: `<rect x="-8.5" y="-7.5" width="17" height="15" rx="2" ${S}/><path d="M-3,-7.5 V7.5 M2.8,-7.5 V7.5 M-6.5,-4 H-5 M-1,-4 H.8 M4.8,-4 H6.5 M-1,0 H.8" ${R}/>`,
  globe: `<circle cx="0" cy="0" r="8" ${S}/><path d="M-8,0 H8 M0,-8 C-4,-4 -4,4 0,8 C4,4 4,-4 0,-8" ${S}/>`,
  puzzle: `<path d="M-7,-3 H-3.5 A2.2,2.2 0 1 1 .5,-3 H4 V.5 A2.2,2.2 0 1 1 4,4.5 V8 H-7 Z" ${R}/>`,
};

/** iconSVG is icon name's markup in colour col (empty for an unknown name). */
export function iconSVG(name: string, col: string): string {
  return (ICONS[name] ?? "").replace(/\{c\}/g, col);
}
