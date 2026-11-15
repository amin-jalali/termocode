# Radial menu

The owner's floating radial menu (the mobocode float_button design,
`mobocode-radial-v6.html`, as shipped in its Flutter port) for touch
pages: a floating button that opens a centre hub, an inner ring of
categories and an outer ring of actions. Framework-free TypeScript, SVG
and one CSS file; no dependencies, no build step of its own. It builds
its DOM from nodes only (no markup, no style attributes), so it runs
under a strict CSP with Trusted Types.

## Use

```ts
import { mount } from "@termocode/radial-menu"; // or a relative path to src/index.ts
import "@termocode/radial-menu/src/radial-menu.css";

const fab = mount(document.body, {
  categories: [
    { id: "git", label: "GIT", icon: "branch", items: [
      { id: "git.push", label: "push", icon: "up", detail: "push to the remote", run: () => push() },
    ] },
  ],
  favorites: ["git.push"],            // item ids: the SAVED ring, in order
  onFavoritesChange(ids) { save(ids) }, // persist them (per device)
  position: saved ?? null,            // the button's centre; null = bottom-right
  onMove(p) { savePos(p) },           // the resting centre after every drop
  theme: { accent: "#007acc", accentDim: "#0e639c", font: "JetBrains Mono" },
});
fab.setCategories(next); fab.setFavorites(ids); fab.open(); fab.close(); fab.destroy();
```

`mountMenu(el, options)` draws the menu inline instead (the design's own
page: the closed hub first; the hub opens and folds the rings in place).
`SAMPLE` and `SAMPLE_FAVORITES` are the design's sample data.

### Options

| Option | Default | Meaning |
|---|---|---|
| `categories` | – | `{ id, label, icon, items: [{ id, label, icon, detail?, run? }] }`; at most 8 items show per ring |
| `favorites` | `[]` | ids of the SAVED ring's items (up to 8) |
| `onFavoritesChange(ids)` | – | ★ favorite was toggled |
| `saved` | `true` | `false` leaves the SAVED ring out (`mountMenu`) |
| `position` / `onMove(p)` | default corner | the button's spot and its saves |
| `onOpen()` / `onClose()` | – | the menu opened / finished closing |
| `strings` | English | every word shown (`STRINGS` lists them) |
| `size` | 64 | the button's diameter |
| `reducedMotion` | `prefers-reduced-motion` | no entrance, bloom, breath or pulse |
| `insets()` | `env(safe-area-inset-*)` | the screen's safe areas |
| `extraInsets()` | – | room the page keeps for itself (its bottom bar), added to the safe areas |
| `viewport()` | window + visual viewport | the visible layer (shrinks with an on-screen keyboard) |
| `clock`, `doc`, `win` | real ones | for tests |

Icons: the design's fifteen (`star starFill plus ask open find diff
shell set check format bolt branch up down`) and more in the same hand
(`pencil play bug flask db box cloud plug gear grid phone home list flow
flag laptop chat folder board globe puzzle`).

## Behaviour

Every number is the design's (see the parity checklist in the PR).

- **Geometry** (600 × 600 design space): centre 300, categories 56…116
  (first at the bottom, clockwise, 1.2° gaps), actions 128…212 (first at
  the top, clockwise, 0.7° gaps), hub 50 with a hairline at 44, a gap
  ring at 122. The box is `min(480, screen − 24)` with 18 px padding.
- **The button**: 64 px, 16 px from the right and 12 px above the bottom
  (safe areas included). It breathes (2.8 s, scale 1.02, the glow
  .2 → .38 and blur 10 → 15 px) only while it shows, the page is visible
  and motion is allowed. A tap opens the menu centred on the button,
  moved inward only as far as the rings need. Holding it 500 ms picks it
  up: a ring of three 64° arcs spins (3 s a turn), the button lifts to
  1.12 and follows the finger; dropped past an edge it springs back
  (460 ms, easeOutBack) to 12 px inside. Its spot is re-clamped to the
  visible layer on every layout, so an on-screen keyboard pushes it up
  and it drops back after.
- **Open**: an invisible tap catcher (no dim scrim; the page stays in
  view). The menu grows in 400 ms from the button (.9 → 1); each petal
  blooms clockwise in its own window (560 ms, softBack
  `cubic-bezier(.2,.9,.3,1.12)`) with a blue spark riding the front. The
  hub shows the category and "tap to close". A category tap switches the
  actions and replays the bloom (no back step).
- **Actions**: a tap flashes the petal blue, closes the menu (300 ms
  suck-in toward the button: fade, a .5 rad swirl, down to .08) and
  runs the item. A 520 ms hold fills a progress arc along the rim (from
  16 %), swells the petal 5 % and opens the details card (icon, name,
  detail, ★ favorite); the release after it never runs the item. The
  card sits above the finger, flips below without room and stays 8 px
  inside the screen.
- **Favourites**: ★ adds the item to SAVED (toast "★ saved x", a blue
  pulse ring) or takes it out ("removed x from saved"); SAVED holds 8.
- **Keyboard**: Enter on the button opens; arrows move between petals,
  Enter runs, the context-menu key (or Shift+F10) opens the details,
  Escape closes the card, then the menu.

## Tests

`go test ./internal/webgw/assets -run TestRadialMenuPackage` bundles
`test/all.ts` with esbuild and runs it in node (a small fake DOM and a
fake clock drive every gesture and animation frame). Type check:
`tsc -p web/packages/radial-menu`.

The preview page (`demo/index.html`) needs `demo/demo.js`: bundle
`demo/demo.ts` with esbuild and serve the folder over http.
