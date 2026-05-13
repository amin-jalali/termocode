// Package replacebar renders a two-row find-and-replace input above the
// editor pane. It mirrors the visual style of the findbar but adds a second
// input for the replacement text. The user moves between the two inputs with
// Tab/Shift+Tab. Enter triggers ReplaceNextMsg (replace one match and
// advance); Ctrl+Enter or Alt+Enter triggers ReplaceAllMsg (replace every
// occurrence in the current buffer). Esc fires CloseMsg.
package replacebar
