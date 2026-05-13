// Package findbar renders a compact floating dark find widget — modeled
// on VS Code's find widget — that the host splices onto the top-right of
// the editor pane. The component emits SearchMsg as the user types,
// FindNext/FindPrev on Enter / Shift+Tab, and CloseMsg on Esc.
//
// The rendered View() is a multi-row block (rounded border + content +
// soft drop shadow). Callers should overlay it instead of stacking it as
// chrome above the editor — the panel deliberately doesn't span the full
// editor width.
package findbar
