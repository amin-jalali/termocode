// @termocode/tactile-keyboard: the Tactile Code on-screen keyboard.
// Framework-free; see README.md.
export { mount, type MountOptions, type TactileKeyboard, type Theme } from "./dom";
export { Engine, REPEAT_DELAY, REPEAT_EVERY, PAD_SLOP, type Clock, type EngineOptions, type KeyEvent, type MediaAction } from "./engine";
export { DELL_LAYOUT, TABLET_BREAKPOINT, autoCompact, defaultHeight, hitTest, placeKeys, type KeyDef, type KeyKind, type Placed } from "./layout";
export { CLICK_VOLUME, VIBRATE_MS, makeTick, webClick, webVibrate } from "./feedback";
