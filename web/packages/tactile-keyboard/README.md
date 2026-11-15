# Tactile Code keyboard

A laptop-style on-screen keyboard for writing code on a phone or tablet:
the owner's "Tactile Code" design (Dell US-ANSI layout, an AI key in
place of the Windows key, recessed-well keycaps). Framework-free
TypeScript and one CSS file; no dependencies, no build step of its own.

## Use

```ts
import { mount } from "@termocode/tactile-keyboard"; // or a relative path to src/index.ts
import "@termocode/tactile-keyboard/src/tactile-keyboard.css";

const kb = mount(document.getElementById("kb")!, {
  layout: "auto", // "compact" (phone), "full" (tablet) or "auto": short side < 720 px → compact
  onKey(e) {
    // e = { key, code, ctrl, alt, shift, meta, id, repeat } in DOM KeyboardEvent terms:
    // "a", "A", "!", " ", "Enter", "Backspace", "ArrowUp", "PageUp", "F5"…
  },
  onAI() {/* the ✨ key */},
  theme: { accent: "#007acc" },
});
kb.destroy();
```

A key with `ctrl` or `alt` set is a shortcut, never text. A printable key
(`key.length === 1`, no ctrl/alt) is text, already cased and shifted.

### The dock

`mountDock(el, options)` wraps the keyboard for a page: it shows while a
text surface has focus (`attach(target)` / `detach(target)`), has a drag
handle (pull down to peek at the code under the keyboard; it springs back),
a hide button and a "Show keyboard" bar, a system-keyboard button
(`onSystemKeyboard`, for other languages and dictation), the suggestion
strip (`dock.suggestions.show(items, onAccept)`), and a safe-area filler.
The keyboard is 380 px high by default (220…540) and never leaves less
than 140 px of the page above it. On a phone held upright (a compact
board under 540 px wide, narrower than the page is high) the default
follows the width instead (`uprightHeight`): a 1-unit key is 0.7 as wide
as high, half the landscape shape, and word labels shrink to one line
(`.tk-narrow`). A height the host sets is kept. A real hardware key on the attached
surface collapses the dock (an external keyboard is in use).
`threeFingerToggle(el, dock)` makes a three-finger tap toggle it.

Give the focus target `inputmode="none"` while the dock is attached so
the system keyboard stays closed.

### Options

| Option | Default | Meaning |
|---|---|---|
| `layout` | `"auto"` | compact / full / auto |
| `height` | width × 0.40 (180…360); the dock: 380 | board height |
| `hapticLevel` | 2 | 0 off, 1 light (6 ms), 2 medium (12 ms), 3 strong (18 ms) |
| `soundOn` | true | soft click on press (gain 0.08) and release (0.04) |
| `vibrate`, `click` | navigator.vibrate, Web Audio | replace the feedback |
| `tilt` | true | device tilt moves the keycap shadows |
| `onPadMove(dx, dy)` | – | spacebar drag (trackpad), accelerated |
| `onMedia(action)` | – | F1 mute, F2/F3 volume, F4 play, F6/F7 brightness (a page cannot change these itself) |
| `onToggleSound(on)` | – | F9 flipped the sound |

## Behaviour

- Keys fire on touch down; every finger is its own pointer (chords, rollover).
- Modifiers (shift, ctrl, alt, fn) are held while a finger is on them. A tap
  that modified nothing arms it for the next key (ctrl, tap, c → Ctrl+C);
  tapping it again disarms it. Armed keys get an accent rim.
- Caps lock toggles on press; its LED lights.
- Backspace, Delete and the arrows repeat: first after 350 ms, then every 55 ms.
- The spacebar: a tap is a space, a drag (more than 10 px) is a trackpad.
- fn layer: fn + function row → F1…F12 (home/end are F11/F12), fn + 1…0 - = →
  F1…F12 (the compact layout has no function row), fn + ↑/↓ → PageUp/PageDown,
  fn + ←/→ → Home/End, fn + ` → Escape, fn + F5 → backlight (off, dim, medium).
- Without fn, F1–F4, F6, F7 are media keys (`onMedia`), F9 toggles the sound.

Helpers for an editor host: `autoPair`, `newlineIndent`, `expandSnippet`
(typing smarts), `currentWord`, `suggestLocal`, `suggest` (ranked buffer +
language-server completions), `editorSuggestions` (snippet chip first) and
`padStepper` (spacebar trackpad → caret steps).

## Test

The tests run in node with esbuild (no npm):

    go test ./internal/webgw/assets -run TestTactileKeyboardPackage -v

Type check: `tsc -p web/packages/tactile-keyboard`.

## Preview

    go run github.com/evanw/esbuild/cmd/esbuild web/packages/tactile-keyboard/demo/demo.ts \
      --bundle --format=esm --outfile=web/packages/tactile-keyboard/demo/demo.js

then serve `web/packages/tactile-keyboard` over http and open `demo/index.html`
(add `?layout=full` for the tablet layout). `demo.js` is not committed.
