// @termocode/tactile-keyboard: the Tactile Code on-screen keyboard.
// Framework-free; see README.md.
export { mount, type MountOptions, type TactileKeyboard, type Theme } from "./dom";
export { mountDock, threeFingerToggle, HANDLE_H, SUGGEST_H, MIN_HEIGHT, MAX_HEIGHT, MIN_EDITOR, type Dock, type DockOptions, type DockLabels } from "./dock";
export { Engine, REPEAT_DELAY, REPEAT_EVERY, PAD_SLOP, type Clock, type EngineOptions, type KeyEvent, type MediaAction } from "./engine";
export { DELL_LAYOUT, TABLET_BREAKPOINT, autoCompact, defaultHeight, hitTest, placeKeys, type KeyDef, type KeyKind, type Placed } from "./layout";
export { CLICK_VOLUME, VIBRATE_MS, makeTick, webClick, webVibrate } from "./feedback";
export { SuggestionBar } from "./suggestions";
export { currentWord, hasTrigger, suggest, suggestLocal, type Suggestion } from "./completion";
export { autoPair, expandSnippet, newlineIndent, type PairInsert, type SnippetExpansion } from "./smarts";
export { editorSuggestions, padStepper, type Step } from "./editing";
