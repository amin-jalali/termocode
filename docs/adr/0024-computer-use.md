# ADR 0024 — Computer use: desktop tools behind a setting, every action asks

Status: **Accepted** (2026-11-13) — small first version; checked with
fakes only, not yet on a real desktop.

## Context
Some work happens outside the editor: a desktop app, an emulator window,
a dialog the AI cannot reach through a file or a command. Other tools let
the AI see the screen and click. That is powerful and risky: a click can
send a mail, delete a file or approve a payment, and a screenshot can
show private things from other windows.

Constraints:
- Off by default. Only the user turns it on, by hand; the AI never can.
- Every single action is approved by the user. No "Always allow", no
  autopilot, no allow rule skips it.
- No cgo and no new library in the default build.
- termocode often runs over SSH with no desktop: it must say so plainly.

## Decision
- Setting `ai_computer_use` (config.json, default off). The settings
  policy marks it "never" for the AI.
- Four chat tools, only while the setting is on: `computer_screenshot`,
  `computer_click`, `computer_type`, `computer_key`. They are not in the
  headless `termocode ai -x` tools, the chat bots or agent CLIs.
- Each tool is mutating with no class: the card shows every time, with no
  "Always allow". `ai.ClassifyRisk` rates `computer_*` high, so autopilot
  and allow rules always fall back to the card.
- The card shows the exact action ("Click left at 10, 20", the text to
  type, the keys). Args are checked before the card: points in range,
  known buttons, keys like `ctrl+s` (never an option), text without
  control characters and at most 2000 characters.
- Package `internal/computeruse` runs one outside program per action,
  with plain arguments and no shell: X11 `xdotool` and `scrot` (or
  ImageMagick `import`), Wayland `grim` and `ydotool`, macOS
  `screencapture` and `cliclick`. A missing program gives a clear
  message with what to install.
- A screenshot is saved as a PNG under `.termocode/screens/` in the
  workspace; the AI reads it with `look_at` (one more request).

## Consequences
- Works where those programs exist; Windows and plain SSH say "not
  supported" / "no desktop".
- No accessibility tree yet: the AI works from screenshots and pixel
  points, which is slower and less exact.
- Later: an accessibility-tree reader, a "this window only" screenshot,
  a session-long approval the user can give with a clear time limit.
